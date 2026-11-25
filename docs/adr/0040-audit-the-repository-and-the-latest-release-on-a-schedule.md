# 0040. Audit the repository and the latest release on a schedule, with a model that triages and a script that publishes

- Status: Accepted, Decision 2's presence-flag rule and the coverage rule that follows from it included:
  the maintainer confirmed both on 2026-11-21 at PR #444's review (see Context).
  PRD #175 milestone 2 amends this record with the workflow's implementation (below); its first
  amendment, how Claude Code runs in the model job, is dated 2026-11-22, its second, the publish
  program's core, 2026-11-23. Issue #568 amends it with the plan for the runner image's move to
  `ubuntu-26.04` (2026-11-25)
- Date: 2026-11-21, amended 2026-11-22 to 2026-11-25

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

Two amendments follow. "The publish program's core" (2026-11-23) settles the publish program's core: the
result schema and its closed category list, the publish script's false-positive matching and its
derivation of each finding's status, the finding limit and the encoded-run thresholds. Then "How Claude
Code runs in the model job" (2026-11-22) covers how Claude Code runs: the shared launcher, the audit's
launcher and its environment, the audit directory, the settings file and its permission mode, the
wrapper, the sandbox preflight, the user-namespace handling and the Claude Code pin. A third, "The
publish program's GitHub side" (2026-11-23), settles the publish program's entry point and its GitHub
side: the package split, the `gh` calling contract and its endpoint table, the fake `gh` its tests run
against, and the subcommands. A fourth, "The workflow" (2026-11-24, PR 5b), settles the encryption tool
and the artifact handling, the jobs' read scopes, the App token's `permission-*` inputs, the time budget
and the Actions-cache residual risk. The other items of the list above (the container flags, the checks
job's final scopes) come with PR 5c.

### The publish program's core (milestone 2, amended 2026-11-23)

The publish job's program is split as the other CI programs are (`ci/aireview`, `ci/claudecodepin`, ADR
0033): a **core** that decides and renders and does no I/O, and an entry point that reads, calls GitHub
and exits. This subsection settles the core, PRD #175 milestone 2's third pull request; the GitHub side
(the fake `gh`, the endpoints, advisories, issues, the artifact listing) and the entry point come in the
fourth, the workflow in the fifth. Where the PRD left a choice open, the choice closest to its security
text is taken, and the reason given.

**Packages.**

- `ci/auditpublish/internal/publish`, the core. It reads nothing and writes nothing: it takes bytes,
  strings and values its caller passes, and returns values and errors. It never reads the clock, the
  environment or a file: the run's date, the audited commit, the run's identity, the fingerprint key and
  the previous reports reach it as arguments. Its depguard rule, `audit-publish-library` (strict), allows
  exactly `bytes`, `crypto/hmac`, `crypto/sha256`, `encoding/base64`, `encoding/hex`, `errors`, `fmt`,
  `math`, `regexp`, `slices`, `strconv`, `strings`, `time`, `unicode` and `unicode/utf8`, and denies
  `os` and `net` with a reason. `encoding/json` is not among them: the core reads JSON with its own
  parser (below). Go's internal rule keeps it out of reach of
  `cmd/` and `internal/`.
