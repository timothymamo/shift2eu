# 0040. Audit the repository and the latest release on a schedule, with a model that triages and a script that publishes

- Status: Accepted, Decision 2's presence-flag rule and the coverage rule that follows from it included:
  the maintainer confirmed both on 2026-11-21 at PR #444's review (see Context).
  PRD #175 milestone 2 amends this record with the workflow's implementation (below)
- Date: 2026-11-21

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
| Contents write (App permissions cannot be narrowed to a path or branch) | creating releases and tags other than `v*`, deleting and merging into unprotected branches, `repository_dispatch` | the token exists only in the publish job, which runs only `main`'s committed script and calls a fixed set of endpoints; the `security-audit/**` branch ruleset (only the App writes there; an admin only through a temporary bypass the runbook adds and removes); a required check that fails an App pull request touching anything outside `docs/security/`, in a new ruleset on `main`; a branch ruleset on `~ALL` excluding `security-audit/**` that restricts creations, updates and deletions to its bypass actors (the maintainer's admin role, the agents App, Renovate, the release-please App), never the audit App; optionally a `**` tag ruleset that lets only the release App create tags |
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
  targets `~ALL` excluding `security-audit/**`, restricts creations, updates and deletions (`creation`,
  `update`, `deletion`), and has as bypass actors every legitimate pusher (the maintainer's admin role (`RepositoryRole` 5), the agents
  App `shift2eu-agents`, Renovate, the release App `shift2eu-release`) and not the audit App, which can
  then write only `security-audit/**` branches. The residual risk is bounded by the fence plus this
  ruleset; without the ruleset, the sender key is only a one-shot signal.
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
  branch runs that branch's copy, which could differ. The branch ruleset on `~ALL` keeps the audit App
  from changing any other branch's copy; without it, the App's contents write could. Retargeting the pull
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
  exactly `bytes`, `crypto/hmac`, `crypto/sha256`, `encoding/base64`, `encoding/hex`, `encoding/json`,
  `errors`, `fmt`, `math`, `regexp`, `slices`, `strconv`, `strings`, `time`, `unicode` and
  `unicode/utf8`, and denies `os` and `net` with a reason. Go's internal rule keeps it out of reach of
  `cmd/` and `internal/`.
