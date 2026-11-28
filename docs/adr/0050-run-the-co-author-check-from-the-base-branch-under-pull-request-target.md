# 0050. Run the co-author check from the base branch's workflow under pull_request_target

- Status: Accepted. Amended 2026-11-28 (issue #723): the two-step rollout is done. Issue #706's pull
  request (PR #711) added the workflow, the maintainer required `Co-author trailers (base)` on
  2026-11-28, after the probe (PR #722, closed unmerged), and issue #723's pull request removed
  `ci.yml`'s legacy `coauthor-trailers` job and `Lint`'s need of it (Decision, What the pin asserts,
  Consequences). `ci.yml` holds no co-author job, `Co-author trailers (base)` is the only co-author
  check, and the `${{` rule covers the job's whole block.
- Date: 2026-11-27

## Context

The co-author check (`TestBranchCoAuthorTrailers`, `test/lint/coauthortrailer_rule_test.go`) refuses a
pull request whose commits carry a co-author address, an author address or a committer address that
`.github/co-author-addresses.txt` does not list, since a squash merge credits every distinct author
(issues #653 and #663). It is part of a required check: today it runs as the `coauthor-trailers` job of
`.github/workflows/ci.yml`, and the required `Lint` job needs it.

Two pull requests moved what the check trusts to the base commit:

- PR #683 (issue #672): the allow list is read from the range's base commit, never from the checkout.
- PR #703 (issue #689): the job runs the base commit's copy of `test/lint`, from a detached worktree of
  the base, under the base's `go.mod` and Go, and the check's git calls ignore the global and the system
  git configuration.

What is left comes from the pull request itself (issue #706). The job runs on `pull_request`, so GitHub
reads its steps from the pull request's own `ci.yml`, and the pin of those steps
(`test/architecture/cilint_test.go`, `checkCoAuthorTrailersJob`) runs from the pull request's own
checkout. A pull request can rewrite the job (run its own `test/lint`, drop the range, `exit 0`) and
loosen the pin in the same diff, and both checks pass. Only the maintainer's code-owner review stands in
the way. The checks should not depend on that review.

The repository is public and owned by a user account (`timothymamo/shift2eu`), not an organization.
`Protect main` requires `Lint`, among other checks, and code-owner approval; its bypass list is empty.

## Decision

**The co-author check moves out of `ci.yml` into a workflow of its own,
`.github/workflows/coauthor-trailers.yml`, triggered by `pull_request_target`. GitHub reads that
trigger's workflow from the base branch, never from the pull request, so for every pull request into
`main` the job definition, the `test/lint` that runs, its Go and the allow list all come from `main`.
The job checks out only the base commit, fetches the pull request's commits as git objects that it never
checks out or runs, holds a read-only token and uses no secret.** Its job name, `Co-author trailers
(base)`, is new, and the maintainer makes it a required check pinned to GitHub Actions.

This is the pattern `.github/workflows/audit-paths.yml` already uses for the audit's required path check
(ADR 0040, "Audit report paths"), and the same reasoning applies: the trigger is what makes the check
hold, and the job runs no line of the pull request's tree.

### The workflow

```yaml
name: Co-author trailers

on:
  pull_request_target:
    types: [opened, synchronize, reopened, edited]

permissions:
  contents: read

concurrency:
  group: ${{ github.workflow }}-${{ github.event.pull_request.number }}
  cancel-in-progress: true

jobs:
  coauthor-trailers:
    name: Co-author trailers (base)
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      # No ref: and no repository: under pull_request_target this is the base branch's commit, github.sha.
      - uses: actions/checkout@<pinned sha> # <version>
        with:
          persist-credentials: false
          fetch-depth: 0
      - name: Fetch the pull request's commits as objects, never checked out
        env:
          PR_NUMBER: ${{ github.event.pull_request.number }}
        run: |
          if ! [[ "${PR_NUMBER}" =~ ^[1-9][0-9]*$ ]]; then
            echo >&2 "coauthor-trailers: the pull request number is not a number: the check fails closed"
            exit 1
          fi
          git fetch --no-tags --no-recurse-submodules origin "+refs/pull/${PR_NUMBER}/head:refs/remotes/pull/head"
      - uses: actions/setup-go@<pinned sha> # <version>
        with:
          go-version-file: go.mod
          cache: false
      - name: The pull request's co-author trailers name the project's addresses
        run: |
          go test -count=1 -v -run '^TestBranchCoAuthorTrailers$' ./test/lint/
        env:
          SHIFT2EU_COAUTHOR_RANGE: ${{ format('{0}..{1}', github.sha, github.event.pull_request.head.sha) }}
          SHIFT2EU_COAUTHOR_CHECK: require
```

The action pins are the ones `ci.yml` uses on the day of the change; Renovate keeps them current, as it
does every workflow's.

- **Trigger.** `pull_request_target` with these four `types:` and no `branches:`, `branches-ignore:`,
  `paths:` or `paths-ignore:`: a required check that never reports blocks its pull request for good. A
  push moves the head (`synchronize`), and retargeting the base branch is an `edited` event, so a pull
  request that passed into another branch and is then retargeted to `main` is checked again by `main`'s
  copy. No `push` trigger: on `main` the old job only skipped.
- **One commit for everything that is trusted.** Under `pull_request_target`, `github.sha` is the tip of
  the base branch, the commit GitHub read the workflow from. The checkout takes it (no `ref:`), so the
  `test/lint` that runs, the `go.mod` setup-go reads and the allow list the check reads (from the range's
  base, PR #683) all come from that one commit. PR #703's detached worktree of the base is no longer
  needed: the checkout already is the base, and its four steps become the four above.
- **The range is `github.sha..head.sha`.** Those are the pull request's commits that the base branch's
  tip does not hold, the commits a merge would add. The check needs the full head SHA, which only the
  fetch brings, and the whole history (`fetch-depth: 0`) for the merge base. `SHIFT2EU_COAUTHOR_CHECK` is
  always `require`: the workflow runs on pull requests only, so a range the run cannot find, for
  example after a force-push while it ran, fails the check closed, and the push's own run reports.
- **The pull request's code never runs.** Its commits are fetched as objects and read only through
  `coAuthorGit` (`git log` and `git show` with `--no-replace-objects`, `GIT_CONFIG_GLOBAL=/dev/null`,
  `GIT_CONFIG_NOSYSTEM=1`, issue #689). No step checks them out, switches to them, adds a worktree of
  them or builds them. Attributes, hooks and `go.mod` come from the base's working tree.
- **Kept from today:** no `${{ }}` inside `run:` (every event value reaches the shell through `env:`, and
  the pull request number is checked to be a number before it becomes part of a refspec),
  `persist-credentials: false`, the base copy of `test/lint`, the isolated git configuration. In the job,
  `${{ }}` appears only in `env:`. setup-go reads a plain `go.mod`, so the new workflow needs no
  exception for its `go-version-file`; PR #703's exception stays for the old job until the follow-up
  removes both (both removed by issue #723).
- **Token and secrets.** Workflow-level `permissions: contents: read`, no job-level `permissions:`, no
  `secrets.` anywhere in the file, no `environment:`. The repository is public, so the fetch needs no
  credential.
- **No build cache.** `cache: false`: under `pull_request_target` a cache entry is saved in the base
  branch's scope, which every later run would restore (the same reason ADR 0033 gives for the AI
  reviewer).
- **`ci.yml` loses the job, in a second pull request.** The move is a two-step rollout, because
  `pull_request_target` reads the workflow from `main` and `main` holds no `coauthor-trailers.yml` until
  this pull request merges. Removing the old job in the same pull request would leave that pull request,
  and every other one opened before the merge, with no co-author check at all.
  1. **This pull request** adds `coauthor-trailers.yml` and keeps `ci.yml`'s `coauthor-trailers` job
     (`Co-author trailers`) and `Lint`'s need of it as they are. It is checked by that old job, which
     runs the base commit's copy of `test/lint` (PR #703) and is defined by the pull request itself. The
     new workflow first runs on pull requests opened or updated after the merge.
  2. **A follow-up pull request** removes the old job, `Lint`'s need of it (`Lint` then needs only
     `lint-target` and `lint-shell`) and the transition parts of the pin. It is merged only after the
     maintainer requires `Co-author trailers (base)`, and it is the first pull request checked by
     `main`'s definition alone.

  Both steps are done (Status).

### What the pin asserts

`test/architecture` pins the new workflow file exactly (and, until issue #723, pinned the old job in
`ci.yml` for the transition):

- `on:` holds `pull_request_target` alone, its `types:` exactly `[opened, synchronize, reopened,
  edited]`, and no other key (no `branches:`, `branches-ignore:`, `paths:`, `paths-ignore:`).
- The top-level `permissions:` is exactly `contents: read`. The `concurrency:` group is keyed on the
  pull request's number.
- One job, `coauthor-trailers`, with exactly the keys `name`, `runs-on`, `timeout-minutes` and `steps`
  (so no `if:`, `permissions:`, `environment:`, `container:`, `services:`, `defaults:`, `env:`,
  `continue-on-error:` or `strategy:`), and `name: Co-author trailers (base)`.
- Exactly the four steps above, in that order, each with a closed set of keys and its `with:`, `env:` and
  `run:` held line by line, as PR #703's pin holds them today. The checkout has no `ref:` and no
  `repository:`. No step uses another action.
- No `${{` anywhere in the job's whole block outside a step's `env:`: not in a step's `run:` or `with:`,
  and not in a job-level key such as `if:`, `runs-on:`, `timeout-minutes:` or a job-level `env:`
  (amended 2026-11-28, issue #723; PR #711's pin read the step lines only). The string `secrets.`
  appears nowhere in the file.
- A `run:` is either one plain line or a `|` block. `>`, `|-`, `|+`, `>-` and `>+` are refused (issue
  #706, part 2, through `ciRunScript`).
- `ci.yml` has no `coauthor-trailers` job and `Lint`'s `needs:` is exactly `[lint-target, lint-shell]`
  (amended 2026-11-28, issue #723; during the transition the old job kept PR #703's pin and `Lint`
  needed it too).
- A name guard over every other workflow under `.github/workflows/`, reading each job's name as a check
  run reports it (quoted, plain, or the job's id when it holds no `name:`): no name holds `(base)`; no
  name holds a `${{ }}` expression unless an allow list pins it (today only
  `Lint ${{ matrix.goos }}/${{ matrix.goarch }} cgo=${{ matrix.cgo }}`); and no job holds the retired
  legacy name `Co-author trailers` or job id `coauthor-trailers`, `ci.yml`'s jobs included (amended
  2026-11-28, issue #723: the guard's exception for the old job is gone with it). A plain name's
  trailing comment is refused after a tab as after a space.

This pin still runs from the pull request's own checkout, under the `Test` job of `ci.yml`. It does not
close the gap: the trigger does. A pull request can loosen the pin, but it cannot change the run that
checks its own commits. The pin's job is to make sure that a change to the trusted workflow, once merged,
is reviewed against a precise description of it.

### Maintainer steps

The repository side is this pull request and its follow-up. The ruleset is the maintainer's:

1. **Before merging this pull request,** check its co-author trailers by hand with `main`'s copy of the
   check, since the only co-author check on it is the old job its own `ci.yml` defines. In a clean
   clone of the repository, with `<n>` this pull request's number. Fetching does not move the working
   tree, so the switch makes the `test/lint` that runs `origin/main`'s, never a stale local `main`'s
   (with local changes, run the same lines in `git worktree add --detach <tmp> origin/main` instead):

   ```bash
   git fetch origin main
   git fetch origin "pull/<n>/head"
   git switch --detach origin/main
   git rev-parse HEAD origin/main
   # PASS: the same SHA twice; FAIL: two SHAs (stop: the check below would run another test/lint)
   SHIFT2EU_COAUTHOR_RANGE=origin/main..FETCH_HEAD SHIFT2EU_COAUTHOR_CHECK=require \
     go test -count=1 -v -run '^TestBranchCoAuthorTrailers$' ./test/lint/
   # PASS: ok, and the log line "read <k> commits of origin/main..FETCH_HEAD" names the pull
   #       request's commit count (gh pr view <n> --json commits --jq '.commits | length')
   # FAIL: FAIL, or another count
   ```

   Then **merge it.** Before the merge, `main` has no `coauthor-trailers.yml`, so no pull request can
   report `Co-author trailers (base)`, and requiring it would block every pull request.
2. **Probe pull request, right after the merge, closed unmerged (the issue's "done when"):** a branch
   into `main` with one commit that changes `coauthor-trailers.yml`'s test step to `exit 0` and loosens
   the pin to match, and one commit whose message carries a `Co-Authored-By:` trailer with an address the
   allow list does not name (an `example.com` address). The probe comes before step 3 because the check
   reports whether or not it is required, and its first run makes the name `Co-author trailers (base)`
   one GitHub offers in the ruleset's check list; requiring a name that has never reported risks a typo
   that blocks every pull request. With `<probe head sha>` the probe's head commit (`filter=all` lists
   every run of the name, not only the latest, and `--paginate` every page):

   ```bash
   gh api --paginate 'repos/timothymamo/shift2eu/commits/<probe head sha>/check-runs?check_name=Co-author%20trailers%20(base)&filter=all' \
     --jq '.check_runs[] | [.id,.app.id,.status,.conclusion,.details_url] | @tsv'
   # PASS: at least one row  <id>  15368  completed  failure  ...  (and its log names the example.com
   #       trailer); no success row; every row's app 15368
   # for each row, <run id> from its details_url (the number after /runs/):
   gh api repos/timothymamo/shift2eu/actions/runs/<run id> \
     --jq '[.event,.path,.head_sha,[.pull_requests[].base.ref]]'
   # PASS: ["pull_request_target",".github/workflows/coauthor-trailers.yml","<probe head sha>",["main"]]
   # FAIL: the check passes, no row, another app id, another event, path, head SHA or base
   ```

   Run these before closing the probe: a run's `pull_requests` lists only the pull requests open with
   that head at query time, so once the probe is closed the last list is `[]`. Then close the probe
   unmerged. Between the merge and step 3 the check runs but is not required; only the maintainer
   merges, so nothing lands in that window unless the maintainer merges it.
3. **Require it.** Edit `Protect main` → Require status checks to pass → add **`Co-author trailers
   (base)`** with **GitHub Actions** as its expected source, and save. `Lint` stays required. Check it:

   ```bash
   id=$(gh api repos/timothymamo/shift2eu/rulesets --jq '.[] | select(.name=="Protect main") | .id')
   gh api "repos/timothymamo/shift2eu/rulesets/$id" \
     --jq '.rules[] | select(.type == "required_status_checks") | .parameters.required_status_checks[]
           | select(.context == "Co-author trailers (base)") | tojson'
   # PASS: {"context":"Co-author trailers (base)","integration_id":15368}
   # FAIL: no row, no integration_id, or another one
   ```

4. **Pull requests open at the merge** have no run of the new workflow and wait on the required check.
   Any new event starts one: a push, **Update branch**, a title edit (`edited`), or a close and reopen.
   Re-running their old jobs cannot help, because no run exists. Their `Co-author trailers` runs, from
   their own `ci.yml`, do not count: the new check has a different name.
5. **Merge the follow-up pull request** that removes the old job, only after step 3. It is the first pull
   request checked by `main`'s definition alone. (Done: steps 1 to 3 on 2026-11-28, with PR #711 and the
   probe PR #722; the follow-up is issue #723's pull request.)
6. **Before every merge from step 3 on,** check that the passing `Co-author trailers (base)` came from
   `main`'s workflow (the residual risk below: the required check matches the name and the app only).
   With `<head sha>` the pull request's head commit and `<n>` its number. The query lists every run of the name
   (`filter=all`; the default, `latest`, hides earlier ones) on every page (`--paginate`; a page holds
   30). `cancelled` rows are legitimate: `cancel-in-progress` cancels a run when a push or a title edit
   (`edited`) starts the next one.

   ```bash
   gh api --paginate 'repos/timothymamo/shift2eu/commits/<head sha>/check-runs?check_name=Co-author%20trailers%20(base)&filter=all' \
     --jq '.check_runs[] | [.id,.app.id,.status,.conclusion,.completed_at,.details_url] | @tsv'
   # PASS: at least one row is success, every row is success or cancelled, every row's app is 15368
   # FAIL: no success row, any other conclusion (failure, skipped, neutral, ...), a row not yet
   #       completed, or another app id
   # for each row, cancelled ones included, <run id> from its details_url (the number after /runs/):
   gh api repos/timothymamo/shift2eu/actions/runs/<run id> \
     --jq '[.event,.path,.head_sha,[.pull_requests[].base.ref]]'
   # PASS: ["pull_request_target",".github/workflows/coauthor-trailers.yml","<head sha>",["main"]]
   #       the last list is the pull requests open with that head now, not the base the run had: it
   #       is [] for a fork's pull request, or one that is closed or merged, or whose head branch is
   #       gone; the lists below are then the deciding lines
   # FAIL: another event, path or head SHA, or any base other than main
   gh pr list --search <head sha> --state all --json number,baseRefName
   gh api --paginate repos/timothymamo/shift2eu/commits/<head sha>/pulls --jq '.[]|[.number,.base.ref]|@tsv'
   # PASS: every pull request in both lists has base main. Only the first catches a closed,
   #       unmerged side pull request into another branch (the pulls endpoint finds open and merged
   #       ones only); the second is not search, which is eventually consistent and can miss a new
   #       pull request
   # FAIL: any base other than main in either list, for example a second pull request with that head
   #       into another branch
   gh api --paginate repos/timothymamo/shift2eu/issues/<n>/timeline --jq '.[]|select(.event=="base_ref_changed")'
   # PASS: no output (GitHub's own record: the pull request was never retargeted)
   # FAIL: any row. Then treat every check run on the head as suspect: a new head commit must be
   #       pushed and this step rerun on it
   ```

   The timeline line catches a retarget that the lines above cannot: retarget pull request `<n>` to a
   branch `x` whose `coauthor-trailers.yml` runs `exit 0` (its `edited` run posts a success row),
   retarget it back to `main`, then re-run `x`'s run so that `cancel-in-progress` cancels `main`'s run.
   The rows are then success and cancelled, every run's `pull_requests` lists `main` (the base now),
   and no other pull request has the head.

`docs/contributing/agents.md` changes with this pull request: the gap it names is closed, and it names
the residual risks below. `docs/dev-environment.md` gets the maintainer step, next to the **Claude Code
pin** step.

## Alternatives considered

1. **A ruleset "required workflow" pinned to `main`.** GitHub's "Require workflows to pass before
   merging" rule pins a workflow file at a ref, and the rule, not the pull request, decides which file
   runs. That would close the same gap and also the name-spoofing risk below. It is an
   organization-ruleset rule, and this repository is owned by a user account, so its repository
   rulesets cannot use it. Moving the repository into an organization is a larger change (the agents
   App's installation, the release App, every `timothymamo/shift2eu` that a script spells out, the
   gate check), out of scope here. If the repository ever moves into an organization, this ADR is worth
   revisiting.
2. **A `pull_request_target` job: chosen.** It needs no change to the owner or to `test/lint`. It reuses
   a pattern the repository already reviewed (`audit-paths.yml`, ADR 0040). Its one new ruleset entry
   is a required check of the kind the maintainer already sets (`Claude Code pin`, `Audit report paths`).
   Reading the commits through the REST API (`pulls/{n}/commits`), instead of fetching them as objects,
   was weighed too and lost: it would need a new input format for `test/lint`, a token with pull request
   read, and a second reading of messages and identities next to the git one the check already tests.
   The commits endpoint also stops at 250 commits.
3. **Keep the job as it is and document the gap.** That is today's state (PR #703's notes,
   `docs/contributing/agents.md`). It leaves a required check that the change under review can turn off,
   held only by the code-owner review that the check exists to support. Issue #706 was accepted to close
   it.

## Consequences

- A pull request into `main` that edits `coauthor-trailers.yml`, `ci.yml`, `test/lint` or the pin is
  still checked by `main`'s workflow, `main`'s `test/lint`, `main`'s Go and `main`'s allow list. Its own
  change counts from the next pull request after the merge.
- This pull request is checked only by the old `ci.yml` job that it keeps, whose steps come from the
  pull request itself (the gap this ADR closes), so the maintainer also runs `main`'s check by hand
  before merging it (Maintainer steps, step 1). The new workflow first runs on pull requests opened or
  updated after the merge.
- The co-author check reports as its own required check, `Co-author trailers (base)`. Until the
  follow-up merged it also still reported inside `Lint`, through the old job; since issue #723 it
  reports there no longer, and `Lint` gates only the lint matrix and `lint-shell`.
- A re-run of a `pull_request_target` run replays the original event, with the same `github.sha` and
  head, so it only repeats a flaky failure. A run against `main`'s newer tip needs a new event (a push,
  **Update branch**, a title edit, or a close and reopen).
- Each event runs `main`'s current tip, not the tip at the pull request's creation, so a check fixed on
  `main` applies to every open pull request at its next event.

Residual risks:

- **The required check pins the integration, not the workflow.** Any GitHub Actions workflow can report
  a job named `Co-author trailers (base)` from the same integration (15368). A pull request could add a
  `pull_request` job of that name to its own `ci.yml` that always passes. Its run would then sit beside
  the real one on the head commit, and GitHub would weigh the two check runs of one name in a way this
  repository does not control. The agents App can edit workflows, unlike the audit App of ADR 0040, so
  this is not ruled out by permissions. What bounds it: the change is a workflow edit in the pull
  request's own diff, which the maintainer's code-owner review sees; the pin refuses the name outside
  `coauthor-trailers.yml` (a pin the same pull request could loosen, so a review aid rather than a
  barrier); and the AI reviewer reads the diff. Only an organization-ruleset required workflow
  (alternative 1) removes it.
- **A passing run from another base branch (PLAUSIBLE, not verified).** The required check matches only
  the check run's name and the GitHub Actions app (15368), never the workflow file or the base ref.
  Someone with push rights (the agents App has contents and workflows write) can push a branch `x` whose
  own `coauthor-trailers.yml` keeps the job name `Co-author trailers (base)` but runs `exit 0`, then open
  a second pull request with the same head and base `x`, or retarget one to `x`. That run, from `x`'s
  copy, posts a passing check run of the same name on the same head SHA, and the pull request into `main`
  shows no workflow change in its own diff. Separately, a re-run of an old `main` run replays that run's
  `github.sha`, so it checks against that commit's older allow list and check (bounded: only a copy
  `main` once held). What bounds it
  today is the maintainer's pre-merge check (Maintainer steps, step 6), which looks at every check run of
  the name on the head commit, its event, the base of every pull request with that head, and the pull
  request's own retargets. A run's `pull_requests` cannot show the base a run had: it lists only the pull
  requests open with that head when it is read. So a retarget leaves no trace there: retarget the pull
  request to `x` (its `edited` run from `x`'s copy posts a success row), retarget it back to `main`, and
  re-run `x`'s run, which makes `cancel-in-progress` cancel `main`'s run. The rows are then success and
  cancelled, and every other line of step 6 passes; only the timeline's `base_ref_changed` events,
  GitHub's own record, show it, and then a new head commit must be pushed and checked again. The
  longer-term fixes are an organization required workflow (alternative 1), or a ruleset that refuses
  changes to `.github/workflows/**` on every branch but `main`.
- **A pull request into another branch runs that branch's copy** of the workflow, which could differ, or
  be missing. The required check matters only on `main`: when the pull request is retargeted to `main`,
  an `edited` run of `main`'s copy decides, and when that branch merges into `main`, the merging pull
  request's commits are checked again by `main`'s copy.
- **The repository's own `.git/config` is not isolated** (as in PR #703). Under this workflow it is the
  config that `actions/checkout` writes on the runner for the base commit, not one the pull request
  controls.
- **Trust in the runner and the actions.** The job trusts the GitHub-hosted runner image, the pinned
  `actions/checkout` and `actions/setup-go`, and the Go toolchain that `go.mod` names (checked against
  `sum.golang.org`), as every other job does.