- `ci/auditpublish`, the entry point (milestone 2's fourth pull request): a main package that reads the
  decrypted result and the merged reports, the run's variables and the fingerprint key, runs gitleaks
  over the texts the core built, calls the fixed GitHub endpoints through `gh`, and prints the one
  summary line. Its strict rule, `audit-publish-main`, and the forbidigo group for its environment reads
  are set with it; it imports the core and named standard packages only, no third-party package.

**The result schema.** The model writes one JSON file, `result.json` in the work area (the model job's
task names that path, as the `security-audit-run` skill asks; the skill's "the name and the schema the
task gives"). The core's `ParseResult(data []byte, commit string) (Result, error)` accepts it only if all
of this holds, and otherwise returns an error wrapping `ErrResultInvalid` (a `commit` argument that is
not 40 lowercase hex is the caller's, `ErrConfig`):

- at most `MaxResultBytes` (4 MiB) in all, one JSON object and nothing after it;
- no unknown field, no duplicate key at any level, every field present, no `null`, every value of its
  type. encoding/json keeps the last of two keys silently, so a second `severity` could hide behind the
  first, and it turns invalid UTF-8 and a lone surrogate escape into U+FFFD before anything sees them,
  so the core reads the result with a strict JSON parser of its own that keeps every member in order and
  marks an invalid string, and follows at most 8 levels of nesting, the top object being level 1: a
  value at level 8 is parsed, one at level 9 is refused as no JSON value (`depth >= maxJSONDepth`);
- every string valid UTF-8, its cap counted in **bytes**, and **the text rule**, an allow list
  (`textRune`, which `ParsePreviousReport`'s cell check applies too): a field holds only printable
  ASCII, U+0020 to U+007E, and a field marked *multi-line* below that plus LF and TAB; a previous
  report's model cell (location, scenario, fix, a False positives reason) holds only U+0020 to U+007E;
  a fixed row's cells are exactly `—`. Every other rune is refused. A lookalike letter (Cyrillic
  and Greek letters for Latin ones, the fullwidth forms), a combining mark with no glyph of its own, a
  typographic dash or quote the eye reads as an ASCII one, and a rune that renders as nothing or as
  blank space all carry a token past the maintainer's eye and past every pattern, and a list of
  characters to refuse cannot be complete: each Unicode version adds to them. The allow list holds no
  control, no bidirectional, zero-width or invisible character and no space but U+0020, so which kind
  of blank a model puts between two words carries nothing; how many U+0020 it writes still can (the
  ASCII-spacing residual below). The cost is that a model field cannot quote a non-ASCII string, a
  name, an error message or a file's text, as it stands: a quoted non-ASCII character refuses the whole
  run. The fourth pull request's model-job prompt tells the model to write ASCII only;
- at most `FindingLimit` findings. **The finding limit is 100.** One root cause is one finding (the
  skill), so a month's triage of this repository stays far below it; a result above it is a sign of a
  broken or steered run, not of a hundred real findings, and the limit bounds the publish job's
  advisory and issue calls well inside GitHub's secondary rate limits and the job's time budget. Over
  the limit, the whole result is refused, never truncated: a truncation would choose which findings
  survive.

| Field | Type | Accepted |
|---|---|---|
| `schema` | number | exactly `1` (`SchemaVersion`) |
| `commit` | string | 40 lowercase hex, equal to the `commit` argument (the job's `github.sha`) |
| `release_tag` | string | `^v(0\|[1-9][0-9]{0,3})\.(0\|[1-9][0-9]{0,3})\.(0\|[1-9][0-9]{0,3})$`; the entry point checks that it names an existing release |
| `image_digest` | string | `^sha256:[0-9a-f]{64}$`, or empty when the image was not pulled; model text, rendered as a `CodeSpan` and scanned as a model field, so the hex rule refuses a digest that decodes to printable ASCII; the entry point checks it against the release's attested image digest before trusting it (fourth pull request) |
| `tools` | array, at most 16 | objects `{name, version}`, each `name` once |
| `tools[].name` | string | one of `actionlint`, `age`, `claude-code`, `cosign`, `gitleaks`, `go`, `govulncheck`, `osv-scanner`, `scorecard`, `trivy`, `zizmor` |
| `tools[].version` | string | `^v?[0-9]{1,4}(\.[0-9]{1,4}){0,3}$` |
| `checks` | array | exactly one object `{id, ran, not_run}` per check id (Coverage, below), in any order |
| `checks[].id` | string | a check id |
| `checks[].ran` | boolean | |
| `checks[].not_run` | string | empty when `ran` is true, otherwise a why id (Coverage, below) |
| `findings` | array, at most 100 | objects with the fields below |
| `findings[].check` | string | a check id whose `ran` is true |
| `findings[].rule` | string | `^[A-Za-z0-9][A-Za-z0-9._:/-]{0,79}$`: the rule or check that found it (`G304`, `GO-2026-1234`, `gitleaks:generic-api-key`) |
| `findings[].file` | string | 1 to 200 bytes of `[A-Za-z0-9_.+@:/-]`, starting with a letter, a digit or `_`, with no empty, `.` or `..` path segment: a repository path, a release artifact's name, or for check `history-secrets` the commit's 40 lowercase hex |
| `findings[].line` | number | an integer from 0 to 9999999, 0 when the location has no line |
| `findings[].sink` | string | 1 to 120 bytes of `[A-Za-z0-9_.()*/:\[\]-]`: the function, call or property the input reaches |
| `findings[].severity` | string | at most 16 bytes; any value but the five severities is unknown |
| `findings[].confirmed` | boolean | |
| `findings[].category` | string | at most 32 bytes; any value but the ten categories above is unknown |
| `findings[].private` | boolean | the model's flag: it can only make the finding more private |
| `findings[].scenario` | string, multi-line | 1 to 2000 bytes |
| `findings[].evidence` | string, multi-line | at most 16000 bytes: the PoC or trace, used in a draft advisory only, never in a report or a public issue, since it is the one field the encoded-run refusal does not read |
| `findings[].fix` | string, multi-line | 1 to 1000 bytes |
| `findings[].dismissal` | string | at most 200 bytes, empty when none is proposed |
| `changes` | object | `{dependencies, narrative}` |
| `changes.dependencies` | array, at most 50 | strings of 1 to 160 bytes |
| `changes.narrative` | string, multi-line | at most 2000 bytes |
| `limits` | array, at most 40 | strings of 1 to 300 bytes |

The severity and category values are compared exactly, case included: `High` is unknown. An unknown
value is accepted, not refused, because the PRD makes it private, and refusing it would let a mislabel
stop the whole run; it is never rendered (a private finding's advisory reads "unknown"). There is no
model summary and no private-notes field: the script writes the summary from its own counts, so a model
paragraph cannot hint at a private finding there, and a field the public never sees is not needed.

The accepted result is a `Result{Schema, Commit, ReleaseTag, ImageDigest, Tools []Tool, Checks []Check,
Findings []Finding, Changes, Limits []string}`, with `Tool{Name, Version}`, `Check{ID, Ran, NotRun}`,
`Changes{Dependencies []string, Narrative}` and `Finding{Check, Rule, File, Line, Sink, Severity,
Confirmed, Category, Private, Scenario, Evidence, Fix, Dismissal}` (`Private` is the schema's
`private`). The value rules also run in `RenderReport`, so a `Result` a caller builds by hand reaches no
report that `ParseResult` would have refused. Two findings with the same rule, file and sink are
accepted by `ParseResult` (the finding limit is the bound) but refused by `RenderReport`
(`ErrResultInvalid`): one fingerprint is one finding, and two rows of one fingerprint would make the next
run's previous report unreadable.

**Escaping.** Model text reaches Markdown only inside a **code span** or a **fenced code block**, never
as Markdown of its own. Backslash escapes would not do: GitHub links bare URLs, `www.` hosts, e-mail
addresses, `@mentions` and `#123` references in text it has already unescaped, and a mention or a
reference also writes to another issue's timeline or notifies a person. Inside a code span or a code block
GitHub renders no HTML, image, link, autolink, mention, reference, entity or emphasis. The functions:

- `CodeSpan(s string) string`, inline (a list item, a paragraph): CR LF and lone CR become LF, then every
  LF and TAB becomes one space; the delimiter is a run of backticks one longer than the longest run of
  backticks in the text; the result is the delimiter, one space, the text, one space, the delimiter
  (GFM strips exactly those two spaces, so a text starting or ending with a backtick stays intact). An
  empty text gives `none`, not a code span.
- `TableCell(s string) string`: `CodeSpan(s)` with every `|` written `\|`, which GFM's table parser
  turns back into `|` inside a cell, code spans included; no other byte changes.
- `CodeBlock(s string) string`, multi-line (an advisory's or an issue's body): CR LF and lone CR become
  LF; the fence is a run of backticks at least three long and one longer than the longest run of
  backticks in the text, with the info string `text`; the text follows on its own lines, ending with
  one LF (a text that already ends in LF gets no second one), then the fence. No line of the text can
  close it.

Every other byte of a published text is the script's own: headings, labels, the fixed vocabularies,
dates, the run's URL, the SHA, the tag, the counts and the fingerprints. The image digest is model text
although the schema narrows it to a digest's shape, so it is a `CodeSpan` and a model field. A finding's
location (`file:line`) is model text although the schema narrows its bytes: a file such as
`www.example.com/x` or `a@example.com` would be a link outside a code span, so the location is a
`TableCell` like the scenario and the fix. A tool's version is a `CodeSpan`; its name, from the closed
list, is the script's. The control and invisible
characters above never reach these functions, because `ParseResult` refuses them. An issue's title and an
advisory's summary are fixed templates with no model text (the fourth pull request builds them):
`Security audit finding fp-…: <category> (<severity>)` for an issue, and `Audit finding fp-…
(<severity>, <category>)` for an advisory, `unknown` in place of an unknown value.

**The disclosure table.** `Place(f Finding) Placement` derives each finding's place from its check,
severity, confirmation, category and the model's flag alone, and the first matching row decides:

| # | When | `Placement` |
|---|---|---|
| 1 | `check` is `history-secrets` (gitleaks on the history) | `PlacementAdvisoryHistorySecret`: a draft advisory naming the commit and the rule only, "unconfirmed: maintainer to check" |
| 2 | the severity or the category is unknown | `PlacementAdvisory`, or `PlacementAdvisoryUnconfirmed` when not confirmed |
| 3 | the category is `injection`, `authentication bypass`, `credential exposure` or `remote code execution` | as row 2 |
| 4 | the severity is `critical`, `high` or `medium` | as row 2 |
| 5 | the model's `private` is true | as row 2 |
| 6 | confirmed, low or informational | `PlacementPublic`: the findings table; after the merge, one `security` issue |
| 7 | not confirmed, low or informational | `PlacementInvestigation`: the "needs investigation" list, no issue |

`Placement.Private() bool` is true for the three advisory placements, and for any value that is none of
the five: the zero `Placement` is none of them, so a placement nobody set fails closed. Nothing a finding
holds can move it up this table: the model's flag is read only to make a finding private (row 5).

**Fingerprints.** `Fingerprint` is `fp-` and 12 lowercase hex characters: the first 6 bytes of a digest of
the input `"shift2eu-audit-fingerprint/v1" NUL rule NUL file NUL sink` (NUL is the byte 0x00, which none
of the three fields may hold, so the encoding is unambiguous; the version prefix lets a later change of
the input start a new series on purpose). `PlainFingerprint(rule, file, sink string) Fingerprint` uses
SHA-256, for a public finding and for every false-positive match. `KeyedFingerprint(key FingerprintKey,
rule, file, sink string) (Fingerprint, error)` uses HMAC-SHA-256 with `AUDIT_FINGERPRINT_KEY`, for a
private finding. The key is the secret's value as bytes, unchanged; `NewFingerprintKey(b []byte)
(FingerprintKey, error)` refuses fewer than 32 bytes (`ErrConfig`), the runbook's minimum; a
`FingerprintKey` formats as a placeholder, never as its bytes. A key that was never set, the zero
`FingerprintKey` or any shorter than 32 bytes, is refused (`ErrConfig`, naming no byte) by
`KeyedFingerprint`, which returns no fingerprint beside the error, since an HMAC under an empty key is a
digest anyone can compute, and by the scans, since an empty key would match every text. A private
finding is never given a
plain fingerprint anywhere public, so nobody can test a guess at its rule, file and sink against the
committed script. A finding's line is not an input: a finding keeps its fingerprint when code above it
moves.

**Status and false positives**, derived from the **previous report**: the newest file in the checkout's
`docs/security/audits/` whose name matches `^[0-9]{4}-[0-9]{2}-[0-9]{2}-[1-9][0-9]{0,19}\.md$`, by date
and then run ID as a number: `NewestReport(names []string) (string, bool)` picks it from the directory's
listing, comparing the dates as the name's digits (no calendar check there; `ParsePreviousReport` refuses
a name whose date is no real date) and the run IDs as numbers, and returns false when no name matches.
The checkout is `github.sha` on `main`, so every report read is one the
maintainer merged. `ParsePreviousReport(name string, data []byte) (Previous, error)` reads its findings
table, its needs-investigation list and its false positives by their fingerprint cells and their fixed
vocabulary cells, keeps the three model cells of each `new` or `open` row byte for byte, and returns an
error wrapping `ErrPreviousReport` when one of the parts it checks breaks its rule; with no such file,
the run is the first. It checks exactly these parts, and no other text of the report: the title of the
name's date; the eight `##` sections in order and no other heading; each table's exact header and
delimiter; seven cells per findings row, a fingerprint, a public severity, a public category, a status,
and three cells that are each exactly what `TableCell` writes for a text of printable ASCII
alone (the text rule), or `—` three times on a `fixed` row; one private-findings line in the section and the same one
in the Summary; two cells per False positives row, the reason exactly such a `TableCell`, which is then
carried forward byte for byte; no fingerprint twice. **The first-report rule**: when the Changes section
holds the line "First report.", that line must be the section's only line before the dependencies' line
(`Dependencies changed since the last audit:`), as `RenderReport` writes it; anywhere else, or beside
other lines, it is refused. A first report must not contradict itself either: it holds no status but
`new` and no False positives row that is none of its own findings. The Summary's sentence and counts,
the Scope, the Coverage table, the Changes section's other lines and the Limits are not checked; nothing
of them is carried. `Previous` is opaque and marked as parsed by `ParsePreviousReport` alone; a
`*Previous` that `ParsePreviousReport` did not return, the zero `Previous` or one built inside the
package included, is refused by `RenderReport` (`ErrPreviousReport`). For the kept public findings
(placements 6 and 7):

- not in the previous report: `new`;
- in its findings table as `new` or `open`: `open`, and in Changes since the last audit "still open";
- in its findings table as `fixed`: `open`, and "regressed";
- placed public and only in the previous needs-investigation list: `open`, and "still open";
- in the previous findings table as `new` or `open` and not found in this run: a row `fixed` in this
  report's table (fingerprint, severity, category and status; the other cells `—`), and "fixed". **The
  fixed-row rules**: a finding this run found is no fixed one, whatever happened to it: a finding placed
  private this time (it gets the carried row below), or dropped as a false positive (it keeps its
  carried False positives row), gets no `fixed` row. One the previous report listed and also
  dismissed, which this run does not find at all, does get one;
- **the carried row**: in the previous findings table as `new` or `open`, and placed private in this run
  (the same plain fingerprint): the report keeps a row for it, status `open`, with the previous report's
  severity, category, location, scenario and fix cells byte for byte, and "still open" in Changes. It
  counts in the public total, under the previous severity, and makes its check read `findings`. None of
  this run's private finding's own values show: not its severity, its category or its text. Without the
  row it would vanish without a `fixed` row, and a reader comparing two reports would learn that a
  public finding had become private. **The carried row is still a statistical tell**: a finding that
  stays public is rendered from this run's text, which a model rarely repeats byte for byte, while a
  carried row repeats the previous report's cells exactly. A reader diffing two reports may so infer
  which open finding went private, more surely when the private-findings line changes in the same
  run. The carried cells are model text and model fields, as on any row;
- a needs-investigation entry is `new`, or `open` when either section of the previous report holds its
  fingerprint; it gets no `fixed` row and no Changes line.

A Changes line reads `- fp-…: still open` (or `regressed`, `fixed`), sorted by fingerprint; a run with a
previous report and no such line says "No public finding changed its status.", after the line naming
the previous report's file. A carried row's line counts, so that sentence never stands beside one.

A finding is **dropped** only when its placement is `PlacementPublic` or `PlacementInvestigation` and its
plain fingerprint equals, byte for byte, a fingerprint in the previous report's False positives section.
A match on a medium finding, a private-category finding or any other private placement drops nothing; a
fingerprint that differs in any character is no match. A dropped finding counts nowhere (table, coverage,
issue, total). The False positives section holds the previous report's rows, carried forward unchanged,
then one row for each kept public finding of this run with a non-empty `dismissal` whose fingerprint has
no row yet; a private finding's dismissal is never written.

**The secret and encoded-token scan.** Before anything is sent, the core scans every text the run would
send, all of them built first: the report, the pull request's title and body, the commit message, each
issue's title and body, each advisory's summary and description. Gitleaks scans the same texts in the
entry point; the core adds:

- **Token and key patterns**, over the whole text: `sk-ant-[A-Za-z0-9_-]{20,}` (Claude OAuth tokens and
  API keys), `-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----` (the App's PEM key and any other),
  `gh[pousr]_[A-Za-z0-9]{36,}` and `github_pat_[A-Za-z0-9_]{22,}` (GitHub tokens; no `\b`, so a letter
  written before the prefix, `Xghp_…`, hides nothing),
  `(?i)AGE-SECRET-KEY-1[0-9A-Z]{58}` (the model and publish keys' private halves), the fingerprint key's
  own bytes, and, over decoded bytes only, a DER private key's start, `\x30(\x82..|\x81.|[\x00-\x7f])\x02\x01[\x00\x01]`.
- **The encoded-token check**, over the whole text: every maximal run of at least 16 base64 or base64url
  characters (`=` padding included, `/` included) is decoded with the standard and the URL alphabet,
  padded and raw, dropping one trailing character when the length leaves 1 over 4; every maximal run of
  at least 16 hex characters is decoded, at both even offsets when its length is odd. The decoded bytes
  are matched against the same patterns, and searched once more the same way (a token encoded twice).
  The core reads the two base64 alphabets' runs separately (a `-` next to a `+` would otherwise make a
  run neither alphabet decodes), splits a run after each group of `=` (two encodings written one after
  the other), and decodes it at each of the four alignments, so an encoding that starts inside a longer
  run is found as well; a hex run is decoded at both offsets whatever its length. The DER start is
  matched over bytes, not by a regular expression, which reads runes.
- **The encoded-run refusal**, for **public text only** (the report and the pull request's and issues'
  texts) and only over **model-written fields after escaping**: the renderer returns, with each text,
  the escaped fragments it built from the result. A model field is refused when one of three rules
  holds (`encodedRun`):
  - **The plain class**: a run is a maximal sequence of the bytes `[A-Za-z0-9+=_-]`: every other byte
    ends one, `/`, `.`, `\` and every other separator included, so a path is many short runs, and the
    `\` of an escaped `\|` ends a run as the `|` would. A run of at least `EncodedRunMinLength` bytes
    whose Shannon entropy over its own byte frequencies is at least `EncodedRunMinEntropy` bits per
    character is refused.
  - **The slash class**: the same measure, L and N, of a maximal run of `[A-Za-z0-9+/=_-]`, so standard
    base64, which holds `/`, is one run and not two short ones. It is measured only inside a **word**
    that holds no `.`: a word is a maximal run of non-whitespace bytes, with its trailing ``.,;:!?)]}'"``
    and backticks trimmed first (repeatedly, backticks first), so a secret that ends a sentence or a code
    span is still measured. A word that holds a `.` after the trim, a path such as
    `.github/workflows/claude-code-pin.yml`, is measured by the plain class alone. That is what keeps the
    repository's paths publishable: with `/` in the class, 38 of its 1028 tracked paths held a run that
    reaches N.
  - **The hex rule**: a maximal run of `[0-9a-fA-F]` of at least L characters is refused unless it is
    exactly 40 or 64 characters long (a SHA-1 commit or a SHA-256 digest), and a 40- or 64-character run
    is refused too when its decoded bytes are all printable ASCII (0x20 to 0x7E): a real SHA or digest
    essentially never decodes to that (each byte printable with probability 95/256, so 20 bytes all
    printable about once in 4·10⁸), and a token body written as hex always does.

  Values the script inserts are never model fields: the `main` SHA, the release tag, the run's URL, the
  dates and the fingerprints.
- **L, the minimum length, is 24.** A fingerprint is 15 characters, so L is longer, as the PRD asks; 24 is
  the padded standard base64 length of a 128-bit secret, the smallest worth stealing, and every
  credential the patterns name is longer. Raw or URL base64 of the same 16 bytes is 22 characters, below
  L, so it is not refused (a residual risk, below). Below L, ordinary words and identifiers would trip
  the entropy test.
- **N, the entropy, is 4.1 bits per character.** Single-case hex has 16 symbols, so its entropy never
  exceeds 4.0: the entropy classes never refuse a lowercase SHA or digest, which the hex rule judges
  instead. Mixed-case hex has up to 22 symbols and is refused by the plain class when it reaches N. The rates,
  measured over 200,000 random samples per row, of the refusal reaching a random encoding:

  | Encoding | Length | Class that measures it | Refused |
  |---|---|---|---|
  | base64url, raw, 16 bytes | 22 | plain | 0% (below L) |
  | base64url, raw, 18 bytes | 24 | plain | about 85% |
  | base64url, raw, 21 bytes | 28 | plain | about 98% |
  | base64url, raw, 24 bytes | 32 | plain | about 99.9% |
  | base64url, raw, 32 bytes | 43 | plain | 100% |
  | standard, padded, 16 bytes | 24 | slash | about 80% |
  | standard, padded, 21 bytes | 28 | slash | about 98% |
  | standard, padded, 24 bytes | 32 | slash | about 99.9% |
  | standard, padded, 32 bytes | 44 | slash | 100% |
  | standard, padded, 16 bytes, a `.` in its word | 24 | plain | about 57% |
  | standard, padded, 32 bytes, a `.` in its word | 44 | plain | about 89% |
  | standard, padded, 48 bytes, a `.` in its word | 64 | plain | about 98% |

  A Claude OAuth token, about 100 characters, is always refused. A long identifier with no separator (a
  34-letter camel-case test name can reach 4.2), or a dot-free path-like word of high entropy
  (`abcDEF1234/ghiJKL5678/mnoPQR9012`), also trips it: the refusal then fails closed on a harmless text,
  which costs a run, never a disclosure. **The hex and slash rules cost the same way**: a model field
  that quotes a 32-hex MD5, an undashed UUID (32 hex), an 80-hex Rekor entry UUID, a SHA-384 or
  SHA-512 hex digest (96 or 128 hex) or a go.sum `h1:` hash (standard base64 of 32 bytes in a dot-free
  word) refuses the whole run. The fourth pull request's
  model-job prompt tells the model not to quote them.
- **Tightening the scan needs a manual edit of the latest report.** Every cell of the latest report is
  checked again by the parse (`escapedCell`, the text rule), and a carried cell and a carried False
  positives reason also by the scan, as model fields, in every later run. A later, stricter rule
  refuses, through the parse, any cell of the latest report (a narrower text rule) and, through the
  scan, a carried cell or a False positives reason (a lower L or N, a new pattern), cells an earlier
  report published; since every later run reads them, every later run is refused. The change that tightens a rule
  therefore also edits the latest merged report, in the same pull request, so that its carried cells
  pass the new rule.
- **Advisory text** gets the patterns and the encoded-token check, **not** the refusal: an advisory holds
  PoCs and crash inputs, which are often base64.
- `ScanPublic(body string, modelFields []string, key FingerprintKey) error` and `ScanAdvisory(body
  string, key FingerprintKey) error` return an error wrapping `ErrScanRefused` on any hit, which names no
  text. `ScanPublic` applies the patterns and the encoded-token check to the body and to each model
  field too, so a field is scanned even where the caller's body leaves it out. **On a hit nothing is
  published**, not the report, not an advisory, not an issue, and the summary is exactly `publish
  refused: see runbook`.
- **Residual risk, encoded tokens.** The scan finds a token in the clear, in base64 or hex, or encoded
  twice that way, and refuses high-entropy and suspect hex runs in public text. It does not find:
  - a token split across fields or runs: a separator every 23 characters defeats both entropy classes,
    and a hex chunk of 23 characters or fewer, or one of exactly 40 or 64 holding a single non-printable
    byte, passes the hex rule;
  - a secret below L: raw or URL base64 of 16 bytes is 22 characters and is never refused;
  - a secret that misses N by chance, at the rates of the table above;
  - **a `.` inside a base64 secret's word**: it drops the word back to the plain class, which splits it
    at every `/` and refuses a 32-byte secret about 9 times in 10, not always;
  - a token encoded any other way (base32, percent-encoding, a cipher, a compression);
  - **any secret's raw bytes as 40- or 64-hex runs**: a secret transformed so that at least one byte of
    each 20 or 32 is non-printable (the high bit set, an XOR with a known byte) passes the hex rule as a
    commit or a digest, `image_digest` included. Each run carries 20 or 32 bytes, and a field may hold
    any number of them. The fourth pull request's entry point could require each 40-hex run in a model
    field to name a commit of the repository, which leaves the 64-hex runs;
  - **ASCII spacing**: which runs of U+0020 a model writes is a channel (TAB and LF in a multi-line
    field become U+0020 in a code span): inside a code span the extra spaces show only as a slightly
    wider gap, which the eye does not read. In a code block (the fourth pull request's advisory and
    issue bodies, `CodeBlock`) TAB against spaces and trailing spaces on a line also carry bits; that
    text goes to private advisories and issues, not to the public report.

  A model steered to leak the subscription token could so reach the public report. The model step's
  containment is what keeps the token from the model (Decision 6); the scan is the last line, and runbook
  step 10 rotates the token.
- **Residual risk, fingerprint grinding.** A public finding's fingerprint is the script's own value and no
  model field, but the model chooses its inputs: rule, file and sink. By trying inputs until the plain
  fingerprint's 48 bits begin with bytes it chose, a model can write about 3 chosen bytes (2²⁴ tries
  each, cheap) into every public row's fingerprint, past every scan: up to about 300 bytes in one run at
  the finding limit, enough for a token. The schema's patterns for rule, file and sink, the
  severity floor and the maintainer's review of each report bound it; nothing closes it, and the model
  step's containment is again what keeps the token from the model.

**The coverage vocabulary.** The coverage table lists every check id, in this order, by its fixed name:

| Check id | Check (as rendered) | PRD row |
|---|---|---|
| `dependencies` | govulncheck (source) and osv-scanner | 3 |
| `history-secrets` | gitleaks on the full history | 4 |
| `workflows` | zizmor and actionlint | 5 |
| `scorecard` | OpenSSF scorecard | 6 |
| `scorecard-admin` | OpenSSF scorecard checks that need admin read | 6 |
| `release-integrity` | Release checksums, attestations and signatures | 7 |
| `image-pull` | Image pulls from GHCR | 8 |
| `binary-vulnerabilities` | govulncheck on the release binaries | 9 |
| `image-scan` | trivy on the image and its SBOM | 10 |
| `image-runtime` | The image's runtime settings | 11 |
| `hostile-inventories` | The CLI on hostile inventory files | 13 |
| `fuzzing` | Fuzzing of the committed `Fuzz*` targets | 14 |
| `supply-chain` | Supply-chain reads | 15 |
| `tree-review` | The security-audit checklist on the whole tree | 18 |
| `agent-surface` | What the agents may run | 18 |

A check reads `findings` when it ran and a kept finding with that `check` is placed `PlacementPublic` or
`PlacementInvestigation`; `run` when it ran otherwise, its only findings private included; and
`not run (<why>)` when it did not, the why rendered from its id:

| Why id | Rendered |
|---|---|
| `needs-admin-read` | `not run (needs admin read)` |
| `no-fuzz-targets` | ``not run (no `Fuzz*` targets)`` |
| `timed-out` | `not run (timed out)` |
| `tool-failed` | `not run (tool failed)` |
| `results-missing` | `not run (results missing)` |
| `read-refused` | `not run (read refused)` |
| `image-not-pulled` | `not run (image not pulled)` |
| `declined` | `not run (declined)` |

A finding whose `check` did not run is refused with the result (`ErrResultInvalid`).

**Rendering.** `RenderReport(in ReportInput) (Rendered, error)`, with `ReportInput{Result Result; Run
Run; Private PrivateState; Previous *Previous}` (`Previous` nil for the first report) and
`Rendered{Path, Body string; ModelFields []string}`, writes `docs/security/audits/YYYY-MM-DD-<run_id>.md`
(the run's UTC date, its run ID). `ModelFields` lists every escaped fragment of model text the body holds
(locations, scenarios, fixes, reasons, tool versions, the image digest, dependencies, the narrative,
limits, and a carried row's cells), never a value the script inserts and never a finding's evidence. `RenderReport` checks the result's values and the run
as `ParseResult` and `NewRun` do. Its first line is the one title, `# Security audit report, YYYY-MM-DD`, and its sections are the
template's, in its order, each a `##` heading: Summary, Scope, Findings (with `### Needs
investigation`), Private findings, Coverage, Changes since the last audit, False positives, Limits. Its
only links are `[Security](../README.md)` in the summary and the run's URL in the scope, so
`test/docs`' title and link checks pass on it; model text is in code spans, which those checks skip.

- **Summary**, fixed text: a sentence naming the commit and the tag, with the `[Security](../README.md)`
  link, then the items `- Public findings: <total> (<n> <severity>, ...)` (each severity with a kept
  finding, most severe first; no parenthesis at 0), `- Listed under needs investigation: <n>` and `- `
  and the private-findings line. The tests pin the content, not this wording.
- **Scope**: Commit, Release (tag and the image digest as a `CodeSpan`, or "image not pulled"), Tool versions (one item per
  tool: its name and its version as a `CodeSpan`), Model (`ModelName` `claude-fable-5-2`, `ModelEffort`
  high: the core's constants, not the result's), Run (the run's URL as a link, and the date).
- **Findings**: the table `Fingerprint | Severity | Category | Status | Location | Scenario | Recommended
  fix`, sorted by severity, most severe first (critical, high, medium, low, informational, the schema
  table's order; only the last two can reach it), then by fingerprint, `fixed` rows sorted in with the
  others; the location `file:line` (or `file` when the line is 0) and every model cell a `TableCell`;
  "No public findings." when empty. **Needs investigation**: the same columns and order, "None." when
  empty.
- **Private findings**: exactly one of `private findings: handled privately`, `private findings: not
  filed` or `private findings: none`, from `PrivateState` (`PrivateNone`, `PrivateHandled`,
  `PrivateNotFiled`), which the entry point sets after filing; `PrivateNone` with any private placement,
  another state with none, or a value that is none of the three is an error wrapping `ErrConfig`, from
  `RenderReport` and `RenderTemplates` alike.
- **Coverage**: the table `Check | Coverage`, every check id in the order above.
- **Changes since the last audit**: the per-fingerprint lines above, then the gap line when there is a
  gap, then the dependencies and the narrative as `CodeSpan`s; "First report." when there is no
  previous report.
- **False positives**: the table `Fingerprint | Reason`, "None." when empty. **Limits**: one item per
  limit, "None." when empty.

The fixed templates, `RenderTemplates(in ReportInput) (Templates, error)` with `Templates{Branch,
PRTitle, PRBody, CommitMessage}`, derived from the same input as the report so their total is the
report's, filled only with the run's UTC date, the run ID, the `main` SHA, the release tag, the public
total, the private-findings line and the run's URL, never with model text (the body's items are plain
lines, not a list, and neither the body nor the commit message ends with a newline):

- branch `security-audit/YYYY-MM-DD-<run_id>`;
- pull request title `Security audit report, YYYY-MM-DD (run <run_id>)`;
- pull request body: `Security audit report of run <run_id>, YYYY-MM-DD (UTC).`, a blank line, then the
  items `Commit: <sha>`, `Release: <tag>`, `Public findings: <total>`, the private-findings line and
  `Run: <url>`, a blank line, and `The report adds docs/security/audits/YYYY-MM-DD-<run_id>.md and nothing
  else.`;
- commit message `docs(security): audit report YYYY-MM-DD, run <run_id>`, a blank line,
  `Commit <sha>, release <tag>, public findings <total>, <private-findings line>.` and `Run: <url>`.

The run's URL is `<GITHUB_SERVER_URL>/<GITHUB_REPOSITORY>/actions/runs/<GITHUB_RUN_ID>`, built by
`NewRun(serverURL, repository, runID, date string) (Run, error)`, which refuses (`ErrConfig`) a server URL
other than `https://` and a host name, a repository other than `owner/name` of `[A-Za-z0-9._-]`, a run
ID other than a decimal of up to 20 digits with no leading zero, and a date other than a real
`YYYY-MM-DD`. The public total counts the kept findings placed `PlacementPublic` or
`PlacementInvestigation`, and the carried rows.

**The gap threshold** is one pinned constant, `GapThresholdDays = 38` (the longest month, 31 days, plus 7
days of slack, for the cron `17 4 3 * *`). The Changes section says `Gap in the schedule: the previous
report is <n> days older than this one.` when the previous report's name's date is more than 38 days
before the run's date, counted in whole UTC days from the two dates' Unix seconds, `(run − previous) /
86400`, so a previous date in the year 0001 gives its true gap and not `time.Duration`'s saturated 292
years. A previous report dated after the run is refused (outcome 7), because `NewestReport` would
otherwise pick it on every later run and suppress the gap check. A previous report on the run's own date
whose run ID is equal to or above the run's own is refused too (outcome 7, `ErrPreviousReport`,
`previous report: not before the run`): run IDs grow with time, so it is this run's or a later run's.
Re-runs are skipped (`run_attempt == 1`), so it needs a hand-made or a later run's report merged first.
A `test/architecture` check reads the core's source
and fails `make check` unless `GapThresholdDays` is declared exactly once, as an integer literal, and
equals 38 while the workflow has no `schedule` trigger. Milestone 3, which adds the trigger, changes it
to the PRD's full check: at least the longest gap between two consecutive dates the cron names plus 7
days, for the cron forms `M H D * *` and `M H * * W` only, and the same number in the staleness check of
the `release-tag` and `dispatch` skills.

**Exit codes and the summary line.** The entry point prints exactly one line, to standard output and to
the step summary, chosen by `OutcomeOf(err error) Outcome` from the error's class, never from its text,
and exits with `Outcome.Code()`; `Outcome.Summary()` holds no finding's text, no count of private
findings, no API response and no error message. `OutcomeOf(err)` cannot know a published run's counts,
so the entry point's success path is `OutcomeOf(nil).WithCounts(total, state)`; `WithCounts` leaves any
other outcome as it is, a published outcome whose private findings were not filed is
`OutcomePrivateNotFiled` (exit 5), and a published outcome whose counts were never set, or set to a negative total
or an unknown state, fails closed as `OutcomeFailed`. Outcomes 5, 6 and 8 come from the entry point's
own error classes, `ErrPrivateNotFiled`, `ErrDuplicateDrafts` and `ErrGitHub`, which the core declares
so that `OutcomeOf` selects every row by class; an error wrapping two classes gets the first in the
order scan, result, private not filed, duplicate drafts, previous report, GitHub, configuration. The
outcome names below are the core's internal kinds:

| Outcome | Code | Summary |
|---|---|---|
| `OutcomePublished` | 0 | `audit published: public findings <total>, <private-findings line>` |
| `OutcomeFailed` (any other error) | 1 | `publish failed: see runbook` |
| `OutcomeResultInvalid` (`ErrResultInvalid`, a result that does not decrypt included) | 3 | `result refused: see runbook` |
| `OutcomeScanRefused` (`ErrScanRefused`) | 4 | `publish refused: see runbook` |
| `OutcomePrivateNotFiled` (`ErrPrivateNotFiled`, or a published run whose private findings were not filed) | 5 | `private findings not filed: see runbook` |
| `OutcomeDuplicateDrafts` | 6 | `publish refused: duplicate draft advisories: see runbook step 11` |
| `OutcomePreviousReport` (`ErrPreviousReport`: a previous report that is unreadable or not before the run) | 7 | `previous report unreadable: see runbook` |
| `OutcomeGitHub` | 8 | `GitHub call failed: see runbook` |
| `OutcomeConfig` (`ErrConfig`) | 9 | `configuration invalid: see runbook` |

Outcome 7's summary names the class, never which case broke it, so the fourth pull request's runbook
step for outcome 7 lists both cases, the unreadable previous report and the one not before the run
(dated after it, or on its date with a run ID not below its own).
Code 2 is left to the Go runtime's panic exit. Only `OutcomePublished` and `OutcomePrivateNotFiled` have
published anything; the second still fails the job, so GitHub notifies the maintainer. The errors the
core returns name a field's path (`findings[3].scenario`) and the rule it broke, never a value, and the
entry point prints none of them.

**Left to the fourth and fifth pull requests**, which the core's choices depend on:

- **The fourth (the entry point)**: a finding's `evidence` goes only into its draft advisory, never into
  a public issue's body, which is public text the encoded-run refusal would otherwise have to read; and
  the entry point checks `image_digest` against the release's attested image digest before trusting it,
  since the schema checks only its shape. Two notes from the encoded-run refusal: the entry point could
  require each 40-hex run in a model field to name a commit of the repository, and the model job's
  prompt tells the model not to quote an MD5, an undashed UUID, a Rekor entry UUID, a SHA-384 or
  SHA-512 hex digest or a go.sum hash. From the text rule, the prompt also tells the model to write
  ASCII only, since a quoted non-ASCII character refuses the run. From the gitleaks refusal (below),
  the prompt tells the model never to write gitleaks' inline allow comment literally, in any letter
  case, and to name it in words instead; the prompt itself never carries the marker.
- **The fifth (the workflow)**: only the audit App's `security-audit/*` branches may change
  `docs/security/audits/`, enforced by CODEOWNERS or a required check. Otherwise a merged edit from
  another branch that adds a False positives row would drop a public finding from every later run,
  silently, since the row is carried forward byte for byte.

### The publish program's GitHub side (milestone 2, amended 2026-11-23)

This subsection settles the fourth pull request's frame: the entry point, its GitHub side, how it calls
GitHub and how that is tested. The workflow that runs it, `.github/workflows/scheduled-security-audit.yml`,
its jobs, `age` and the schedule are the fifth pull request's; the behaviours below the frame (the
report's Git data, the issues, the advisories' tracking) are built test-first on it, and this record
gains each one's details with the change that builds it.

**Packages.** The publish program is three packages, each held by a strict depguard rule of named
standard packages, each entry ending in `$`, pinned by `test/architecture`:

- `ci/auditpublish`, the **entry point**, a main package (`audit-publish-main`: `context`, `errors`,
  `flag`, `io`, `os`, `os/signal`, `time`, the GitHub side and the core; `os/exec` and `net`
  denied). It reads the step's environment through one `os.LookupEnv` passed to `run` (the forbidigo
  group `audit-publish-main-env`, its exception in `main.go` alone), reads the clock for the run's UTC
  date, prints the one summary line and exits. It starts no program.
- `ci/auditpublish/internal/github`, the **GitHub side**, a library (`audit-publish-github`: `bytes`,
  `context`, `encoding/base64` (an image attestation's DSSE payload, PR 4b), `encoding/json`, `errors`, `fmt`, `io`, `os`, `os/exec`, `path/filepath`, `regexp`,
  `slices`, `strconv`, `strings`, `time` (the deadlines below) and the core; `net` denied). It runs
  `gh` and gitleaks, reads the result and the merged reports, and reads no environment variable at all
  (the forbidigo group `audit-publish-github-env`, which holds for its tests too; its one exception is
  `os.Environ` in `fake_test.go`, where the fakes record the environment the library gave them; it also
  refuses `exec.Command` and `exec.LookPath`, so every program starts by absolute path through
  `exec.CommandContext`; the rule matches those constructors only, not an `exec.Cmd{Path: ...}`
  composite literal followed by `Start` or `Run`, which review holds out: nothing uses one). The
  process starts sit in the library, not in the entry point, so that the endpoint check and the `gh`
  run are one function the tests drive against the fake.
- `ci/auditpublish/internal/publish`, the **core**, unchanged; its rule `audit-publish-library` now
  names `ci/auditpublish/internal/publish/**` instead of the whole `internal/` tree, which the GitHub
  side shares.

**The subcommands.** `auditpublish artifacts -job model|publish -gh <path> -tmp <dir>` is the artifact
check both downloading jobs run before they download: one listing of the run's artifacts, refused unless
it is exactly `checks-result` (the model job) or exactly `checks-result` and `model-result` (the publish
job): an artifact the job does not expect, a name twice, an expected artifact missing, an expired one or
a `total_count` above the listed count (a second page) is refused, and so is an answer with any data
after the one listing (`ErrGitHub`, outcome 8: whitespace alone may follow it). Its client is
**read-only by construction**: the entry point builds it only through `github.NewArtifactsClient`,
which refuses what `NewClient` refuses and sets `ClientConfig.ReadOnly` whatever its caller passes (a
source check pins that `main.go` never calls `NewClient`, since no behaviour test can tell a dropped
flag apart: the check only reads). Every method but `GET` is refused with `ErrEndpointRefused`
before the endpoint table is consulted and before `gh` runs. The table is the publish job's, wider than
one listing needs; a client that may only read keeps the artifact check, whose step holds the job's
`github.token`, no wider than the subcommand needs. It prints `artifacts: as expected`,
`artifacts: unexpected: see runbook step 11` (exit 3), or the core's GitHub and configuration lines
(exits 8 and 9). The job sets the expected names, not a caller's list: a step cannot widen them.

`auditpublish image -result <path> -gh <path> -tmp <dir>` (PR 4b) checks the result's release and image
digest before `publish` trusts them: `CheckRelease` (row 2: 200 with the tag's `tag_name`, 404 is
`ErrResultInvalid`; then `R/releases/latest`, also row 2, whose `tag_name` must be the result's tag:
another tag or no latest release is `ErrResultInvalid`, an answer that is no release `ErrGitHub`, PR 5b
review round 1) and `CheckImageDigest` (row 3, below). It is a subcommand of its own, run in its own
step with the job's `github.token` and `attestations: read`, because the audit App has no Attestations
permission and is not widened for it (maintainer, decision 2 of PR 4's review). Its client is read-only
by construction, `github.NewImageClient`, as the artifact check's is. It prints `image digest: as
expected` (exit 0) or the core's `result refused` (3), GitHub (8), configuration (9) and failed (1)
lines.

`auditpublish publish -result <path> -reports <dir> -gh <path> -gitleaks <path> -tmp <dir>` (PR 4b) is
the publish job's program, built through `github.NewPublishClient`, which refuses a scope without the
audited commit; its summary line and exit code are the core's `Outcome`, with the report's public total
and the private state as its counts. It runs `github.Publish`, in this order: it reads the result
(`LoadResult`, the audited commit `GITHUB_SHA`) and the previous report (`LoadPrevious`, below) before
any call; checks the release (row 2) and lists the App's issues (row 12); renders the report, the
templates and the issues; scans every text; and only then writes the report and the issues. The release
check runs again here, under the App's token, so a `publish` started without the `image` step still
refuses a tag that names no release; the digest check is `image`'s alone.

A usage error (no or an unknown subcommand, a flag it cannot parse, an extra argument, an unknown job)
writes one constant usage text to stderr, holding no value, and is outcome 9. Every outcome, the refusals
included, appends its one line to the step summary as well as standard output. The summary is opened
for appending once, before any `gh` call; the runner makes the file, so the run never creates it. When
`GITHUB_STEP_SUMMARY` is unset or not absolute, or the file cannot be opened, written or closed, the run fails
closed: a run that would have passed prints the configuration line instead and exits 9 (the summary is
part of the step's configuration), and a run that already failed keeps its own line and code. Either way,
standard output holds the run's one line. One case is not exact: when the line was written but closing
the file fails, a would-be pass exits 9 and prints the configuration line, while the summary may already
hold the pass line (the write may have reached the file before the close reported the error). The code
stays fail-closed on purpose: the exit code is what the workflow acts on, and the job fails; a reader of
the summary who sees a pass line on a failed step reads the step's standard output, which holds the
configuration line.

**Deadlines and signals.** Each `gh` call and each gitleaks scan runs under a deadline of its own,
`ClientConfig.CallTimeout` and `GitleaksConfig.Timeout`, 2 minutes when unset, through the context
`exec.CommandContext` is given. A `gh` call whose context ended (its deadline or the run's cancellation)
is `ErrGitHub` (outcome 8) whatever `gh` printed, a status line included: an answer read after the
context ended is never trusted. A gitleaks scan whose context ended is a scan that did not run to its
end, `publish.ErrScanRefused` (outcome 4), never "no leak". The entry point stops on SIGINT
(`signal.NotifyContext`), which cancels the run's context and so every running call. SIGINT alone is
enough: the Actions runner cancels a step with SIGINT first, then SIGTERM after a grace period, then a
kill, so SIGINT is the signal that reaches the program while it can still act. It does not handle
SIGTERM: naming it needs `syscall`, which `TestAuditPackagesAreHeldByStrictRules` denies every audit
package, as for `ci/auditrun`. If the step outlives the grace period, the runner kills its process
tree, and each call's and scan's own deadline bounds the run meanwhile. The fifth pull request sets
`timeout-minutes` on every job of the workflow, so a hung step cannot hold the runner beyond the job's
bound.

A POST or PATCH whose call ended at its deadline or on a signal has an **unknown** effect, not none:
GitHub may have applied it before the answer was lost. The program therefore never retries a mutating
call within a run (built in PR 4b for every POST; PR 4c's PATCH goes through the same helper): every
write goes through one helper, which makes one call and turns a non-2xx status or an ended context into
`ErrGitHub`, and no write follows a refused one. The next run's listing (the bot's issues, the App's
draft advisories) reconciles with what is already there before it writes. Fake-`gh` tests pin both: a
POST route that answers only after a delay past the call's deadline is called exactly once with nothing
after it, and each write of the report (blob, tree, commit, ref, pull request) and an issue answering
422 stops the run there.

**The environment.** The entry point reads `GH_TOKEN`, `GITHUB_SERVER_URL`, `GITHUB_REPOSITORY`,
`GITHUB_RUN_ID`, `GITHUB_STEP_SUMMARY`, for `image` also `GITHUB_SHA`, and for `publish` also
`GITHUB_SHA` and `AUDIT_FINGERPRINT_KEY`, and no other value. `image` and `publish` refuse (outcome 9)
a `GITHUB_SHA` that is not 40 lowercase hex, `publish` an `AUDIT_FINGERPRINT_KEY` unset or under 32
bytes, and both a `GITHUB_SERVER_URL` other than exactly `https://github.com` (a trailing slash, another
host, `http://` and an unset one included): the report, the commit message and the pull request name the
run's URL on that server, and an attestation's workflow names its repository there. It refuses to run (outcome 9) when `AUDIT_APP_PRIVATE_KEY`,
`AUDIT_MODEL_PRIVATE_KEY`, `AUDIT_PUBLISH_PRIVATE_KEY` or `CLAUDE_CODE_OAUTH_TOKEN` is set at all, reading
only whether it is: none belongs in its step. `GH_TOKEN` is passed through to `gh` and nothing else:
`gh` takes its token from its environment, and the library builds that environment itself, so the value
is copied in as the `GH_TOKEN` entry, held in a type that formats as a placeholder, and never printed,
compared or written. The artifact check's and the image check's steps get the job's `github.token`
(the App has no Actions read and no Attestations permission); the publish step gets the App's token.

**The `gh` calling contract.**

- `gh` and gitleaks run by the absolute path of their flags, never looked up on `PATH`, through
  `exec.CommandContext`, with an environment of the library's own and nothing inherited: for `gh`,
  `GH_TOKEN`, `GH_CONFIG_DIR` and `HOME` (one fresh directory under `-tmp`, removed after the run),
  `GH_NO_UPDATE_NOTIFIER=1`, `GH_PROMPT_DISABLED=1` and `NO_COLOR=1`; for gitleaks, `HOME` (its own
  fresh directory) and `NO_COLOR=1`. A variable an earlier step set through `$GITHUB_ENV` (`GH_HOST`,
  `GH_DEBUG`, `HTTPS_PROXY`) reaches neither.
- Every GitHub call goes through one function, `(*Client).Call`. It marshals the request body, checks
  the method, the endpoint and the body against a fixed table (data, below; unexported, and
  `Endpoints()` returns a copy, so no caller can change it), with
  `Allowed`, and only then runs `gh api --method <M> --include -H "Accept: application/vnd.github+json"
  -H "X-GitHub-Api-Version: 2022-11-28" [--input -] <endpoint>`, the body on stdin. A refused call runs
  nothing and wraps `ErrEndpointRefused`, a programming error (outcome 1).
- `--include` puts the status line and the headers before the body on stdout, so the status is read
  from there and never from `gh`'s stderr, which is discarded unread. Every answer with a status line,
  a 404 or any other non-2xx included, is returned as its status, `Link` header and body, with no
  error: the caller decides what a status means (a 404 on the branch lookup is "absent"; the artifact
  check turns any non-2xx into `ErrGitHub`). Output is capped at 8 MiB; a `gh` that fails without a
  status line, or whose output is over the cap, wraps `ErrGitHub` (outcome 8). No response, status
  text or stderr reaches any output.
- Pagination is explicit, `per_page=100` with a page number or the advisories' `after` cursor, and
  bounded at ten pages per listing; a listing over the bound fails closed and is never truncated. For
  the issue listing the bound is the table itself: its row accepts pages 1 to 10 only. The advisory
  listing (row 14) pages by an opaque `after` cursor, which the table cannot count, so its row holds no
  page bound: the ten-page bound lives in its caller (`ListAuditAdvisories`, PR 4c), which stops and fails closed when the
  tenth page's answer still carries a `Link` to a next one, pinned by a fake-`gh` test that answers an
  eleventh `Link`.
- Gitleaks honours inline allow comments: a line holding gitleaks' inline allow comment (its name, a
  colon, then the word allow) is skipped, so a model that wrote that marker beside a token into a
  finding's text would hide it from the scan. The program therefore refuses any text it is about to
  scan that contains the marker in any letter case,
  before gitleaks runs, as a scan that found a leak (`publish.ErrScanRefused`, outcome 4); nothing is
  published. The core's own token patterns do not depend on gitleaks and still run. Since the model
  reads this repository, no tracked file may spell the marker out either, in any letter case
  (`test/lint`'s `TestNoTrackedFileSpellsTheInlineAllowComment`): a line the model quoted would refuse
  the audit's own run. Tests that feed a scanner the marker build it in code from its two parts.

**The endpoint table.** `R` is `repos/<owner>/<name>` of `GITHUB_REPOSITORY`; the run ID, the date, the
branch and the audited commit are this run's own:

| Method | Endpoint | Body |
|---|---|---|
| GET | `R/actions/runs/<run id>/artifacts?per_page=100` | none |
| GET | `R/releases/tags/<tag>` or `R/releases/latest` | none |
| GET | `R/attestations/sha256:<64 hex>?per_page=100` | none |
| GET | `R/git/commits/<40 hex>` | none |
| GET | `R/git/ref/heads/security-audit/<date>-<run id>` | none |
| POST | `R/git/blobs` | exactly `content` and `encoding: utf-8` |
| POST | `R/git/trees` | a base tree and one entry: this run's report path, mode `100644`, a blob |
| POST | `R/git/commits` | a message, a tree, and the audited commit as the one parent |
| POST | `R/git/refs` | `refs/heads/security-audit/<date>-<run id>` of this run |
| GET | `R/compare/<audited commit>...<40 hex>` | none |
| POST | `R/pulls` | this run's branch into `main`, not a draft, no maintainer edits |
| GET | `R/issues?creator=shift2eu-audit%5Bbot%5D&state=all&per_page=100&page=<n>`, `n` 1 to 10 | none |
| POST | `R/issues` | exactly a title, a body and the label `security` |
| GET | `R/security-advisories?per_page=100[&after=<cursor>]` | none |
| POST | `R/security-advisories` | only a summary, a description, a severity and the vulnerabilities: no `state`, `cve_id`, credits or collaborators |
| PATCH | `R/security-advisories/<GHSA id>` | only a summary, a description, a severity and `state`, which is exactly `draft` |

The advisory-create body (row 15) is checked element by element: every element of `vulnerabilities`
holds only `package` (itself exactly `ecosystem` and `name`), `vulnerable_version_range`,
`patched_versions` and `vulnerable_functions`, and `summary` and `description` are required, not only
allowed.

The table cannot bind the PATCH row (16) to the App's own drafts: a GHSA ID matches its pattern whoever
wrote the advisory and whatever its state. The caller (`PlanAdvisories` and `FileAdvisories`, PR 4c)
holds that bound: it PATCHes only a GHSA
that the same run's advisory listing showed with `state == draft` and authored by the audit App, never an
ID from the model's result or a report, pinned by a fake-`gh` test in which the listing holds a published
advisory and another author's draft and no PATCH reaches either.

There is no PUT and no DELETE, and no endpoint that merges or reviews a pull request, requests a CVE,
publishes an advisory, comments on or edits an issue, or touches a tag, a release or another branch.
The issues are matched by listing the App's own issues and reading each one's author and fingerprint,
not through the search API, whose rate limit a run of up to 100 findings could reach.

**The report's publication (PR 4b).** `PublishReport` makes these calls, in this order, none of them
twice:

1. `GET R/git/ref/heads/<branch>`: 404 goes on. 200, an existing `security-audit/<date>-<run id>`
   branch, is `ErrBranchExists`, outcome 1 (`publish failed`), not 8, and nothing is written; any other
   status is `ErrGitHub`.
2. `GET R/git/commits/<audited commit>`: the answer's `sha` must be the audited commit, and its
   `tree.sha` (40 hex) is the base tree.
3. `POST R/git/blobs` with the report's body; 4. `POST R/git/trees` with that base tree and one entry,
   the report's path and the blob's answered sha; 5. `POST R/git/commits` with the templates' message,
   the tree's answered sha and the audited commit as the one parent.
6. `GET R/compare/<audited commit>...<the commit's answered sha>`: the answer must bind the new commit
   to the audited one, status `ahead`, `ahead_by` 1, `behind_by` 0 and `merge_base_commit.sha` the
   audited commit (one commit on top of it, not any history with the same net diff), and its files must
   be exactly one, the run's report path, status `added`; anything else is `ErrReportDiff`, outcome 1,
   and no ref and no pull request follow.
7. `POST R/git/refs`, `refs/heads/<branch>` at the commit's answered sha; 8. `POST R/pulls` with the
   templates' title and body, the branch into `main`, no draft, no maintainer edits.

The object SHAs are bound from the answers, never computed or taken from the plan: the tree's
`base_tree` is the audited commit's tree as step 2 answered it, the commit's `tree` is step 4's answer,
and the compare and the ref name step 5's answer; an answer without its 40-hex sha is `ErrGitHub` and
nothing follows. **The compare comes before the ref**: a commit object exists, and can be compared,
before any ref names it, and only a ref makes a push-triggered workflow run it, so no workflow ever runs
a commit the program has not proved to be the audited commit plus the one report. A refused compare
leaves an unreferenced commit object, which GitHub collects. Fake-`gh` tests pin the order from the
fake's record, each binding with distinct answer SHAs, a compare of two files (and one modified, one
other file, none), and one that is not ahead, two ahead, one behind or on another merge base, each with
no ref after it, and an existing branch with no write at all.

**Issues for merged reports (PR 4b).** A run files issues for the **previous** merged report's public
findings whose status is `new` or `open`, never for its own (the report it opens is not merged yet), and
never for needs-investigation rows or false positives. `ListAuditIssues` reads row 12, open and closed,
page by page while the `Link` header has `rel="next"`; an eleventh page fails closed (`ErrGitHub`).
`PlanIssues` then matches locally (decision 5: listing plus a local match, not the search API): a
finding has its issue when an entry that is no pull request, whose `user.login` is
`shift2eu-audit[bot]` and whose `user.type` is `Bot`, holds the fingerprint in its title, not preceded
by a letter, a digit or `-` and not followed by a hex digit. An outsider's issue carrying the
fingerprint matches nothing, so an outsider cannot suppress an issue; a closed bot issue matches, so a
finding the maintainer closed is not filed again. Each unmatched finding becomes one `security` issue
(`publish.RenderIssue`: a fixed title and body, its location, scenario and fix as code blocks, its
report's path and the run's URL, never its evidence), filed after the report's pull request, in the
table's order; the first refused issue stops the run with the report already published.

**Every scan before the first write.** `Publish` renders every text first and scans each with the
core's `ScanPublic` and gitleaks: the report (with its model fields), the pull request's title and body,
the commit message, and each issue's title and body (with its model fields). Every distinct maximal run
of exactly 40 hex digits, either case, in those model fields must be a commit of the repository
(`CheckCommitRuns`, row 4 answering 200): a 404, and more than 20 distinct runs before any lookup, is
`ErrScanRefused` (outcome 4), which narrows the hex-encoded-secret residual risk above. A hit at any of
these writes nothing: the branch lookup, the first call of the writes' phase, is never made.

**The draft advisories (PR 4c).** After the issue listing, `Publish` lists the repository's advisories
(`ListAuditAdvisories`, row 14, ten pages through the `Link` header's `after` cursor, only the cursor
read), takes the private findings with their keyed fingerprints (`publish.PrivateFindings`) and plans
the advisory writes (`PlanAdvisories`) before anything is rendered: two matchable drafts with one
fingerprint are `ErrDuplicateDrafts` (outcome 6) whatever the findings, and no write follows. A
matchable advisory is the App's (`shift2eu-audit[bot]`, type `Bot`), `draft` or `closed`, with no credit
and no collaborator, and its summary holds a fingerprint. A listing entry whose `credits`,
`credits_detailed`, `collaborating_users` or `collaborating_teams` is missing or `null` is no empty list:
counted as none it would fail open on an advisory a person is on, so it fails the listing closed
(`ErrGitHub`, outcome 8), as a missing `ghsa_id`, `summary` or `state` does. GitHub's schema declares
those four nullable, so PRD #175's runbook step 3 (now in
[`docs/dev-environment.md`](../dev-environment.md#the-scheduled-security-audit)) checks, before milestone 2 goes live, that GitHub
answers arrays for them. A draft with a finding's
fingerprint is updated `open` (a draft marked `not reproduced` that recurs is `open` again); with no
draft, the most recently closed one (by `closed_at`, a tie the listing's first) marked `not reproduced`
makes a new draft `regressed`, any other closed one is dismissed with no write, and no advisory at all a
new draft `new`. A draft no finding holds is marked `not reproduced` by replacing only its last status line
(`publish.SetAdvisoryStatus`), and skipped when it holds none (a maintainer's rewrite). A create sends
`{summary, description, severity (only when known), vulnerabilities}`, its one element this module's Go
package with the other three keys null; an update sends `state: "draft"` and never a severity, so the
maintainer's severity or CVSS on the draft stands. Every advisory summary and description is scanned
with `ScanAdvisory` and gitleaks before the first write. The writes go first: the advisories, then the
report, then the issues. A create answered 403, 404 or 422 as the run's first advisory write is
`ErrPrivateNotFiled`: nothing was filed, no further advisory call follows, the report and templates
rendered beforehand under `private findings: not filed` are published with the issues, and
`Published.Private` is `PrivateNotFiled`, so `OutcomeOf(nil).WithCounts` is outcome 5. The same refusal
after an earlier create or update of the run went through is `ErrGitHub` (outcome 8): the run did file
part of its findings, so a "not filed" report would be false. That run's earlier drafts stand, and the
next run's listing reconciles them. A refused listing (any non-2xx) is `ErrGitHub` (outcome 8), as is
any other failed advisory write, after which no report and no issue are written. The report states
`handled privately` when the run reproduced a private finding, a dismissed one included, and `none`
otherwise. Nothing of a private finding reaches a public text: the evidence goes into its advisory only.

A 422 means an invalid body as well as a refused App, and the program cannot tell them apart without
reading the answer's body, which it never does. The residual risk: GitHub might reject a valid finding's
body, for example as too long, and the run then reports its private findings as not filed (or, after an
earlier write, fails with outcome 8). Today's caps keep the worst-case description at about 57 KB, under
GitHub's 65535; raising a cap must keep it under.

**The reopen race.** The listing and the PATCHes are separate calls. If the maintainer closes one of the
App's drafts between this run's listing and its PATCH (a window of seconds), the PATCH with
`state: draft` reopens it. The REST API has no conditional PATCH, so this is accepted: the runbook tells
the maintainer to close an advisory between scheduled runs, never during one.

**The maintainer's edits.** A reproduced draft's summary and description are rewritten on every run, so
the maintainer's edits to them are overwritten: notes go in the advisory's comments, or in its credits once
the advisory is no longer the App's to update (a credit makes it unmatchable). A reworded summary no longer
holds the fingerprint, so the next run that reproduces the finding creates a new draft. *Added 2026-11-24
(PR 5a):* a credited advisory is unmatchable too, so a run that reproduces its finding files a new
draft. A maintainer's
text in a draft's description that hits the advisory scan or gitleaks (a pasted example token, say) makes
every later run that re-sends that description (the not-reproduced mark, which keeps the listing's text)
fail with outcome 4 (`ErrScanRefused`) before any write, until the text is removed. The not-reproduced
mark replaces only the status line, so the `Run:` line still names the run that last reproduced the
finding: that is intended, since it is the run to look at.

**The image check (PR 4b).** `CheckImageDigest` reads row 3 and accepts a digest when at least one
attestation's DSSE envelope has the payload type `application/vnd.in-toto+json` and its statement
(base64, `encoding/base64`) has the `_type` `https://in-toto.io/Statement/v1`, the predicate type
`https://slsa.dev/provenance/v1`, a subject whose `sha256` is the digest's hex, and a workflow whose
`path` is `.github/workflows/release.yml`, whose `ref` is `refs/tags/<tag>` and whose `repository` is
`https://github.com/<owner>/<name>`; other attestations (an SBOM, an undecodable payload) are skipped.
An attestation the listing returns without an inline `bundle` (only a `bundle_url`) is not fetched: it
matches nothing, so a listing of only such answers is refused. No match, an empty listing and a 404 are
`ErrResultInvalid`. It matches the attestation's statement only: **it does not verify the bundle's
Sigstore signature or certificate chain**. GitHub serves the listing over the authenticated API for
this repository, and the check trusts that answer as it trusts every other API answer; `gh attestation
verify` would verify the signature, but it is a command outside the `gh api` contract above (its own
network calls, its own trust root download), so the program does not run it.

*Residual risk.* Because the statement is not signature-checked, anyone who can run a workflow in this
repository with `attestations: write` (any workflow file on a branch that can be pushed, not only the
release workflow), or otherwise POST an attestation to it, could store a statement that satisfies the
check for any digest. The effect is bounded: a wrong digest is named in the report as the audited
image, and nothing is written on it (the image check's client is read-only, and `publish` uses the
digest only as text of the report). PR 5 closes the gap with `gh attestation verify
oci://<image>@<digest> --repo <owner>/<name> --signer-workflow <owner>/<name>/.github/workflows/release.yml
--source-ref refs/tags/<tag>` as its own workflow step, outside the program's `gh api` contract, as
`vm-tools.yml` already verifies Lima's attestations. *Added 2026-11-24 (PR 5b):* done, the publish job's
`Verify the image's provenance attestation` step (The workflow, below).

**The previous report.** `LoadPrevious` lists `-reports` (the checkout's `docs/security/audits`), picks
the newest name by `publish.NewestReport` and opens only it: a symbolic link, a directory or any other
non-regular file, a file other than the one `Lstat` saw when opened, one over 8 MiB and one the core
refuses are `ErrPreviousReport` (outcome 7); no report name at all is a first run. `LoadResult` reads
the result the same way, capped at the core's `MaxResultBytes`.

**The fakes.** The tests run the library against a fake `gh` and a fake gitleaks, both the test binary
itself, started through a symbolic link named `gh` or `gitleaks` that `TestMain` recognises by its name
(an environment variable could not mark it: the library gives both an environment of its own). The fake
`gh` answers from canned routes (method, endpoint, status, body, `Link`), writes `--include`'s output,
exits 1 on a non-2xx status as `gh` does, records each call's arguments, environment and body, and holds
a guard of its own written from the PRD's list rather than from `Endpoints`: a call outside the PRD's
fixed set, an advisory created with a `state` or updated to any `state` but `draft`, a ref outside
`refs/heads/security-audit/` or a pull request into another branch than `main` is answered 418 and fails
the test. The fake gitleaks reports a leak (exit 10) on a fixed marker and records its stdin. Each writes
an output marker, so a test can show that none of it reaches the program's output: the fake `gh` only to
its stderr, and only on a non-2xx answer (its stdout carries the answer itself), the fake gitleaks to both
its stdout and its stderr on every scan.

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
  (`ci/internal/audit` joined its exceptions in the pull request that added it) and which
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
  | `Token` | the subscription's OAuth token, passed as `CLAUDE_CODE_OAUTH_TOKEN` | refused before anything runs, with `ErrModel` naming the token, never quoting it (*amended 2026-11-23, issue #277 round L*; it was refused only by the CLI) |
  | `Model` | `--model`; letters, digits, `.` and `-` only | refused: no run asks the CLI's default model |
  | `Timeout` | the run's own deadline | refused, as is a negative one, before anything runs, with `ErrModel` naming the timeout (*amended 2026-11-23, issue #277 round L*; a zero deadline had ended the run at once as a timeout) |
  | `Tools` | `--tools`, the built-in tools, joined with `,`; each a tool name of letters only | **no tools**: `--tools ""` |
  | `Effort` | `--effort`; one of `low`, `medium`, `high`, `xhigh`, `max` | no flag: the CLI's default |
  | `Settings` | `--settings <absolute path>` | no flag |
  | `PermissionMode` | `--permission-mode`; `dontAsk` or `plan` only, never `bypassPermissions` | no flag |
  | `AppendSystemPrompt` | the system prompt is appended to the CLI's own (`--append-system-prompt-file`) | it replaces it (`--system-prompt-file`) |
  | `SafeModeOff` | leave `--safe-mode` out | `--safe-mode` |
  | `WorkDir` | the CLI's working directory, an absolute path that exists | a fresh empty temporary directory, removed after the run |
  | `Home` | the CLI's `HOME`, an absolute path that exists | a fresh empty temporary directory, removed after the run |
  | `Env` | extra `NAME=VALUE` entries of the allow list; a name the launcher sets itself (`CLAUDE_CODE_OAUTH_TOKEN`, `HOME`, `PATH`, the four switches below) is refused, and so is one that names another credential, endpoint, proxy, policy or provider or loads code into the CLI's process (`ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, `CLAUDE_CODE_MANAGED_SETTINGS_PATH`, `NODE_OPTIONS`, `LD_PRELOAD`, `LD_LIBRARY_PATH`, `HTTPS_PROXY`, `HTTP_PROXY` and every `CLAUDE_CODE_USE_*`; amended 2026-11-22, issue #277 round C), and, amended 2026-11-23 (issue #277 round D), `LD_AUDIT`, `ALL_PROXY`, `NO_PROXY`, the lower-case `https_proxy`, `http_proxy`, `all_proxy`, `no_proxy` (Node and curl read them too; names are compared exactly), `NODE_TLS_REJECT_UNAUTHORIZED`, `NODE_EXTRA_CA_CERTS`, `CLAUDE_CONFIG_DIR` and `ANTHROPIC_CUSTOM_HEADERS`. *Amended 2026-11-23 (issue #277 round F):* the deny list is replaced by an **allow list**: `Env` may hold only `TMPDIR`, `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB` and `SHIFT2EU_AUDIT_CANARY`, the three the audit sets, and any other name is refused with `ErrModel` naming it, before anything runs. The deny list had missed `BASH_ENV`, `ENV`, `CLAUDE_CODE_SHELL_PREFIX`, `NODE_PATH`, `SSL_CERT_FILE` and `SSL_CERT_DIR`, and a list of refused names misses whatever its authors did not think of; every name above stays refused, the launcher's own entries are unchanged, and the reviewer, which sets no `Env`, is unaffected. *Amended 2026-11-23 (issue #277 round L):* a value holding a NUL or a newline is refused, naming the entry, as `SearchPath` is | none |
  | `SearchPath` | the CLI's `PATH` | `/usr/local/bin:/usr/bin:/bin` |
  | `SystemPromptDir` | the directory the system prompt's file is written into, under a fresh name, an absolute path that exists, which the caller controls (the audit's `/srv/audit/state/tmp`); the file is removed after the run | a fresh temporary directory of the launcher's own under `os.TempDir()`, removed after the run |
  | `Redact` | values, beside the token, that the launcher's error never quotes: each stands in for `[redacted]` in the excerpt of the CLI's text (the audit passes its canary) | the token alone |
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
  text and every `errors.Is` stay as they were. The reviewer's launch is unchanged by the move but for
  one flag: `--mcp-config {"mcpServers":{}}` is added after `--strict-mcp-config`; otherwise the same
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
     `work`, `state/home` and `state/tmp` empty, `bin/audit-exec` with mode exactly 0555 (only its mode
     is checked; its owner, root, is not, and that check is left to PR 5's model-free step, which
     installs it with `sudo install -o root -m 0555`; amended 2026-11-23, issue #277 round H).
     *Amended 2026-11-23 (issue #277 round J):* `bin/` itself has no write bit and holds `audit-exec`
     alone: a second file or a link beside the wrapper is refused.
     *Amended 2026-11-23 (issue #277 round L):* read-only here means read-only **by mode**, and that is
     defence in depth: the runner's user owns `checkout/` and `checks/` and could add a write bit back.
     What holds both trees against their owner is the sandbox's write allow list, which names neither;
     the owner check is left to PR 5's model-free step. Every refusal of this step names what it
     refuses: the path (the wrapper's as `srv/audit/bin/audit-exec`), the entry, the key or the line.
     *Amended 2026-11-22 (issue #277 round C):* both trees are walked whole, no link followed: no entry
     anywhere in them has a write bit, and every symbolic link in them is relative and resolves, its
     whole chain followed, to a path inside its own tree. The checkout copy's `.git` is a directory, not
     a file or a link.
     *Amended 2026-11-23 (issue #277 round D):* the walk also refuses every entry that is not a
     directory, a regular file or such a link: a FIFO, a socket, a device. The checkout copy's git
     configuration is an **allow list**, which replaces round C's list of refused keys (a deny list
     misses every key it does not name, `gpg.program` and `core.pager`'s neighbours among them):
     - `.git/config`, when present, is a regular file that holds only these keys, compared without
       regard to case on the section and the key name: `core.repositoryformatversion`, `core.filemode`,
       `core.bare`, `core.logallrefupdates`; `remote.origin.url`, `remote.origin.fetch`; `gc.auto`;
       `branch.<name>.remote`, `branch.<name>.merge`; `extensions.objectformat`. The subsection
       `origin` is compared exactly, as git compares subsections; `<name>` is any. Every other key and
       every other section is refused, an empty one included: `extensions.worktreeconfig`,
       `[include]`, `[includeIf]`, `gpg.*`, `core.fsmonitor`, `core.pager`, `http.*` and every
       `[filter]`, `[diff]` and `[merge]` section among them.
     - **A strict parser.** Each line must be blank, a comment (`#` or `;`), a section header alone on
       its line, `[section]` or `[section "subsection"]`, or `key = value` under a header. It refuses
       any line it cannot place: a UTF-8 byte order mark; text after a header's closing `]`, a comment
       included; two headers on one line; a quoted subsection not closed by `"]`, or one with an escape
       other than git's `\"` and `\\`; the old dotted header (`[branch.main]`); a key before any header;
       a key without `=` (git's bare boolean); and a value that ends in `\`, which continues on the next
       line. Git writes none of these forms, so refusing them costs a checkout nothing. A refusal
       names the line's number and the section or key, never a value or a subsection, which could hold a
       credential.
     - **Two other files** are refused by their presence, in any file type: `.git/commondir` and
       `.git/gitdir`, the files beside `.git/config` git reads a repository out of.

     The wrapper's git reads that one local config (its environment turns the system and global ones
     off, see the wrapper below), so what that config may hold is exactly the allow list. The check
     makes no claim about what git does with a key; it only holds that no key outside the list is there.
     *Amended 2026-11-23 (issue #277 round E):*
     - **`.git/config.worktree` is taken when harmless.** Every default `actions/checkout` (v7.0.1, the
       pin) leaves it, holding `core.sparseCheckout = false`, `core.sparseCheckoutCone = false` and
       `index.sparse = false`; round D refused it by its presence, so every real run would have failed
       step 3. It may now be there as a regular file (`Lstat`: a link or any other type is refused)
       that passes the same strict parser under an allow list of its own: `core.sparsecheckout`,
       `core.sparsecheckoutcone`, `index.sparse`, compared without regard to case. Any other key or
       section in it is refused, `core.fsmonitor` and `[include]` among them. The three keys pass
       whatever their values: the check holds keys, not values, and `false` is only what checkout
       writes. That is harmless because git reads the file only under `extensions.worktreeconfig`,
       which `.git/config`'s allow list refuses, so the wrapper's git never reads it at all; the check
       holds that it would add nothing if it did.
     - **No lazy fetch.** `remote.origin.promisor` and `remote.origin.partialclonefilter` are off
       `.git/config`'s allow list (round D took them): a `fetch-depth: 1` checkout writes neither, and
       with them the wrapper's git could fetch an object the checkout lacks. A partial clone's config is
       refused.
  4. **Bubblewrap** starts (the user-namespace check, below).
  5. **The preflight** (below).
  6. **The audit**: one `Ask` with the skill's text as the appended system prompt, read from
     `/srv/audit/checkout/.claude/skills/security-audit-run/SKILL.md`, and a fixed user prompt that names
     the audit directory's paths and the result file `/srv/audit/work/result.json`. The transcript goes to
     `/srv/audit/state/out/audit.jsonl` and the CLI's stderr to `/srv/audit/state/out/audit.stderr`,
     never to the log. The step prints one line, "finished" or the failure's constant class.
     *Amended 2026-11-22 (issue #277 round C):* before the run, the work area is checked again: it holds
     its `tmp/` alone, a real directory, and `tmp/` is empty, so nothing the preflight's model wrote and
     no `result.json` that existed before the run is carried into the audit. After the run,
     `result.json` must be a regular file, read capped at 16 MiB, that holds neither the token nor the
     canary; otherwise the run fails with exit 1 and the audit's constant line. No 64-hex rule applies
     to it, as the preflight's judgement applies to its transcript: the audit's findings may quote
     SHA-256 pins, which are 64 lower-case hex characters too.
     *Amended 2026-11-23 (issue #277 round D):* the result file is read from one descriptor and never
     through a link: its entry must be a regular file (`Lstat`, so neither a link nor a FIFO is
     opened), and the descriptor opened must be that same file (`os.SameFile` on its `Stat`). That gives
     `O_NOFOLLOW`'s guarantee with `os` alone: `os` has no `O_NOFOLLOW`, and `syscall` is outside
     `audit-library`, whose packages are linted for every release target. The read is capped at
     `audit.MaxResult` (`int64`, 16 MiB) from that descriptor; a file over it fails with a failure of
     its own, which names the cap in decimal, not with the no-result-file one. The bytes must be one
     JSON value and nothing after it, and the token and the canary are searched for in the raw bytes
     and in every decoded string, the objects' keys included, so a secret written as `\uXXXX` escapes
     is found too; a file that is no JSON is refused. *Amended 2026-11-23 (issue #277 round E):* the
     strings are collected by walking the document's tokens (`json.Decoder.Token`), not by decoding it
     into a map, so every key and every value of a duplicate key is searched: a map keeps a duplicate
     key's last value alone, and a secret under its first would never reach the search. The `Token()`
     walk inherits `encoding/json`'s nesting limit (10000 levels), so a deeper result is refused at
     the token that exceeds it (`exceeded max depth`), as a file that is no JSON is. *Amended
     2026-11-23 (issue #277 round J):* the limit is the audit's own, `maxResultDepth = 10000`: the walk
     counts its depth and refuses a document nested deeper as no JSON, whatever `encoding/json`'s
     internals do. *Noted 2026-11-23 (issue #277 round K):* `encoding/json`'s decoder refuses a
     document 10001 levels deep by itself, so `maxResultDepth` is defence in depth: the rows cannot tell
     it apart from the decoder's own limit from the outside. The check for
     one value and then EOF stays. Both failures still print the audit's one constant
     line. `Audit(ctx, root, in, launch)` returns `([]byte, error)`: the bytes it read and checked,
     never the path. `Steps.Audit` carries them, and `Run.Result func(body []byte) error`, beside
     `Run.Out`, is handed exactly those bytes once, after every step passed, and never for a run that
     failed; a `Result` that fails fails the run with the audit's line. It is the seam the
     publication works from, so nothing reads the result file again by its path: `ci/auditrun`'s
     `Result` writes exactly those bytes to its required `-result-out` path, a new file (`O_EXCL`, mode
     0600) under the model step's `$RUNNER_TEMP`, which the workflow's encrypt step reads (The workflow,
     below; PR 5b, review round 1).
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
  allow rule. `Read(//home/runner/**)` is redundant beside `Read(//home/**)` and stays (issue #277 round
  G): PRD #175 names `/home/runner` explicitly, and `test/architecture` pins the file entry by entry, so
  the rule the PRD asks for is visible and held on its own.
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

- `/usr/bin/git` with `--no-pager`, `-C <a directory under /srv/audit/checkout>` (*amended 2026-11-23,
  issue #277 round H:* git runs only with the wrapper's own `-C /srv/audit/checkout`; a caller's `-C` is
  refused in every form) and one of the subcommands `log`, `show`, `diff`, `grep`, `ls-files`, `ls-tree`, `cat-file`, `rev-parse`, `rev-list`,
  `blame`, `shortlog`, `describe`, `for-each-ref`. It refuses `-c`, `--config-env`, `--exec-path`,
  `--git-dir`, `--work-tree`, `--namespace`, `--ext-diff`, `--textconv`, `--output` and `--output=`
  anywhere on the line.
- `/usr/bin/jq`, `/usr/bin/sha256sum`, `/usr/bin/base64`, `/usr/bin/od`, `/usr/bin/cat`, `/usr/bin/head`,
  `/usr/bin/tail`, `/usr/bin/wc`, `/usr/bin/sort`, `/usr/bin/uniq`, `/usr/bin/cut`, `/usr/bin/tr`: read
  and reshape text, none writes a file of its own or opens a connection (`sort -o` and `--output` are
  refused). *Amended 2026-11-22 (issue #277 round C):* sort takes an **allow list** of options alone,
  `-b -d -f -g -h -i -k -M -n -r -R -s -t -u -V -z`, `--key=`, `--field-separator=` and the long forms of
  the allowed short ones, taken whole (no abbreviation); a short cluster may combine only allowed letters,
  and `-k` and `-t` take their value attached or as the next argument. Everything else is refused,
  `--compress-program` and every abbreviation of it, `--files0-from`, `--random-source`, `-T`,
  `--temporary-directory`, `-o` and `--output` included. uniq takes one operand at most: its second is
  OUT, a file it writes.
  *Recorded 2026-11-23 for PR 5 (issue #277 round J):* the option analysis of these commands assumes
  **GNU coreutils**. Ubuntu 25.10 and later ship uutils coreutils by default, whose option parsing
  differs in places, so a move of `ubuntu-latest` would change the binaries under `/usr/bin` unseen.
  PR 5's workflow must pin the runner image (`runs-on: ubuntu-24.04`) and its model-free step must check
  that each listed coreutil under `/usr/bin` is the GNU one (its `--version`) before the model runs.

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
  caller's. *Amended 2026-11-22 (issue #277 round C):* git reads a cluster of short options one letter at
  a time, so any single-dash word that holds `c` or `O` anywhere is refused (`-iOsh` runs `sh`, `-nc`
  takes the next word as a configuration value), not only one that starts with it; a word after `-e`
  that happens to hold them is refused too, which fails closed. sort's options are the allow list above,
  and uniq's second operand is refused.
  *Amended 2026-11-23 (issue #277 round D):* git's signature verification runs gpg (`gpg.program`), so
  it is refused in both its forms: `--show-signature`, alone, with `=value` and abbreviated (`--show-sig`)
  as the other long options are; and a format that holds a signature placeholder, `%G` (`%G?`, `%GS`,
  `%GK`, `%GG`, ...) or for-each-ref's `%(signature...)`, when it is the attached value of `--format=` or
  `--pretty=` (or an abbreviation of either), a `format:` or `tformat:` value, or the next word after
  `--format` or `--pretty`. `--format=%H%n%s` and every other format still pass.
  *Amended 2026-11-23 (issue #277 round E):* round D's check looked only at format options, and missed
  git's modifiers (`%+G?`, `%-GS`, `% GK`), shortlog's `--group=format:%G?`, an abbreviation such as
  `--gr=format:%GS` and for-each-ref's sort atom. It is replaced by a check of **every** git argument
  word, wherever it sits on the line: a word that holds `%` followed by an optional `+`, `-` or space
  and then `G`, or that holds `%(signature`, is refused; and on a `for-each-ref` line, any word that holds
  `signature` (`--sort=signature`, `--sort=-signature`, `--sort signature:grade`, attached, negated,
  abbreviated or as the next word). The check reads a word whatever option it belongs to, so it
  over-refuses and fails closed: a grep pattern or a `-S` value holding `%G`, or a for-each-ref pattern
  holding `signature`, is refused too, and the model has no way to pass one. `git log --format=%H%n%s`
  and `for-each-ref --sort=-committerdate` still pass.
- *Added 2026-11-23 (issue #277 round F):* **The wrapper's path rule.** The settings file's
  `Bash(*/proc/*/environ*)` deny rule matches the command's text, so a glob (`audit-exec cat
  /proc/[0-9]*/envir?n`) gets past it and the wrapper then receives the real paths the shell expanded.
  The wrapper therefore refuses, on the line of every command it runs, any word that names a path into
  `/proc`, `/sys` or `/dev`. A word names a path when it is (a) no option word (no leading `-`; `-` alone
  is stdin) and holds a `/`, so a bare grep pattern, ref or jq filter names none, and a word without a
  `/` cannot reach one of the three from the work directory; (b) an option word holding `=`, whose value
  after the first `=` is the path (`--field-separator=/proc/x`); or (c) on a git line, the value of `-C`,
  attached (`-C/proc/1`) or the next word (superseded by round H, below: a caller's `-C` is refused
  outright). A path is refused when, read from `/` if it is relative and
  cleaned lexically as `filepath.Clean` cleans it, it is `/proc`, `/sys` or `/dev` or lies under one of
  them, so `/proc/../proc/1/environ` and `../../proc/1/environ` are refused and
  `/srv/audit/work/proc/x` passes. The wrapper does the cleaning itself with `strings`, since its depguard
  rule does not take `path/filepath`. The refusal is the others': exit 126 and the line naming the whole
  word. The rule reads every word whatever option it belongs to, so it fails closed as the signature
  check does: a git grep pattern that reads as such a path is refused too, while `git grep -e proc` passes.
  *Amended 2026-11-23 (issue #277 round G):* round F's deny list of roots leaked six ways: a `-`-led
  operand after `--`; `/dev/fd/../../1/environ`, where the kernel follows `/dev/fd` into `/proc/self/fd`
  before it takes the `..`, so a lexical clean does not show where the path leads; an attached
  short-option value (`git grep -f/sys/x`); `/home`, `/etc` and `/srv/audit/state`, which it never
  checked; `git diff --no-index` (and `git diff` of two absolute paths outside the checkout, which runs
  no-index on its own: the path allow list below keeps both inside the allowed roots, and the explicit
  `--no-index` refusal is for clarity; see round L's implicit no-index diff); and the caller's working
  directory. It also refused git reads of the
  repository's own `dev/`. The rule becomes an **allow list** matching the settings' `Read` allow list:
  - **Path words** are every non-option word that holds a `/` or is `..`; every word after `--`,
    whatever its first character; an option word's value after its first `=`; the rest of a single-dash
    word after its first letter, when that rest holds a `/` (`-f/sys/x`, `-S/dev/null`, `jq
    -L/proc/1/root`); and git's `-C` value, attached or the next word (superseded by round H, below: a
    caller's `-C` is refused in every form, so its value is no path word). `-` alone is stdin and no path
    word. A word with no `/` is a name in the working directory and passes, `..` excepted.
    *Amended 2026-11-23 (issue #277 round K, replacing round J's letter-run rule):* the wrapper models
    **no tool's option cluster**. The value of a single-dash word may start after any letter of its
    cluster (`git blame -wS/var/lib/dpkg/status`: `S` takes `/var/lib/dpkg/status`), and git also reads
    a run of digits inside a cluster as `-NUM` and goes on, so in `git grep -n5f/tmp/x HEAD` `f` takes
    `/tmp/x` and git echoes the file through its regex error; round J's rule, which followed the leading
    run of letters, stopped at the `5`. The rule is now: **in every single-dash word that holds a `/`,
    the suffix from its first `/` (`arg[strings.Index(arg, "/"):]`) is a path word, and must be an
    allowed absolute path**, under `/srv/audit/checkout`, `/srv/audit/checks` or `/srv/audit/work`; a
    word holding a `..` element stays refused. Whichever letter takes the value, a value holding a `/`
    starts at that first `/` or holds it, so its absolute part is checked. A single-dash word with no
    `/` holds no path beyond a name relative to the base, which cannot leave it. So `git grep
    -if/srv/audit/work/p`, `git grep -n5f/srv/audit/work/p` and `git blame -S/srv/audit/work/revs` pass,
    and `git grep -n5f/tmp/x`, `-0f/run/x`, `git blame -wS/var/x` and `git ls-files -oX/home/runner/x`
    are refused. *Accepted side effect (fail closed):* an attached value that is a relative path holding
    a `/` (`-Sfoo/bar`) is refused, since its suffix `/bar` is absolute and outside the roots; the model
    passes the value as a word of its own (`-S foo/bar`).
    *Amended 2026-11-23 (issue #277 round L, the word rules, replacing every earlier wording that `..`
    is refused anywhere):* that wording held only for path words, and a word starting with `--` with no
    `=` was none, so `jq -n --rawfile a --/../../../../etc/passwd '$a'` printed any file the sandbox let
    jq read, once Write had made `/srv/audit/work/--/x`. Three rules now run **before any command's own
    rules, on every word of every line**, and fail closed:
    1. a word whose `/`-separated elements include `..` is refused (`..`, `x/../y`,
       `--/../etc/passwd`);
    2. a word starting with `-` that holds `..` anywhere is refused, since an attached value is no
       element of its own (`-S..`, `-f..`);
    3. a word starting with `--` that holds a `/` before any `=` is a path word **whole**, joined to the
       line's base like any relative word (`jq --rawfile a --x/y` reads `/srv/audit/work/--x/y`, which
       passes). A `--` word whose first `/` comes after its `=` keeps its value as its path word, as
       before: `git log --format=%h/%s` holds the path word `%h/%s`, read from the checkout, and passes.

    A git range (`HEAD~3..HEAD`, `main..origin/main`) starts with no `-` and holds no `..` element, so it
    passes. The rules are stated here once; the bullets above and below that refuse a `..` element in a
    path word stay true and are now a consequence of rule 1.
  - **Resolving:** an absolute word is read as it is; a relative one on a git line is joined to
    `/srv/audit/checkout`, which is what git reads it under the wrapper's `-C`, and on any other line to
    `/srv/audit/work`, the wrapper's working directory.
  - **Refused:** any path word holding a `..` element at all, before anything is cleaned (this closes
    `/dev/fd/..` and every link-then-`..` form); and any word whose path, cleaned of its empty and `.`
    elements, is not `/srv/audit/checkout`, `/srv/audit/checks` or `/srv/audit/work` and lies under none
    of them. The refused word is named as before, the option's included; the option rules run first, so
    a refused option is named even when its value lies outside the roots too. (Since round L the word
    rules, above, run before both.)
  - **Fail-closed side effects, accepted:** a git ref holding a `/` (`origin/main`) resolves under the
    checkout and passes; a git grep pattern holding an absolute path outside the roots (`-e /proc/`) is
    refused; a URL-like pattern (`http://x`) cleans to a relative name and passes; `sort -t /` is
    refused (the model can use another separator). `git log -- dev/tofu.sh`, `git blame dev/lima.yaml`
    and `cat /srv/audit/checkout/dev/tofu.sh` pass.
  - **The working directory is fixed:** before every `exec` the wrapper changes into `/srv/audit/work`
    (`os.Chdir`), so a relative name means the same whatever directory the caller ran it in; git keeps
    its own `-C /srv/audit/checkout`. A change that fails exits 127, as a failed exec does, and a
    refused line changes and runs nothing.
  - **`--no-index`** is refused on a git line, alone, with `=value` and abbreviated, as git's other
    refused long options are: `git diff --no-index` compares two files of its own, no part of the
    checkout. `git diff` of two absolute paths outside the checkout runs no-index on its own; the path
    rule is what keeps both inside the allowed roots, and the explicit refusal is for clarity.
  - **Defence in depth, not the boundary.** The wrapper sees only the words it is given: a shell
    redirection (`audit-exec cat < /proc/<pid>/envir?n`) is opened by the shell, glob and all, before the
    wrapper starts, and no rule of the wrapper's reads it. The sandbox is the boundary; the wrapper
    narrows what a command run through it may name. The two forms the wrapper cannot see, or could only
    see after the kernel resolved a link, are therefore preflight probes (probe 5, below), so the
    sandbox itself is tested on every run.
  - **Links in the checkout are covered already:** step 3's tree walk refuses any link in the checkout
    or the checks tree that resolves outside its own tree, and the work area is checked to hold only
    its empty `tmp/` before each run. The model cannot make a link: the wrapper lists no command that
    makes one, Bash runs nothing but the wrapper, and Write and Edit write regular files. A path word
    under one of the three roots therefore reaches only what lies under it.
- *Added 2026-11-23 (issue #277 round H):* **A caller's `-C` and jq's module system are refused.**
  - **git's `-C`, in every form.** The wrapper's own `-C /srv/audit/checkout` comes first, and git
    applies its `-C` options in order, so a caller's later global `-C` would win and run git in the work
    area, an allowed root the model may Write in, where a planted `.git/config` names `core.fsmonitor`
    or `diff.external` and git runs that program. *Corrected in round I:* `git -C /srv/audit/work diff`
    never reached git: the first word after `git` must be a listed subcommand, so the subcommand-first
    check already refused a global `-C`; after the subcommand, `-C` is the subcommand's option and no
    directory. Round G only held `-C`'s value to the allow list, which `/srv/audit/work` passes; the
    letter rule below makes the refusal explicit wherever `-C` sits, as defence in depth. `C` joins `c` and `O` as a refused letter: any single-dash
    word holding it is refused, separate (`-C`), attached (`-C/srv/audit/work`) or inside a short
    cluster (`-nC`), wherever it sits on the line. As a subcommand option it is git log's and git
    blame's copy detection, no directory, and it is refused all the same: `-C` is ambiguous on the
    line, the check fails closed, and the audit does without copy detection (`-M` still passes). The
    wrapper's own `-C` stays the only one git reads, so `-C`'s value is no longer a path word.
  - **More refused long options**, alone, with `=value` and abbreviated, anywhere on the line:
    `--super-prefix`, `--attr-source`, `--bare` and `--literal-pathspecs`, beside `--git-dir`,
    `--work-tree`, `--namespace` and `--exec-path`. Each changes which repository, work tree or
    attributes git reads, or how it reads a pathspec; `--literal-pathspecs` is harmless, but nothing
    before the subcommand but what the wrapper adds reaches git, and the same word after it is refused
    with the others.
  - **jq's module system.** A jq filter's `import` and `include` directives read a module, and
    `{search: ...}` and `-L`/`--library-path` name where jq looks for it, so a filter could read any
    `.json` file of the image, outside the roots (a `search` value is no word of the line, so the path
    rule never sees it). On a jq line every word holding `import` or `include` is refused, wherever it
    sits, so a filter or an `--arg` value that merely mentions one is refused too (fail closed: the audit
    needs filters, no modules); and `-L` in any single-dash word (alone, attached, in a cluster) and
    `--library-path` (alone, `=value`, abbreviated). A file name holding `import` or `include` is refused
    too (fail closed). *Amended 2026-11-23 (issue #277 round I):* `-f`/`--from-file`, a program file, are
    refused in the same forms, since a program file's own `import` is never checked (below).
    *Amended 2026-11-23 (issue #277 round L):* the builtins `modulemeta` and `get_search_list` search
    jq's module paths and read a module's metadata, the module system by another name: a jq word
    holding either is refused in the same way. `jq -r
    .findings /srv/audit/work/result.json` and `jq '.[] | .id' /srv/audit/checks/a.json` pass.
  - **git's implicit no-index diff.** `git diff <a> <b>` compares two files outside git's work tree as
    `--no-index` would, without the option, when both paths lie outside the repository. `--no-index` is
    refused for clarity, not because it alone keeps git in the checkout: it is the path allow list that
    keeps both paths inside the three roots, so an implicit no-index diff reads nothing the audit may
    not read anyway.
  - **Fail-closed side effects, accepted** (beside round G's `sort -t /`): `cut -d/` and `tr` with a `/`
    operand are refused: `-d/`'s attached value and `tr`'s `/` operand are path words, and `/` lies in
    none of the roots. The model can use another delimiter, or `tr`'s octal escape `\057`. git log's `-C` and any cluster
    holding `C` (a `-S` value with a capital C attached, `-SConfig`) are refused too; `-S Config`, the
    value as the next word, passes. *Amended 2026-11-23 (issue #277 round I):* so is `git grep -C <n>`,
    its context lines; the model uses `-A <n> -B <n>`. A jq filter, `--arg` value or file name that
    holds `import` or `include` as part of another word (`.imports`, `includes.json`) is refused as well.
- *Added 2026-11-23 (issue #277 round I):* **No command reads a program or a list of paths from a
  file.** The path rule and the module check read the words of the line; these options make a command
  open what a file names instead, so neither rule sees it.
  - **jq's program file.** `-f`/`--from-file` read the jq program from a file the model may have
    written, and that program's own `import` and `include` are never read by the module check: a
    program file is the module system by another name. `f` joins `L` as a refused letter, in any
    single-dash jq word (alone, `-f p.jq`; attached, `-fp.jq`; in a cluster, `-nf`), and `--from-file`
    is refused alone, with `=value` and abbreviated (`--from-f`), as `--library-path` is. The audit
    passes its filters on the line; `jq -r .findings /srv/audit/work/result.json` passes.
  - **wc's `--files0-from`** reads the names of the files to count from a file: refused alone, with
    `=value` and abbreviated (`--files0`).
  - **sha256sum's `-c`/`--check`** reads the names of the files to check from a digests' file: `c` is
    refused in any single-dash sha256sum word (`-c`, `-bc`) and `--check` alone, with `=value` and
    abbreviated. `wc -l /srv/audit/work/x` and `sha256sum /srv/audit/work/x` pass.
  - These checks read every word of the line, those after `--` too, so they fail closed. sort's
    `--files0-from` was refused already by its allow list (round C).
- **The same environment for every command**, the five `GIT_*` included: PRD #175's criterion is one
  scrubbed environment, and a variable only git reads changes nothing for the others.
- **The exec seam.** The logic is `run(args []string, exec func(path string, argv []string, env []string)
  error, stderr io.Writer) int` in `package main` (`run.go`); `main` (`main.go`, `//go:build unix`)
  passes `syscall.Exec`, which has that shape and replaces the wrapper, so no wrapper process outlives
  the exec, and a test passes a fake. On a system without `syscall.Exec` (`main_other.go`, `!unix`, so
  the package builds and is linted for every release target) `main` refuses with exit 126. A refusal is
  exit 126 with one line, `audit-exec: refused: <word>`, the word cut to 64 bytes and quoted as a Go
  string (so a newline, a carriage return or a control character stays one line and no line starts a
  workflow command); an exec that fails is exit 127 with one such line. *Amended 2026-11-23 (issue #277
  round G):* `run` takes a second seam, the change of directory, ahead of the exec:
  `run(args, chdir func(dir string) error, exec, stderr)`; `main` passes `os.Chdir`, a test a fake.
  In full (amended 2026-11-23, issue #277 round H): `run(args []string, chdir func(dir string) error,
  exec func(path string, argv []string, env []string) error, stderr io.Writer) int`.
- **Its rules:** the strict depguard rule `audit-exec` (`fmt`, `io`, `os`, `strings`, `syscall`; `os/exec`
  and `net` denied) and the forbidigo group `audit-exec-env`, which refuses every read of the environment,
  every process start, library load and raw system call, with one named exception: `syscall.Exec` in
  `ci/auditexec/main.go` alone. *Amended 2026-11-23 (issue #277 round G):* the group
  `tests-no-process-state` has one named, path-scoped exception of the same shape: `os.Chdir` in
  `ci/auditexec/main.go` alone, the change into `/srv/audit/work`. Its exclusion rule is msg-granular, so
  it would also drop a `Setenv` there; `test/lint`'s AST check still refuses every call there but
  `os.Chdir`. `ci/auditexec` imports no library yet; the lax rule
  `audit-library-importers` lets it import `ci/internal/audit`, which itself is held by `audit-library`
  and not by `audit-exec-env` (it starts `bwrap` and `sudo`, which `audit-exec-env` would refuse).
  *Amended 2026-11-23 (issue #277 round F):* the library has a forbidigo group of its own,
  `audit-library-env`, which refuses `os.Getenv`, `os.Environ`, `os.LookupEnv`, `os.UserHomeDir`,
  `os.UserConfigDir`, `os.UserCacheDir` and `os.ExpandEnv` in every file under `ci/internal/audit/`, its
  tests included, through one exclusion rule (`path-except: ^ci/internal/audit/`) and no exception. It
  names no process start, so `bwrap` and `sudo` stay allowed; what the library needs of the step's
  environment reaches it from `ci/auditrun` as parameters.

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
  | 5 | Read and Grep (pattern `SHIFT2EU_AUDIT_CANARY`) of `/proc/<auditrun pid>/environ`, Glob for the pattern `environ` in the path `/proc/<auditrun pid>`; Bash `/srv/audit/bin/audit-exec cat /proc/<auditrun pid>/environ`, `.../cat /proc/$PPID/environ`, `.../cat /proc/$$/environ`, `.../cat < /proc/<auditrun pid>/envir?n`, `.../cat < /dev/fd/../../<auditrun pid>/environ` (added 2026-11-23, issue #277 round G; a redirection since round H) and `.../cat /srv/audit/work/nonexistent/${CLAUDE_CODE_OAUTH_TOKEN:+tokenset}` (in the work area since round I) | neither the canary's value nor the token's appears in any `tool_result` or anywhere else on stdout, and the last Bash call's result is an error naming `/nonexistent/` with nothing after it |
  | 6 | Write and Edit of the `$GITHUB_ENV` file's path; Read and Grep of it, Glob for its base name in the path of its directory; Bash `/srv/audit/bin/audit-exec cat /srv/audit/work/preflight-control.txt >> <path>` | the file's sha256 is unchanged |

  *Amended 2026-11-22 (issue #277 round C):* a Glob that names no path searches the working directory,
  which the settings allow, so its result proved nothing about the denied path: each Glob now names the
  denied directory as its `path` and the name looked for as its `pattern`. Probe 5's last Bash call
  shows whether the token variable reached the sandboxed shell without ever showing its value: the shell
  expands an unset variable to nothing, so `cat` fails naming `/nonexistent/`; a set one would make it
  name `/nonexistent/tokenset`. It is **the one call of the plan expected to be an error for a reason
  other than a refusal**: a result that holds `tokenset`, is not an error, or does not name
  `/nonexistent/` with nothing after it (a refusal by the settings among them) fails probe 5.
  *Amended 2026-11-23 (issue #277 round G):* probe 5 gains two Bash calls for the forms the wrapper's
  path rule cannot see, after `cat /proc/$$/environ` and before the token-absence call: `cat <
  /proc/<pid>/envir?n`, a shell redirection plus a glob, which the shell opens before the wrapper runs
  and which gets past the `Bash(*/proc/*/environ*)` rule's text; and `cat /dev/fd/../../<pid>/environ`,
  which the kernel resolves through `/proc/self/fd`. Each is expected to be refused, as every other
  probe call is: a result that is not an error, or that holds the canary, fails probe 5. They make the
  sandbox itself, not only the wrapper, show on every run that the environ is out of reach.
  *Amended 2026-11-23 (issue #277 round H):* the `/dev/fd` call is a redirection too, `cat <
  /dev/fd/../../<pid>/environ`. As an argument, the wrapper's path rule refused it for its `..` element
  before anything opened it, so the call tested the wrapper and not the sandbox; redirected, the shell
  opens it before the wrapper starts, the wrapper never sees the path, and the command's text holds no
  `/proc/` for the `Bash(*/proc/*/environ*)` rule to match. The plan's row, the prompt's line (made from
  it) and the judgement's match all name the redirected command.
  *Amended 2026-11-23 (issue #277 round I):* since round G the wrapper refuses `/nonexistent/...` as
  outside its roots, so `cat` never ran, and the wrapper's refusal, `audit-exec: refused:
  "/nonexistent/"`, also names `/nonexistent/` with nothing after it: the judgement could not tell it
  from `cat`'s own failure, and the call proved nothing. The token-absence call becomes `cat
  /srv/audit/work/nonexistent/${CLAUDE_CODE_OAUTH_TOKEN:+tokenset}`: the path lies in the work area,
  inside the roots, so the wrapper passes it and `cat` fails with "No such file or directory".
  `work/nonexistent` cannot exist when the preflight runs: the layout check holds the work area to its
  empty `tmp/` alone, and during the preflight it holds `tmp/` and the launcher's control file
  `preflight-control.txt` only (wording amended in round J). The judgement's pass condition is unchanged: an error result naming
  `/nonexistent/` followed by the end, `:`, whitespace or a quote, and holding no `tokenset`.
  *Corrected 2026-11-23 (issue #277 round J):* the old call did prove absence: the shell expands
  `${CLAUDE_CODE_OAUTH_TOKEN:+tokenset}` before the wrapper starts, and the wrapper's refusal quotes the
  expanded word, so a set token gave `refused: "/nonexistent/tokenset"` and the judgement failed it.
  What was wrong is that `cat` never ran, so the call tested the wrapper rather than the command the
  plan describes, and the tests' passing answers did not match the real output. Inside the roots, `cat`
  runs and the answer is the one the plan names.

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
  drops each call in turn and the control's nonce; each case must fail the step. *Amended 2026-11-22
  (issue #277 round C):* the observation is a func seam, `audit.Observer` (`func(ctx, root, gitHubEnv
  string, before [32]byte, accepted bool) audit.Effects`), which `Preflight` calls once, after it has
  closed the listener, under `context.WithTimeout(context.WithoutCancel(ctx), 10s)`: a run that was
  cancelled or passed its deadline is still observed, where a done context would fail the Docker
  connect and `sudo` for the context and read probes 3 and 4 as refused. `audit.Observe` is the real
  one; the audit's one CLI run is the seam `audit.Launch`. Two tests of
  `ci/auditrun` drive the real steps under a temporary root with a fake `claude` that records its
  environment. `TestRunInheritsNoneOfTheStepsEnvironment` re-executes its own test binary as the
  launcher with only the variables an earlier step could set through `$GITHUB_ENV`
  (`ANTHROPIC_BASE_URL`, `HTTPS_PROXY`, `NODE_OPTIONS`, `LD_PRELOAD`) and a sentinel in its
  environment: none of them is in the recorded environment, which holds the launcher's allow list and
  the shell's own names (`PWD`, `SHLVL`, `_`, `OLDPWD`) alone. `TestRunStopsAtAManagedSettingsFile`
  shows that a managed-settings directory under the test's root stops the launcher with exit 3 before
  the fake runs.

*Settled by the implementation (issue #277):*

- **The packages and their rules.** `ci/internal/audit` holds the six steps (`CheckManagedSettings`,
  `InstallClaude`, `CheckLayout`, `CheckBubblewrap`, `Preflight`, `Audit`), `Run`, which takes them in
  order, and `SystemSteps(pid, observer)`, the real steps; every path is taken under a root standing in
  for `/`. `ci/auditrun` is `main` and `run(args, getenv, steps, root, stdout, stderr) int`, which reads
  the flags and the five variables, refuses an `ANTHROPIC_API_KEY` (even one set to nothing) with exit
  3, and runs `audit.Run` with the root it is given: `main` passes `/` and `audit.Observe`, a test a
  temporary directory. A malformed token, canary or integrity, a missing `$GITHUB_ENV` or a flag
  error is exit 2; a missing `$GITHUB_STEP_SUMMARY` leaves the line on stdout alone. *Amended
  2026-11-23 (issue #277 round N):* the API key is refused first, before the flags and the other
  variables are read, so a step that holds it exits 3 with its line whatever else is missing or
  malformed. Depguard holds the
  library to `audit-library` (named standard packages and `ci/internal/claudecode`; `os`, `os/exec` and
  `net` for its own checks) and the entry point to `audit-main` (no `os/exec` or `net`); the lax rule
  `audit-library-importers` names `ci/auditrun` and `ci/auditexec`. The forbidigo group `audit-run-env`
  holds in `ci/auditrun`, with `os.LookupEnv` named in its `main.go` alone, and, amended 2026-11-23
  (issue #277 round F), `audit-library-env` in `ci/internal/audit` (environment reads and home, config
  and cache lookups, no process start; see the wrapper's rules above).
- **The run's one line.** Each step's failure prints a constant class; the bubblewrap failure prints the
  sentence above; the preflight's prints its `Verdict`, whose text is built from constants alone
  (`preflight failed: probe 2, TCP connect: not refused; see runbook step 10`). A preflight error that is
  no `Verdict` (the CLI run failed, the set-up failed) prints a constant class too. *Amended 2026-11-23
  (issue #277 round N):* a CLI run that failed (cancelled, timed out, exited non-zero) is still judged
  by its observed effects: if any shows, the preflight returns the `Verdict` naming each such probe and
  runbook step 10; only a failed run with no effect prints the constant class. The line goes to
  stdout and is appended to `$GITHUB_STEP_SUMMARY`.
- **The inputs reach the context-taking steps** through the context: `Run` puts its `Inputs` there and
  `InputsFrom` reads them, so the real steps can be built before the inputs are read.
- **The checksum** reads the tarball once (at most 1 GiB), compares its sha512 in constant time, then
  checks every entry before writing anything: only regular files and directories, every name a clean
  relative path under `package/`, `package/claude` exactly once. The binary is written to a temporary
  file beside `bin/claude` and renamed into place, so a refused tarball leaves nothing behind.
  *Amended 2026-11-22 (issue #277 round C):* a PAX global header (`g`), which holds records and no file,
  is skipped before the entry check and nothing is extracted from it; `state/bin` is checked with
  `Lstat` as a real directory before anything is written, so a link there is followed nowhere (the
  binary is extracted before the layout check looks at `state/bin`).
  *Amended 2026-11-23 (issue #277 round D):* the signature is `InstallClaude(root, tarball, integrity,
  bin string) error`. `bin` must lie under `root`, and every directory of its path from `root` down to
  `bin`'s own (`srv`, `srv/audit`, `srv/audit/state`, `srv/audit/state/bin`) is checked with `Lstat` as
  a real directory, no link, before anything is written, so nothing is written through a link anywhere
  in the path. The destination stays a parameter: a three-string `(root, tarball, integrity)` would have
  the old signature's shape, and a call in the old order would compile.
- **The layout** also wants `/srv` and `/srv/audit` to be directories, not links, and `work/tmp` empty. The
  wrapper's owner is not checked (a check would need `syscall`, which the library is denied); the
  model-free step's `sudo install -o root -m 0555` makes it, and the check holds its mode, exactly 0555.
  Checking the owner is left to PR 5's model-free step (amended 2026-11-23, issue #277 round H).
- **The bubblewrap check** runs `/usr/bin/bwrap` by absolute path, in `/`, with `PATH=/usr/bin:/bin` alone.
- **The preflight's plan** is 24 calls (amended 2026-11-23, issue #277 round G, from 22; round C made it
  22, from 21): the control's
  Read, then probe 1's Write and two Bash calls, probe 2's Bash, probe 3's and probe 4's two Bash calls
  each, probe 5's Read, Grep, Glob and six Bash calls (the redirected and the `/dev/fd` read fifth and
  sixth, the token-absence call last), probe 6's Write,
  Edit, Read, Grep, Glob and Bash. `PreflightPlan(audit.Targets{Root, Port, Pid, GitHubEnv})` fills
  each call's expected input from its targets: `file_path` of Read, Write and Edit, `command` of Bash,
  `pattern` and `path` of Grep and Glob. The prompt numbers them, names each tool and target,
  and the appended system prompt says to make each exactly once, in order, refused or not. The nonce is
  16 random bytes in hex, removed with the control file after the run. The preflight's run has a deadline
  of its own, 10 minutes (the audit's is 40).
- **The judgement** matches the transcript's `tool_use` elements to the plan by tool, input and order
  (amended 2026-11-22, issue #277 round C, from "by tool name and order"): the transcript's `tool_use`
  input is parsed, and a call matches a planned one when its tool is the plan's and every key the plan
  names holds exactly the plan's value as a JSON string (keys the plan does not name, such as a Write's
  content, are the model's). The transcript's calls must be the plan's in order with some left out. When
  they are not, the judgement aligns them by tool alone: a call of the plan's tool in a planned call's
  place, made with another input, is a call the plan does not list, and fails that call's probe by
  name; a transcript that is no run of the plan's tools in order fails closed. The order check stays:
  the ordering risk is recorded below, and matching by input is what would let it be relaxed. Where
  repeated tools make it ambiguous which call was left out (two Bash calls side by side), every probe
  the missing call could belong to is named. A call whose input holds `dangerouslyDisableSandbox`, the
  field with which a call asks the CLI to run it outside the sandbox, with any value but JSON `false`
  (`true`, a string, a number, `null`), fails the probe it belongs to, or the control, whatever its tool
  and however harmless its command (amended 2026-11-22, issue #277 round C): the preflight shows that
  the sandbox holds, so a call that asks to leave it fails closed. `false`, or no such key, is the
  plan's call. A `tool_result` with `is_error` is a refusal; its text is
  its string content or its text blocks joined. Probe 5 succeeds when the canary or the token given to
  `Judge` (`Judgement.Canary`, `Judgement.Token`) appears anywhere in the transcript, or a run of 64
  lower-case hex characters appears in any `tool_result` (so a canary split across text blocks counts),
  or its token-absence call does not show the variable unset. An effect that cannot be observed counts
  as observed: a probe file that cannot be checked, a `$GITHUB_ENV` file that cannot be read again.
- **The CLI's stderr.** The launcher keeps it to itself and quotes at most 500 bytes of it, the token
  and the canary redacted (`Config.Redact`, amended 2026-11-22), in its error; that error is written to
  `/srv/audit/state/out/preflight.stderr` or `audit.stderr` and never printed. The transcripts go to
  `preflight.jsonl` and `audit.jsonl` there. The system prompt's file is written into
  `/srv/audit/state/tmp` (`Config.SystemPromptDir`), which no tool can read and no earlier step's
  `TMPDIR` can move, and removed after each run.

#### Risks for the first workflow run (issue #277 rounds C and D)

What the tests cannot show without a real runner, to be checked on the audit workflow's first run (PR 5
of milestone 2), and the constraints PR 5 must keep:

- **A refused Glob may not return `is_error`.** The judgement wants every probe call's result to be an
  error; if the pinned CLI answers a denied Glob with an empty, non-error result, probes 5 and 6 fail
  the step on every run, closed. The first run shows which; the judgement is changed only by a reviewed
  amendment here.
- **The model may not keep the plan's order.** The judgement fails closed when it does not. Matching by
  input now makes it possible to relax the order check if a real run needs it; that is a later,
  reviewed change, not this one.
- **The pinned tarball's entry types.** The checksum step refuses any entry but a regular file, a
  directory or a PAX global header. Before PR 5, run `tar -tvvf` once on the pinned tarball to confirm it
  holds no other type, so the first run is not refused for a packing detail. *Added 2026-11-24 (PR 5a):*
  done for the pin `@anthropic-ai/claude-code-linux-x64@2.1.331`, its tarball fetched from the registry's
  `dist.tarball` and its SHA-512 matching the pin's `integrity:`. `tar -tvvf claude-code-linux-x64-2.1.331.tgz`
  lists 4 entries, all regular files (`package/claude`, `package/package.json`, `package/LICENSE.md`,
  `package/README.md`), and a walk of the raw ustar headers (each header's type flag read, so PAX `g` and
  `x` and GNU `L` and `K` headers would show too) finds 4 headers, all type `0`: no directory, no PAX
  header of either kind, no link. The result holds for that version only.
- **Probe 5's `cat /proc/$$/environ`** probes the sandboxed shell's own environment, where the canary is
  present (the CLI passes it on) and the token is scrubbed: it fails only when the shell's environ is
  readable through the wrapper's `cat`, which the `Bash(*/proc/*/environ*)` rule and the sandbox refuse.
  The token-absence call is what shows the token itself is absent.
- *Added 2026-11-23 (issue #277 round G):* **Anything the preflight leaves in `work/tmp` fails the
  audit's work-area check.** The audit's run checks the work area again before it starts, and `tmp/`
  must be empty; if the sandbox, a refused probe call or a command run through the wrapper leaves a file
  there during the preflight, the audit fails closed on every run. The first run shows whether the
  sandbox or a command leaves one; any change to the check is a reviewed amendment here.
- **Bash's `cat` can read world-readable paths outside the Read allow list** (the sandbox's read rules
  follow the deny list, not the allow list). PR 5 must therefore put no secret under `/tmp` or `/run`,
  copy the checkout with `persist-credentials: false` (so `.git/config` holds no `extraheader`, which the
  layout check refuses anyway), and never upload the `state/out` transcripts unencrypted.
- *Added 2026-11-23 (issue #277 round D):* **Probe 6's Write and Edit prove nothing by their own
  `is_error`.** Claude Code's read-before-write guard refuses a Write or an Edit of a file the run has not
  read, before any permission rule or sandbox is consulted, so their error shows the guard, not the
  deny rule. Probe 6 rests on the launcher's own observation, the `$GITHUB_ENV` file's digest before and
  after the run.
- *Added 2026-11-23:* **Whether the pinned CLI marks a non-zero Bash exit as `is_error`.** Probe 5's
  token-absence call needs it: if a Bash call that exits non-zero comes back without `is_error`, the
  judgement reads that call wrongly. The first run's transcript shows which; a change to the judgement
  is a reviewed amendment here.
- *Added 2026-11-23, amended in round K:* **git's cluster rule over-refuses.** Any single-dash word that
  holds `c`, `O` or `C` is refused, so an attached value that holds one of these letters is refused with
  it: `-Scredential`, `-Gfunc`, `-SConfig`, `git grep -c`. It fails closed, and the model can pass such a
  value as a separate word (`-S credential`, `-S Config`). Recorded only: the skill is not changed in
  this PR.
- *Added 2026-11-23 (issue #277 round K):* **A delimiter of `/` reads as a path.** `tr /`, `tr -d /`,
  `cut -d /`, `cut -d/` and `sort -t/` are refused: the `/` operand or attached value is a path word,
  and `/` lies in none of the roots. It fails closed, and the model can pass a delimiter other than `/`
  (or `tr`'s octal escape `\057`).
- *Added 2026-11-23 (issue #277 round K):* **A SIGTERM from the runner does not cancel the CLI's
  context.** `ci/auditrun` handles only SIGINT (adding SIGTERM needs `syscall` in the strict
  `audit-main` rule, a design change for a minor effect). On a cancel or a timeout the runner kills the
  step's process tree, so the step still ends; only the launcher's own clean-up on cancel is skipped.
- *Added 2026-11-23, amended in round E:* **PR 5's checkout must not turn on sparse checkout or
  worktree config.** `.git/config`'s allow list refuses `extensions.worktreeconfig` and every
  `core.sparsecheckout*` key; `.git/config.worktree` may hold only `core.sparsecheckout`,
  `core.sparsecheckoutcone` and `index.sparse` (the three a default `actions/checkout` writes, each
  `false`, though the check holds keys and passes any value); and the layout check refuses `.git/commondir` and `.git/gitdir`. The default checkout
  passes; one with `sparse-checkout:` set, or any other key in either file, fails step 3 on every run.
- *Added 2026-11-23 (issue #277 round E):* **The runner's git may leave an empty section header after
  checkout's unsets.** With `persist-credentials: false`, `actions/checkout` unsets its
  `http.<url>.extraheader` and `includeIf` keys; a git that keeps the emptied `[http "..."]` or
  `[includeIf "..."]` header behind leaves a section off the allow list, which is refused, empty or
  not. It fails closed; the first run shows which the runner's git does, and any change to the allow
  list is a reviewed amendment here.
- *Added 2026-11-23 (issue #277 round E):* **`readResult` can block between its `Lstat` and its
  `Open`** if something swaps a FIFO in at `result.json` in that window: the open of a FIFO waits for
  a writer. That needs a process outliving the CLI, which `--die-with-parent` rules out for everything
  the sandbox started. It fails closed: the step never takes a result, and should the open return, the
  `os.SameFile` check refuses what it opened.
- **Renovate's generic manager** lists the workflows that hold an annotated pin one by one; PR 5's audit
  workflow must join that list, which `test/lint`'s `TestEveryAnnotatedWorkflowPinIsScanned` enforces
  once the workflow holds a `# renovate: datasource=` comment. *Added 2026-11-24 (PR 5b):* it has joined
  both managers' lists, the versions' and the release assets' digests'.

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
`Env` entry that is no `NAME=VALUE` or whose name is not on the `Env` allow list (`TMPDIR`,
`CLAUDE_CODE_SUBPROCESS_ENV_SCRUB` and `SHIFT2EU_AUDIT_CANARY`, amended 2026-11-23, issue #277 round F;
it replaced the deny list of the names the launcher sets itself and of the credential, endpoint, proxy
and loader names above). A config's `Home` also
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

### The runner image: the move to ubuntu-26.04 (issue #568, amended 2026-11-25)

Renovate's PR #561 moves the workflow's three jobs from `ubuntu-24.04` to `ubuntu-26.04`. Its CI fails
by design, on three jobs: **Test**, where `TestScheduledSecurityAuditWorkflow` reports for each job
`runs-on is "ubuntu-26.04", want ubuntu-24.04, never a moving image: the wrapper's option analysis
assumes its GNU coreutils (ADR 0040)`; **Lint shell scripts and workflows**, where actionlint reports
`label "ubuntu-26.04" is unknown` on lines 34, 217 and 463 (`[runner-label]`); and **Lint**, which only
reports that `lint-shell` failed (run 45504245044 of 2026-11-24). The maintainer decided on 2026-11-25
to plan the move with this amendment rather than close the PR outright.

**Decision.** The audit **stays on `ubuntu-24.04` until two conditions hold**, and then moves in **one
pull request** that changes the image, the GNU coreutils it relies on, the Ubuntu packages and their
pins, and the pin test together:

1. **actionlint knows the label.** A release of actionlint that lists `ubuntu-26.04` is pinned in
   `tools/go.mod`. ADR 0022 refuses an actionlint configuration file (`dev/actionlint.sh`, `test/lint`,
   `test/architecture`'s make-lint test), so declaring the label in `.github/actionlint.yaml` is no way
   around it without an amendment to ADR 0022, which is the maintainer's choice (below).
2. **The image probe has run** (below) and settled every fact this section marks *unconfirmed*.

On the new image, **every job switches the image to GNU coreutils before anything else runs**, with the
switch Ubuntu documents, and the GNU check holds in every job, so the wrapper's option analysis, its
command paths under `/usr/bin` and the workflow's own guards (`sha256sum --check --strict`, `stat -c`,
`od`, `tr`, `ls -A`, `install`) keep running the implementation they were written and reviewed against.
PR #561 is then **closed, replaced** by that pull request: the image cannot move alone, since the
pinned `.deb`s are noble's and the pin test wants `ubuntu-24.04`, and nobody pushes to Renovate's branch.
Until then `ubuntu-24.04` is safe: the runner-images README lists it as available, with no deprecation
notice (read 2026-11-25), and the workflow does not merge its schedule before milestone 3 anyway.

**The facts** (public sources, each read 2026-11-25):

- **`ubuntu-26.04` is generally available.** The runner-images README lists `ubuntu-26.04` (and
  `ubuntu-latest`) for Ubuntu 26.04 x64; its preview label was removed on 2026-09-17 (commit `a99056ad72`,
  "Preview label for ubuntu26 removal", PR actions/runner-images#14739), and `ubuntu-latest` was
  associated with it on 2026-11-24 (commit `e015a08e76`, PR actions/runner-images#15381). The
  announcement actions/runner-images#14748 says "Ubuntu 26.04 has completed its public preview period and
  is generally available for production workloads", with the `ubuntu-latest` migration rolled out from
  2026-10-19 to 2026-11-19. Sources: <https://github.com/actions/runner-images/blob/main/README.md>,
  <https://github.com/actions/runner-images/issues/14748>. (So every workflow of ours on
  `ubuntu-latest` already runs on 26.04: the pin test's "never a moving image" is borne out.)
- **The image** (`images/ubuntu/Ubuntu2604-Readme.md`, blob `58bb856f8f`, image version
  `20261116.202.1`; <https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2604-Readme.md>),
  beside 24.04's (`Ubuntu2404-Readme.md`, image `20261116.376.1`):

  | Tool | `ubuntu-24.04` | `ubuntu-26.04` |
  |---|---|---|
  | OS | 24.04.5 LTS | 26.04.1 LTS |
  | Kernel | 7.0.0-1017-azure | 7.0.0-1020-azure |
  | `coreutils` package | 9.4-3ubuntu6.3 (GNU) | 9.5-1ubuntu2+0.0.0~ubuntu25, the **meta package** (below) |
  | Bash | 5.2.21 | 5.3.9 |
  | Docker server and client | 28.0.4 | 29.4.2 |
  | Git | 2.56.1 | 2.56.1 |
  | jq | 1.7 | 1.8.1 |
  | GitHub CLI | 2.104.0 | 2.104.0 |
  | sudo package | 1.9.15p5 | 1.9.17p2 (classic sudo) |
  | Go in the tool cache | 1.25.14, 1.26.11, 1.27.4; default 1.26 (`toolset-2404.json`) | 1.25.14, 1.26.11, 1.27.4; default 1.26 (`toolset-2604.json`) |

- **Coreutils: uutils by default, GNU beside it.** On 26.04 (resolute) the `coreutils` package is the
  `coreutils-from` source's meta package, version `9.5-1ubuntu2+0.0.0~ubuntu25`, which depends on
  `coreutils-from-uutils | coreutils-from-gnu`
  (<https://packages.ubuntu.com/resolute/coreutils>, <https://launchpad.net/ubuntu/+source/coreutils-from>).
  `coreutils-from-uutils` (0.0.0~ubuntu25) depends on `rust-coreutils` and on `gnu-coreutils`
  (<https://packages.ubuntu.com/resolute/coreutils-from-uutils>); `rust-coreutils` is 0.8.0-0ubuntu3
  (<https://packages.ubuntu.com/resolute/rust-coreutils>); GNU coreutils is the `gnu-coreutils` binary
  of the `coreutils` source, 9.7-3ubuntu2.1 in resolute's updates
  (<https://launchpad.net/ubuntu/+source/coreutils>), and installs every tool the wrapper runs as
  `/usr/bin/gnu<tool>` only (`/usr/bin/gnucat`, `/usr/bin/gnusort`, ...;
  <https://packages.ubuntu.com/resolute/amd64/gnu-coreutils/filelist>), while `coreutils-from-gnu`
  installs the plain `/usr/bin/<tool>` names (<https://packages.ubuntu.com/resolute/amd64/coreutils-from-gnu/filelist>).
  The 26.04 release notes: "The core utilities of the operating system are now provided by the
  `rust-coreutils` package", "Because `rust-coreutils` are not fully compatible yet, we continue to
  provide the classic GNU utilities as well", the switch `sudo apt install coreutils-from-gnu
  coreutils-from-uutils- --allow-remove-essential`, and "the `cp`, `mv`, and `rm` utilities are still from
  GNU in `rust-coreutils`" (<https://documentation.ubuntu.com/release-notes/26.04/summary-for-lts-users/>).
  **Unconfirmed:** which alternative the runner image installs (its README names the meta package
  alone; apt's default is the first, uutils), and what `/usr/bin/<tool> --version` prints there.
- **sudo.** The release notes say "The `sudo-rs` tool is now the default `sudo` provider" (since
  25.10); the image lists the classic `sudo` package, 1.9.17p2-1ubuntu3.2, which only recommends
  `sudo-rs` (<https://packages.ubuntu.com/resolute/sudo>). **Unconfirmed:** which program `/usr/bin/sudo`
  is on the runner, and whether the runner user's rule is still `/etc/sudoers.d/runner` alone. Related:
  actions/runner-images#15004 (open), confirmed by its maintainers on 26.04 and not on 24.04, where a
  privilege drop by ACL on `/run/dbus/system_bus_socket` no longer applies
  (<https://github.com/actions/runner-images/issues/15004>).
- **bubblewrap and socat** are not in the image's package table; resolute ships bubblewrap
  `0.11.1-1ubuntu0.3` and socat `1.8.1.1-1ubuntu0.1` on amd64, both in the security pocket
  (<https://packages.ubuntu.com/search?keywords=bubblewrap&searchon=names&suite=resolute&section=all>,
  <https://packages.ubuntu.com/search?keywords=socat&searchon=names&suite=resolute&section=all>); the pins
  today are noble-updates' 0.9.0-1ubuntu0.3 and 1.8.0.0-4ubuntu0.1.
- **AppArmor** in resolute is `5.0.0~beta1-0ubuntu7`
  (<https://packages.ubuntu.com/search?keywords=apparmor&searchon=names&exact=1&suite=resolute&section=all>).
  **Unconfirmed:** whether the runner still sets `kernel.apparmor_restrict_unprivileged_userns=1`;
  whether `abi <abi/4.0>` still loads under AppArmor 5; and whether an Ubuntu profile already attaches
  to `/usr/bin/bwrap` (the release notes say only that AppArmor "now includes many new profiles for
  applications", since 25.04).
- **actionlint knows no `ubuntu-26.04`.** The pin is `github.com/rhysd/actionlint v1.7.12` in
  `tools/go.mod`, which is also its latest release (2026-03-30;
  <https://github.com/rhysd/actionlint/releases>). `rule_runner_label.go` on its `main` lists
  `ubuntu-24.04`, `ubuntu-24.04-arm`, `ubuntu-22.04`, `ubuntu-22.04-arm`, `ubuntu-slim` and the
  `ubuntu-latest` sizes, and maps `ubuntu-latest` to 24.04's compatibility set
  (<https://github.com/rhysd/actionlint/blob/main/rule_runner_label.go>). Pull requests adding the label
  are open and unmerged (rhysd/actionlint#682, #723, #743), and rhysd/actionlint#719 asks whether the
  project is still maintained. **No release that knows the label exists; when one will is unknown.**

**What assumes the 24.04 image, and what the move changes:**

| Place | Assumption | On 26.04 |
|---|---|---|
| `ci/auditexec`'s command table | `/usr/bin/{sha256sum,base64,od,cat,head,tail,wc,sort,uniq,cut,tr}` are GNU coreutils 9.4 | Unchanged paths after the switch to `coreutils-from-gnu`; GNU becomes 9.7. **Re-verify** GNU's NEWS from 9.5 to 9.7 for each of the 11 tools: no new option that writes a file, reads file names or a program from a file, or runs a program (the deny lists of `wc`, `sha256sum` and `uniq`; `sort`'s allow list is unaffected by a new option, which it refuses) |
| `ci/auditexec`, jq | jq 1.7's module system (`import`, `include`, `-L`, `--library-path`, `-f`, `--from-file`, `modulemeta`, `get_search_list`) is the whole of what reads a file the line does not name | jq 1.8.1. **Re-verify** jq 1.8's changelog for any new option or builtin that reads a file or the environment; add each to the refusals |
| `ci/auditexec`, git | `/usr/bin/git` 2.56.1 | Same version on both images; nothing to re-verify beyond the path |
| The GNU check (model job, round J) | `(GNU coreutils)` on the first line of `/usr/bin/<tool> --version` | Stays, and fails closed on uutils. Runs **after** the switch, and in **all three jobs** |
| The workflow's own steps | GNU `sha256sum --check --strict -`, `stat -c '%U:%G %a %n'`, `od -An -N32 -tx1`, `tr -d`, `ls -A`, `install`, `base64 -w0` (the installer) | The switch makes them GNU again; without it they would run uutils, whose option parsing differs |
| `ci/internal/audit`'s bubblewrap check | `/usr/bin/bwrap --unshare-all --die-with-parent --ro-bind / / /bin/true` | bubblewrap 0.11.1; `/bin/true` is a coreutil (GNU after the switch). The check itself is unchanged; the first run shows it starts |
| The sandbox preflight | bash 5.2's `/dev/tcp` redirection and `${VAR:+word}` expansion (probes 2 and 5); `sudo -n true` (probe 4); the Docker socket at `/var/run/docker.sock` (probe 3) | bash 5.3, Docker 29, sudo **unconfirmed**. The probes judge the effects themselves, so a change fails closed; the first run must show "all six fail" |
| `Install the pinned tools` | bubblewrap and socat `.deb`s from noble-updates, digests from its `Packages` index | Resolute's versions and digests, from resolute's signed index (the suite that carries each, `resolute-updates` or `resolute-security`); `dpkg -i` must still find their libraries on the image |
| `.github/renovate.json5`'s deb rule | `suite=noble-updates`, `bubblewrap` and `socat` | Resolute's suites, and `coreutils-from-gnu` added to the rule (its `.deb`'s digest also by hand) |
| `bwrap.apparmor` and `TestBwrapAppArmorProfile` | AppArmor 4 (`abi <abi/4.0>`), the runner's userns restriction, no other profile for `/usr/bin/bwrap` | **Re-verify** all three; the comment's "Ubuntu 24.04 runners" changes; the profile's rules change only by a reviewed amendment here |
| The hardening step | `docker.service` and `docker.socket`, `/etc/sudoers.d/runner`, `sudo -n true` failing after its removal, `stat -c` | Docker 29's unit names, the sudo provider and the runner's sudo rule: **re-verify** (#15004 shows the privilege layout changed) |
| Go | No setup-go; the image's `go` switches to go.mod's toolchain (`GOTOOLCHAIN`) in `go mod download`; `go env GOVERSION` checked | The same tool cache on both images (1.27.4, go.mod's, among them; default 1.26), so the same mechanism; the check holds |
| `gh` | `/usr/bin/gh` (the artifact listing, the image check, the publish) | 2.104.0 on both; **re-verify** the path |
| `check.AuditWorkflow` (`auditRunner`) and its tests, `testdata/auditworkflow/valid.yml` | `ubuntu-24.04` on every job | `ubuntu-26.04`; still exact, still never `ubuntu-latest`; the switch step and the GNU check in every job join the jobs' closed step lists |
| actionlint (`tools/go.mod`, CI's lint-shell job, the checks job's row 5) | Knows the label | Blocked until a release knows it (condition 1) |

**The switch to GNU coreutils.** A model-free step, the first after checkout in each job and while sudo
works, installs `coreutils-from-gnu` from a `.deb` pinned by version and by the SHA-256 of resolute's
signed `Packages` index (as bubblewrap's is), removing `coreutils-from-uutils` as the release notes
show (`--allow-remove-essential`), without fetching anything else: `gnu-coreutils`, its one dependency,
is on the image already, since `coreutils-from-uutils` depends on it (to be confirmed by the probe). The
GNU check then runs. A switch that fails, or a tool that is not GNU after it, fails the job before the
first tool runs. *Why not point the wrapper at `/usr/bin/gnu<tool>`:* it needs no switch, but covers only
the wrapper; every guard of the workflow's own steps would run uutils, and the wrapper's table would
name paths only Ubuntu 25.10 and later have. *Why not adopt uutils:* the wrapper's option analysis
would have to be redone against a parser Ubuntu itself calls "not fully compatible yet", and redone
again as it moves; the GNU one is the one issue #277's review rounds read.

**The image probe** (before the move). A pull request, closed unmerged, adds one job to a CI workflow
of its own on `ubuntu-26.04` with `permissions: {}`, no secret, no environment and no third-party
action, which prints, and fails on none of: `dpkg-query` for `coreutils`, `coreutils-from-uutils`,
`coreutils-from-gnu`, `gnu-coreutils`, `rust-coreutils`, `bubblewrap`, `socat`, `apparmor`, `sudo` and
`sudo-rs`; `readlink -f` and the first line of `--version` of each of the 11 coreutils and of
`/usr/bin/sudo`; the same after the switch command above; `sysctl
kernel.apparmor_restrict_unprivileged_userns`; `sudo aa-status` filtered to profiles naming `bwrap`;
`apparmor_parser --version` and a dry load of `bwrap.apparmor` (`apparmor_parser -QK`); the sudoers
files' names (`sudo ls /etc/sudoers.d`, never their contents); `systemctl list-unit-files 'docker*'`;
`command -v gh git jq go` and `go version`; and `dpkg -i` of resolute's bubblewrap and socat `.deb`s
followed by the bubblewrap check's own invocation. Its log is the source the move's pull request cites
for every fact marked unconfirmed above; a fact that would change a decision here comes back as an
amendment first.

**The order:**

1. This amendment (issue #568).
2. The image probe (its pull request, closed unmerged), and an actionlint release that knows
   `ubuntu-26.04` pinned in `tools/go.mod` (Renovate's pull request), in either order.
3. The move: one pull request, test first (the tester's red tests on `check.AuditWorkflow`, then the
   change), that sets `runs-on: ubuntu-26.04` on the three jobs and its comment, moves `auditRunner` to
   `ubuntu-26.04` with the check's message (`want ubuntu-26.04, never a moving image: the wrapper's
   option analysis assumes the GNU coreutils the switch installs (ADR 0040)`), adds the switch step and
   the GNU check to every job's closed step list (each before the job's first download), replaces the
   bubblewrap and socat pins and digests with resolute's, adds the `coreutils-from-gnu` pin, moves the
   Renovate deb rule to resolute's suites, updates `bwrap.apparmor`'s comment (and its rules only if the
   probe shows they must change), and amends this record with the probe's facts and the GNU and jq
   changelog review. It names PR #561, which is then closed unmerged. PR #565 merges before it.
4. The first run on 26.04: runbook step 8 as for any first run, plus, from that run's log and summary:
   the switch step's and the GNU check's success in all three jobs; bubblewrap's check and "all six
   fail" in the preflight's line; the hardening step's three checks; `go env GOVERSION` equal to go.mod's;
   the round C, D and E notes above ("Risks for the first workflow run"), read again on the new image.
   Its result is recorded here as an amendment.

**How the pin test changes:** only its value and its message. `auditRunner` stays one exact label for
every job, a moving label (`ubuntu-latest`) stays refused, and its test cases that mutate the label move
from `ubuntu-24.04` to `ubuntu-26.04`. The new steps are held as every other step is: by name, in order,
with the commands their scripts must run.

**Alternatives considered for the label (condition 1):**

- **An actionlint configuration declaring `ubuntu-26.04`** (`self-hosted-runner: labels:`), as the
  error message suggests. ADR 0022 refuses any actionlint configuration file because one can silence
  actionlint's checks, and three checks hold that rule; declaring the label would also make actionlint
  treat it as a self-hosted label in every workflow. It needs an amendment of ADR 0022, which the
  maintainer may choose if no release comes; it is not taken here.
- **actionlint from an unreleased commit or a fork.** A pseudo-version of an unmerged pull request, or a
  fork of a project whose maintenance is in question, is a supply-chain change to a tool that runs on
  every workflow; rejected.
- **Skipping actionlint for the audit workflow.** It is the workflow that most needs it; rejected.

### The workflow (milestone 2, PR 5b, amended 2026-11-24)

PR 5b adds `.github/workflows/scheduled-security-audit.yml` and `check.AuditWorkflow`, which
`test/architecture`'s `TestScheduledSecurityAuditWorkflow` holds it to; PR 5c completes the checks job.
The frame is the one Decision's sections above give: `workflow_dispatch` with no inputs as the only
trigger until milestone 3, the guard condition verbatim in every job's `if:` and in the concurrency
group, `permissions: {}` at the top, the `security-audit` environment, `runs-on: ubuntu-24.04` and every
checkout of `github.sha` with `persist-credentials: false` and no sparse checkout.

**The check.** Each job's steps are a closed list, recognised by name and held in order, with the
commands each step's script must run: a step that is missing, out of order or not on the list is
reported by name; the hardening step's place before the model step and the App token step's in the
publish job alone are held this way. Every step is required, the model job's `Read the Claude Code pin`
too, the step `test/lint`'s install check wants before the install step (`id: claude-code-pin`). Only
`actions/checkout`, `actions/upload-artifact` and `actions/create-github-app-token` run, each pinned by
commit with a version comment. Every reference to a secret, a variable, `github.token` or the App token's
output is refused on any line but the one its step's spec allows, comments included; `secrets[`,
`toJSON(secrets)`, `secrets: inherit`, `set -x`, a `${{ }}` in a script, a path under `/tmp` or `/run`, a
job output and `actions/cache` are refused anywhere. The raw lines also hold the model job free of every
check and container command, so a document the YAML reader refuses still draws the message that names it.

Review round 1 (2026-11-24) widened the check: a step's spec may also name commands its script must not
run (comments aside) and commands it must run in a given order, and every copy of a `*_VERSION` or
`*_SHA256` pin in the file must hold the first copy's value, since Renovate moves them together and a
copy that differs is a hand edit that moved one alone.

Review round 2 (2026-11-24): **a comment is no command.** The commands a step must run, must not run and
must run in order are all matched against its script with every whole comment line (a line whose first
non-blank character is `#`) taken out, so a guard commented out (`# --deny-self-hosted-runners`, the
zip's `sha256sum --check --strict`, `-result-out`) is a guard missing, and a commented copy of a command
above the real one cannot satisfy the order. Only whole lines go: a `#` inside a code line
(`${digest#sha256:}`) is code. A trailing comment is refused outright (review round 3, H1): a code line
holding a `#` at the start of a word (round 3's `(^|\s)#`, widened below) is a problem, because the shell drops the rest of the
line, a trailing `\` included, so a guard hidden after it still matches every pattern while the shell
never runs it, and a `--source-ref "refs/tags/${tag}" # \` cuts the continuation so that
`gh attestation verify`'s failure is ignored (only the last command's status counts in
`if ! A; B; then`). A word starts after a blank and right after an operator character too
(review round 4, M1), and right after a backtick (review round 5), which opens a command substitution:
``(^|[\s;&|()<>`])#``, so `;#`, `&&#`, `|#` and `` `# `` are trailing comments as well. A `#`
inside a word (`${digest#sha256:}`, `${GITLEAKS_VERSION#v}`) stays code; one after a blank or an
operator character inside quotes is refused too, which fails closed. A whole comment line right after a
line ending in `\` is refused as well: the continuation joins it to the command as a trailing comment,
and since whole comment lines are taken out before matching, the command would otherwise look whole.
A guard cut with no `#` at all is held by matching it whole (review round 4, M1): the verification is
one contiguous pattern, from `gh attestation verify "oci://${image}@${digest}"` through `--repo`,
`--signer-workflow` and `--source-ref`, each on a line ending in ` \`, to `--deny-self-hosted-runners`
ending its line, alone or as `if ! ... > /dev/null; then`, so a `;` after a flag or a dropped `\`
(either leaves `--deny-self-hosted-runners` to run as a command of its own, whose status alone counts)
matches no more; and the zip's `echo "${digest#sha256:}  ...zip" | sha256sum --check --strict -` must be
a whole line, so it cannot ride at the end of another command's line. A guard's refusing branch is
held whole too (review round 5, L1): the tag's and the digest's shape checks and the verification in its
`if !` form each match only with their branch, an `echo "::error ..."` line, an `exit 1` line and a `fi`
line, so a branch whose `exit 1` is dropped or turned into `exit 0` is a guard missing; the bare
verification stays accepted, `set -e` failing the step. Review round 6 holds the rest the same way: the
attestation step's `if ! digest="$(jq -er ...)"` read, and in both download steps the shape guard of the
artifact's ID and digest (`if [[ ! "${id}" =~ ^[0-9]+$ ]] || [[ ! "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]]`,
between the digest's read and the zip's download) and the single-file `ls -A` check, each with that
branch; the empty digest's branch is held whole as `then`, `echo "image not pulled: ..."`, `exit 0` and
`fi`, each a line of its own, so none of them can be run together onto one line. In today's workflow the verification is the only
command continued over lines; the check does not require that, it only matches the verification as one
contiguous command. An order broken is reported for every pair out of order, the command that
must come first named first.

**The workflow check's residual risk (accepted, review round 5).** The check is a structural tripwire
over the workflow's text, not a shell parser. It refuses the edits that make a guard look present and
whole while the shell never runs it, or runs it cut: a guard commented out, hidden after a trailing
comment (a `#` at the start of a word, after an operator character or a backtick), a continuation cut
by a `;`, a dropped `\` or a comment line, a branch without its `exit 1`. A guard can still be
neutralised by an edit that leaves it visibly in place: `|| true`, `|| :` or `|| exit 0` on a guard's
line, a `||` ending the line before a bare command, the guard inside `if false`, in a function nobody
calls or in a heredoc. Each such edit is explicit in the diff, and reaches `main` only through a pull
request the maintainer reviews (CODEOWNERS and `Protect main`'s required code-owner review); the check
does not try to read shell semantics to refuse them.

**Job permissions** (`GITHUB_TOKEN`, each read): the checks job `contents` alone (no row PR 5b runs reads
an attestation: PR 5c adds `attestations` with its release-integrity rows, and scorecard and row 15 may
add `actions` or `issues` after the first run); the
model job `contents` and `actions`, the artifact listing and download; the publish job `contents`,
`actions` and `attestations`, the listing, the download and the image check. No `packages`: the image is
public.

**Go without `actions/setup-go`.** No job runs setup-go, whose default restores an Actions cache. Each
job's `go mod download`, run in the module, fetches go.mod's toolchain (`GOTOOLCHAIN`, checked against
the checksum database) with the modules (and `go -C tools mod download` in the checks job). The checks
job's `go install golang.org/x/vuln/cmd/govulncheck@<version>`, which runs before it, does not: `go
install pkg@version` picks its toolchain from the target module's own go line, not from the checkout's
go.mod, so the switch to go.mod's Go happens in "Download the Go modules". Every later Go command, the
builds of `ci/auditexec`, `ci/auditrun` and `ci/auditpublish` included, runs with `GOFLAGS=-mod=readonly
GOPROXY=off`, and govulncheck with the same. The checks job then checks that `go env GOVERSION` is
go.mod's.

**The encryption tool is `age`** (v1.3.2, pinned by version and the SHA-256 of
`age-<version>-linux-amd64.tar.gz` in each job). The public halves are the variables
`AUDIT_MODEL_PUBLIC_KEY` and `AUDIT_PUBLISH_PUBLIC_KEY`, read only in the step that encrypts to them. Each
private half reaches `age --decrypt --identity -` on stdin, from that one step's `env:`, and is never
written to a file. A ciphertext that does not decrypt fails the step, and `age` writes no output.

**The artifact hand-off.** Each downloading job first runs `auditpublish artifacts -job model|publish`,
which fails closed on an extra, a duplicate, a missing or an expired artifact, and writes the artifact
the job downloads, as the checked listing names it, to `$RUNNER_TEMP/artifact.json` (`O_EXCL`, mode
0600, exactly `{"name":...,"id":...,"digest":"sha256:..."}`; a path that already exists is outcome 9, and
a refused listing writes nothing): its numeric ID and its `digest`, the SHA-256 that `upload-artifact`
computes of the zip it stores. The same step reads the ID and the digest from that file, never from a
second listing (PR 5b review round 1: a second call could answer otherwise than the one the check
accepted), downloads exactly that ID through `gh api .../actions/artifacts/<id>/zip`, checks the zip
against the digest with `sha256sum --check --strict`, unzips it, and refuses a zip that holds anything but
the one `.age` file (`ls -A`). The model job checks the decrypted archive before unpacking it (`tar
-tvf`): only directories and regular files under `results/`, none with a `..` element, unpacked as root
into `/srv/audit/checks` with `--no-same-owner --no-same-permissions`, then every write bit removed, and
the decrypted archive and its two listings removed from `$RUNNER_TEMP` (`rm -f`) in the same step. The
check holds each of these commands, in that order.

**The checks job (5b)** runs rows 1 to 5 and 16: the commit and the latest release's tag, the pinned
tools (gitleaks v8.30.1, osv-scanner v2.7.1, zizmor v1.33.1, age, each by SHA-256; govulncheck v1.10.0 as
`security.yml` pins it, and actionlint from `tools/go.mod`), govulncheck and osv-scanner on the module and
`tools/`, gitleaks on the whole history with `--redact` and `--ignore-gitleaks-allow` (no allow comment
in the history hides a secret), and zizmor and actionlint on the workflows, in two steps. Each
tool's output, its stderr and its exit code go to `$RUNNER_TEMP/results`, never to the log: a finding is
data for the model, not a failure of the job. `results/not-run.txt` names rows 6 to 11 and 13 to 15, so
the result reports them "not run", never "clean". `github.token` reaches only the row 1 step and the
zizmor step's `env:` (zizmor's online audits); the actionlint step holds none.

**The model job** installs `bubblewrap` (0.9.0-1ubuntu0.3) and `socat` (1.8.0.0-4ubuntu0.1) from the
Ubuntu archive, each `.deb` checked against the SHA-256 of `noble-updates`' Packages index (whose
`InRelease` signature was checked against the Ubuntu archive key when the pin was taken) before
`dpkg -i`; Renovate's `deb` datasource reads that suite (a package rule in `.github/renovate.json5`) and
the digest is replaced by hand in the same PR. It loads `.github/security-audit/bwrap.apparmor`
(`TestBwrapAppArmorProfile` pins its rules line for line), checks that `base64`, `cat`, `cut`, `head`,
`od`, `sha256sum`, `sort`, `tail`, `tr`, `uniq` and `wc` under `/usr/bin` are GNU coreutils', and builds
the wrapper, the launcher and the artifact check. The audit directory is made then, while sudo works:
`/srv`, `/srv/audit`, `bin/` and `bin/audit-exec` are root's (`bin/` and the wrapper mode 0555); `checks/`
is root's, every write bit removed; `work/` and `state/` are the runner's, as the launcher writes them.
**`checkout/` stays the runner's** (round L's owner question): git refuses a repository another user
owns, and the wrapper's git reads no global or system configuration where a `safe.directory` could be
set. What holds that tree against its owner is the sandbox's write allow list, which names neither tree,
and the layout check's mode walk. The model-free hardening step then stops and disables Docker, checks
the owners and modes above (`stat`), removes `/etc/sudoers.d/runner`, and fails unless `sudo -n true`
fails and the Docker socket is gone. The model step runs `auditrun -claude-tarball
$RUNNER_TEMP/claude-code/claude-code.tgz -claude-integrity "${CLAUDE_CODE_PIN_INTEGRITY}" -result-out
$RUNNER_TEMP/result.json`, the integrity from the pin step's output (`test/lint` holds that argument, and
the installer's two last arguments, exactly, and refuses a `sha512-` literal on every line but the pin
step's anchored `grep -E '^integrity: sha512-...'` of the pin file, other lines of the pin step included:
review round 2). **The result reaches the encrypt step through the launcher alone** (PR 5b review round 1): the
launcher's `Run.Result` writes the bytes its result check read and accepted (a regular file and no link,
within the cap, one JSON value, neither the token nor the canary) to `-result-out`, a new file (`O_EXCL`,
0600) under the step's `$RUNNER_TEMP`, out of the sandbox's reach, and the encrypt step encrypts that
file; `/srv/audit/work/result.json` is never read again by its path, which the check refuses there. **The
canary is made in the model step for each run** (32 bytes of `/dev/urandom` as
hex, exported to the launcher alone): a constant in the workflow would be in the checkout copy the model
reads, and a result that quoted it would be refused.

**The publish job** downloads gitleaks and age and builds `ci/auditpublish` offline. After the
`image` subcommand it runs `gh attestation verify oci://ghcr.io/<owner>/<name>@<digest> --repo
<owner>/<name> --signer-workflow <owner>/<name>/.github/workflows/release.yml --source-ref
refs/tags/<tag> --deny-self-hosted-runners` as a model-free step with the job's `github.token` and
`attestations: read`, the digest and tag read from the result and held to their shapes first, the tag's
whatever the digest is; a refusal fails the run. **The step stands alone** (PR 5b review round 2): it
trusts no earlier step to have read the digest, and reads it with `jq -er '.image_digest | select(type ==
"string") | select(. == "" or test("^sha256:[0-9a-f]{64}\\z"))'`, refusing anything but a string that is
exactly empty or one sha256 digest (`\z`, not `$`, which also matches before a final newline) before the
empty branch; the check requires that read, before that branch. An empty `image_digest` is the result's
documented "image not pulled", which `publish.Result` and `CheckImageDigest` accept: the step then prints
the fixed notice `image not pulled: no provenance attestation to verify` and verifies nothing, rather than
failing every such run (PR 5b review round 1). That closes the image check's residual risk above: the
attestation's Sigstore signature and certificate are verified before a digest is trusted. The `image`
step's `CheckRelease` also holds the result's tag to the repository's latest release, so a result that
names an older release is refused before anything is written. That includes a release published or
marked latest while the run is under way: the checks job read the tag before it, so the result names the
older one and the run fails closed (`ErrResultInvalid`), publishing nothing; the maintainer starts a new
run, which audits the new release (runbook step 11). gitleaks runs `stdin
--ignore-gitleaks-allow --redact` over the decrypted result **before** the App token is minted, so a
result it refuses has no write token minted for it; the App token step has exactly the four
`permission-*` inputs of Decision 1 (`contents`, `pull-requests`, `issues`, `security-events`, each
`write`); the publish step alone holds the App's token and `AUDIT_FINGERPRINT_KEY`.

**Time budget:** checks 180 minutes, model 120 (its model step 90, around the preflight's 10-minute and
the audit's 40-minute deadlines), publish 30; every step has its own `timeout-minutes`.

**The Actions cache (residual risk).** Anything in the checks job runs as the runner's user and could read
the Actions runtime token and save cache entries scoped to `main`, which every other workflow on `main`
restores by key. The audit cannot stop that; the rule is that **no release-relevant workflow restores an
Actions cache**: `release.yml`'s setup-go steps set `cache: false`, which `test/architecture`'s
`TestReleaseWorkflowDisablesTheGoCache` and `TestGoCacheGaps` pin, and the audit workflow itself restores
none (no `actions/cache`, no setup-go; the check refuses the first). CI workflows on `main` that do
restore a cache could read a poisoned entry; their results are checks, not releases, and that is accepted.

**Left to PR 5c:** rows 6 to 11 and 13 to 15 and their containers and flags; the trivy, cosign and
scorecard pins; the checks job's final read scopes after the first run; a check that times out recorded in
the report's limits while the run still publishes (today a step timeout fails the job); and failure
summaries that name the runbook step for every cause (today the programs' one lines and the workflow's
own errors do; a tool download whose checksum differs fails with sha256sum's line alone, the hardening
step's commands that fail under `set -e` before its own checks, such as `systemctl stop`, fail with their
own lines, the errors that point at runbook step 11 name none of their causes there yet, and the hardening
step's three own errors, wrong owners or modes of the audit directory, `sudo -n true` still succeeding
and the Docker socket still there, point at runbook step 10, which names only preflight and bubblewrap
failures). **To be checked
on the first run** (runbook step 8): that `socat`'s and `bubblewrap`'s `.deb`s install with the image's
libraries (`dpkg -i` does not fetch a missing dependency) and that the pinned versions are still in the
archive's pool; that the runner's sudo rule is `/etc/sudoers.d/runner`; that the runner's preinstalled
Go switches to go.mod's toolchain; that govulncheck runs offline on what `go mod download` fetched; and
the round E note on an empty section header in the checkout copy's `.git/config`.

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
  label, and check that no other App holds Actions write, before milestone 2 merges
  ([The scheduled security audit](../dev-environment.md#the-scheduled-security-audit) in
  `docs/dev-environment.md`, the runbook PR 5a moved there from the PRD on 2026-11-24, its step numbers
  kept).
- Milestone 2 amends this record; milestone 3 enables the schedule after the first report is merged.