- `ci/auditpublish`, the entry point (milestone 2's fourth pull request): a main package that reads the
  decrypted result and the merged reports, the run's variables and the fingerprint key, runs gitleaks
  over the texts the core built, calls the fixed GitHub endpoints through `gh`, and prints the one
  summary line. Its strict rule, `audit-publish-main`, and the forbidigo group for its environment reads
  are set with it; it imports the core and named standard packages only, no third-party package.

**The result schema.** The model writes one JSON file, `result.json` in the work area (the model job's
task names that path, as the `security-audit-run` skill asks; the skill's "the name and the schema the
task gives"). The core's `ParseResult(data []byte, commit string) (Result, error)` accepts it only if all
of this holds, and otherwise returns an error wrapping `ErrResultInvalid`:

- at most `MaxResultBytes` (4 MiB) in all, one JSON object and nothing after it;
- no unknown field, no duplicate key at any level (encoding/json keeps the last of two keys silently, so
  a second `severity` could hide behind the first; the core walks the tokens to refuse it), every field
  present, no `null`, every value of its type;
- every string valid UTF-8, its cap counted in **bytes**, and no C0 or C1 control character, DEL, U+2028,
  U+2029, bidirectional control (U+061C, U+200E, U+200F, U+202A to U+202E, U+2066 to U+2069) or
  zero-width character (U+200B to U+200D, U+2060, U+FEFF); a field marked *multi-line* below may hold LF
  and TAB, no other field may;
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
| `image_digest` | string | `^sha256:[0-9a-f]{64}$`, or empty when the image was not pulled |
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
| `findings[].evidence` | string, multi-line | at most 16000 bytes: the PoC or trace, used in an advisory or an issue only |
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
  one LF, then the fence. No line of the text can close it.

Every other byte of a published text is the script's own: headings, labels, the fixed vocabularies,
dates, the run's URL, the SHA, the tag, the counts and the fingerprints. The control and invisible
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

`Placement.Private() bool` is true for the three advisory placements. Nothing a finding holds can move it
up this table: the model's flag is read only to make a finding private (row 5).

**Fingerprints.** `Fingerprint` is `fp-` and 12 lowercase hex characters: the first 6 bytes of a digest of
the input `"shift2eu-audit-fingerprint/v1" NUL rule NUL file NUL sink` (NUL is the byte 0x00, which none
of the three fields may hold, so the encoding is unambiguous; the version prefix lets a later change of
the input start a new series on purpose). `PlainFingerprint(rule, file, sink string) Fingerprint` uses
SHA-256, for a public finding and for every false-positive match. `KeyedFingerprint(key FingerprintKey,
rule, file, sink string) Fingerprint` uses HMAC-SHA-256 with `AUDIT_FINGERPRINT_KEY`, for a private finding.
The key is the secret's value as bytes, unchanged; `NewFingerprintKey(b []byte) (FingerprintKey, error)`
refuses fewer than 32 bytes (`ErrConfig`), the runbook's minimum. A private finding is never given a
plain fingerprint anywhere public, so nobody can test a guess at its rule, file and sink against the
committed script. A finding's line is not an input: a finding keeps its fingerprint when code above it
moves.

**Status and false positives**, derived from the **previous report**: the newest file in the checkout's
`docs/security/audits/` whose name matches `^[0-9]{4}-[0-9]{2}-[0-9]{2}-[1-9][0-9]{0,19}\.md$`, by date
and then run ID as a number. The checkout is `github.sha` on `main`, so every report read is one the
maintainer merged. `ParsePreviousReport(name string, data []byte) (Previous, error)` reads its findings
table, its needs-investigation list and its false positives by their fingerprint cells and their fixed
vocabulary cells only, and returns an error wrapping `ErrPreviousReport` on anything it cannot read
exactly as the core renders it; with no such file, the run is the first. For the kept public findings
(placements 6 and 7):

- not in the previous report: `new`;
- in its findings table as `new` or `open`: `open`, and in Changes since the last audit "still open";
- in its findings table as `fixed`: `open`, and "regressed";
- in the previous findings table as `new` or `open` and not found in this run: a row `fixed` in this
  report's table (fingerprint, severity, category and status; the other cells `—`), and "fixed";
- a needs-investigation entry is `new`, or `open` when either section of the previous report holds its
  fingerprint; it gets no `fixed` row and no Changes line.

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
  `\bgh[pousr]_[A-Za-z0-9]{36,}` and `github_pat_[A-Za-z0-9_]{22,}` (GitHub tokens),
  `(?i)AGE-SECRET-KEY-1[0-9A-Z]{58}` (the model and publish keys' private halves), the fingerprint key's
  own bytes, and, over decoded bytes only, a DER private key's start, `\x30(\x82..|\x81.|[\x00-\x7f])\x02\x01[\x00\x01]`.
- **The encoded-token check**, over the whole text: every maximal run of at least 16 base64 or base64url
  characters (`=` padding included, `/` included) is decoded with the standard and the URL alphabet,
  padded and raw, dropping one trailing character when the length leaves 1 over 4; every maximal run of
  at least 16 hex characters is decoded, at both even offsets when its length is odd. The decoded bytes
  are matched against the same patterns, and searched once more the same way (a token encoded twice).
- **The encoded-run refusal**, for **public text only** (the report and the pull request's and issues'
  texts) and only over **model-written fields after escaping**: the renderer returns, with each text,
  the escaped fragments it built from the result. A run is a maximal sequence of the bytes
  `[A-Za-z0-9+=_-]`: every other byte ends one, `/`, `.`, `\` and every other separator included, so a
  path is many short runs, and the `\` of an escaped `\|` ends a run as the `|` would. A run of at least
  `EncodedRunMinLength` bytes whose Shannon entropy over its own byte frequencies is at least
  `EncodedRunMinEntropy` bits per character is refused. Values the script inserts are never model
  fields: the `main` SHA, the release tag, the run's URL, the dates and the fingerprints.
- **L, the minimum length, is 24.** A fingerprint is 15 characters, so L is longer, as the PRD asks; 24 is
  the base64 length of a 128-bit secret, the smallest worth stealing, and every credential the patterns
  name is longer. Below it, ordinary words and identifiers would trip the entropy test.
- **N, the entropy, is 4.1 bits per character.** Hex has 16 symbols, so its entropy never exceeds 4.0:
  commit SHAs and digests a finding cites never trip the refusal, and a hex-encoded token is the
  encoded-token check's to find. Random base64 of 24 characters reaches 4.1 in about 85% of samples, of 28
  in about 98%, of 32 or more in more than 99.9%; a Claude OAuth token, about 100 characters, always.
  A long identifier with no separator (a 34-letter camel-case test name can reach 4.2) also trips it: the
  refusal then fails closed on a harmless text, which costs a run, never a disclosure.
- **Advisory text** gets the patterns and the encoded-token check, **not** the refusal: an advisory holds
  PoCs and crash inputs, which are often base64.
- `ScanPublic(body string, modelFields []string, key FingerprintKey) error` and `ScanAdvisory(body
  string, key FingerprintKey) error` return an error wrapping `ErrScanRefused` on any hit, which names no
  text. **On a hit nothing is published**, not the report, not an advisory, not an issue, and the summary
  is exactly `publish refused: see runbook`.
- **Residual risk, encoded tokens.** The scan finds a token in the clear, in base64 or hex, or encoded
  twice that way, and refuses high-entropy runs in public text. It does not find a token split across
  fields or runs (a separator every 23 characters defeats the refusal), encoded any other way (base32,
  percent-encoding, a cipher, a compression), written below L, or below N. A model steered to leak the
  subscription token could so reach the public report. The model step's containment is what keeps the
  token from the model (Decision 6); the scan is the last line, and runbook step 10 rotates the token.

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

**Rendering.** `RenderReport(in ReportInput) (Rendered, error)`, with `Rendered{Path, Body string;
ModelFields []string}`, writes `docs/security/audits/YYYY-MM-DD-<run_id>.md` (the run's UTC date, its run
ID). Its first line is the one title, `# Security audit report, YYYY-MM-DD`, and its sections are the
template's, in its order, each a `##` heading: Summary, Scope, Findings (with `### Needs
investigation`), Private findings, Coverage, Changes since the last audit, False positives, Limits. Its
only links are `[Security](../README.md)` in the summary and the run's URL in the scope, so
`test/docs`' title and link checks pass on it; model text is in code spans, which those checks skip.

- **Summary**, fixed text: the commit, the tag, the public total by severity and the needs-investigation
  count, and the private-findings line.
- **Scope**: Commit, Release (tag and image digest), Tool versions (one item per tool), Model
  (`claude-fable-5-2`, effort high, the core's constants, not the result's), Run (URL and date).
- **Findings**: the table `Fingerprint | Severity | Category | Status | Location | Scenario | Recommended
  fix`, sorted by severity then fingerprint, the location `file:line` (or `file` when the line is 0) and
  every model cell a `TableCell`; "No public findings." when empty. **Needs investigation**: the same
  columns, "None." when empty.
- **Private findings**: exactly one of `private findings: handled privately`, `private findings: not
  filed` or `private findings: none`, from `PrivateState` (`PrivateNone`, `PrivateHandled`,
  `PrivateNotFiled`), which the entry point sets after filing; `PrivateNone` with any private placement,
  or another state with none, is an error.
- **Coverage**: the table `Check | Coverage`, every check id in the order above.
- **Changes since the last audit**: the per-fingerprint lines above, then the gap line when there is a
  gap, then the dependencies and the narrative as `CodeSpan`s; "First report." when there is no
  previous report.
- **False positives**: the table `Fingerprint | Reason`, "None." when empty. **Limits**: one item per
  limit, "None." when empty.

The fixed templates, filled only with the run's UTC date, the run ID, the `main` SHA, the release tag,
the public total, the private-findings line and the run's URL, never with model text:

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
`PlacementInvestigation`.

**The gap threshold** is one pinned constant, `GapThresholdDays = 38` (the longest month, 31 days, plus 7
days of slack, for the cron `17 4 3 * *`). The Changes section says `Gap in the schedule: the previous
report is <n> days older than this one.` when the previous report's name's date is more than 38 days
before the run's date, counted in whole UTC days. A `test/architecture` check reads the core's source
and fails `make check` unless `GapThresholdDays` is declared exactly once, as an integer literal, and
equals 38 while the workflow has no `schedule` trigger. Milestone 3, which adds the trigger, changes it
to the PRD's full check: at least the longest gap between two consecutive dates the cron names plus 7
days, for the cron forms `M H D * *` and `M H * * W` only, and the same number in the staleness check of
the `release-tag` and `dispatch` skills.

**Exit codes and the summary line.** The entry point prints exactly one line, to standard output and to
the step summary, chosen by `OutcomeOf(err error) Outcome` from the error's class, never from its text,
and exits with `Outcome.Code()`; `Outcome.Summary()` holds no finding's text, no count of private
findings, no API response and no error message:

| Outcome | Code | Summary |
|---|---|---|
| `OutcomePublished` | 0 | `audit published: public findings <total>, <private-findings line>` |
| `OutcomeFailed` (any other error) | 1 | `publish failed: see runbook` |
| `OutcomeResultInvalid` (`ErrResultInvalid`, a result that does not decrypt included) | 3 | `result refused: see runbook` |
| `OutcomeScanRefused` (`ErrScanRefused`) | 4 | `publish refused: see runbook` |
| `OutcomePrivateNotFiled` | 5 | `private findings not filed: see runbook` |
| `OutcomeDuplicateDrafts` | 6 | `publish refused: duplicate draft advisories: see runbook step 11` |
| `OutcomePreviousReport` (`ErrPreviousReport`) | 7 | `previous report unreadable: see runbook` |
| `OutcomeGitHub` | 8 | `GitHub call failed: see runbook` |
| `OutcomeConfig` (`ErrConfig`) | 9 | `configuration invalid: see runbook` |

Code 2 is left to the Go runtime's panic exit. Only `OutcomePublished` and `OutcomePrivateNotFiled` have
published anything; the second still fails the job, so GitHub notifies the maintainer. The errors the
core returns name a field's path (`findings[3].scenario`) and the rule it broke, never a value, and the
entry point prints none of them.

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
