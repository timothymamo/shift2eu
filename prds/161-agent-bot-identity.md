# PRD #161: Agents open pull requests as a GitHub App, an AI reviewer comments, and main requires the maintainer's approval

**Status:** Draft
**Issue:** #161

## Problem

`Protect main` (ruleset 26865773) requires a pull request and the required status checks, but **0
approvals**, and has no bypass actor. Anything holding a token with Contents and Pull requests write can
open a pull request and merge it once the checks pass: the agents' fine-grained token, and since #142 the
release-please App's token, which the third-party `release-please-action` holds. #157 stopped that token
from creating release tags, not from merging.

A required approval would close this: GitHub never lets the author of a pull request approve it, so a
token could open a pull request but not land it. It cannot be turned on today, because the agents push
and open pull requests **as the maintainer's own account** (`GITHUB_TOKEN`, `make vm-env`), and the
maintainer cannot approve their own pull requests: every agent pull request would be stuck.

The maintainer also reads every diff alone. The units' own reviewer and auditor run inside the same VM, from
the same branch, with the same task: nothing outside the unit looks at a pull request before the maintainer
does.

## Outcome

The agents push branches, open pull requests and file issues as a **GitHub App** of the maintainer's (the
**agents App**), installed on this repository only. The App's private key never enters the Lima VM: a job
on the Mac mints one-hour installation tokens from it and hands only those to the VM, where `gh` and `git`
read the current one on every call. Agent commits are authored by the App's bot identity.

A second App, the **reviewer App**, reviews every pull request from a GitHub Actions workflow outside the
VM, with `main`'s copy of the `review-change` and `security-audit` skills: it posts inline comments and a
summary, and **requests changes** when it finds a blocking problem, which blocks the merge until it is
resolved or the maintainer dismisses it. It **never approves**, and its approval would not count if it did.

`Protect main` then requires one approval **from a code owner** (`.github/CODEOWNERS`: the maintainer, for
every path), dismisses stale approvals on push and requires the last push to be approved by someone other
than its pusher, with no bypass: no App, bot or token can land a change on `main` alone, and the
maintainer's approval and merge are the single human gate. The reviewer narrows what the maintainer needs
to read; it never replaces them. Renovate's pull requests get no exception. The maintainer's own
`GITHUB_TOKEN` leaves the VM.

## Scope

| | In scope | Out of scope |
|---|---|---|
| Source providers | none (development environment only) | |
| Target providers | none | |
| Taxonomy categories | none | |
| Development environment | `Makefile` (`vm-env`, `vm-init`, a token target), `dev/lima.yaml`, `dev/vm-init.sh`, the host-side token job, the VM's `gh` and `git` credential path, the VM's git author | The deck's own code (dot-agent-deck upstream) |
| Repository settings | The documented `Protect main` change, `.github/CODEOWNERS`, the `ai-review` environment, and the checks that read them | Changing the release Apps, their rulesets or environments (ADR 0021) |
| CI | The AI review workflow and its prompt | An AI reviewer that approves or merges (dot-agent-deck's model, rejected below) |
| Skills and docs | `pr-create`, `release-tag`, `review-change` where they call `gh` or read the AI review; `docs/dev-environment.md`, `docs/contributing/agents.md`, CLAUDE.md, ADRs | |

## Acceptance criteria

### The agents App's credential (milestone 1)

- [x] The maintainer's `.envrc` **above the workspace** names the App ID, the installation ID and the
  **path** of the App's private key file, itself outside the workspace and readable only by the
  maintainer. No file in the workspace holds any of them, and `make vm-env` never copies the key or its
  path into the VM.
- [x] A make target on the Mac mints an installation token from the key (standard library or tools the
  Mac already has, no new third-party dependency without a reason in the commit message) and writes it
  into the VM over standard input, never on a command line, to a file readable only by the VM user.
- [x] While the VM runs, the token is refreshed before it expires, without restarting the deck: a running
  agent's next `gh` or `git push` uses the new token. Given a stopped refresh, the next call after expiry
  fails with an error that names the refresh target, not a generic 401.
- [x] No command prints a token, the key or its contents; nothing writes them to a log, the workspace or a
  test. Tests use invented keys generated in `t.TempDir()`.

### The agents' identity (milestone 1)

- [x] In the VM, `gh` and the git credential helper (`gh auth git-credential`) use the App token when its
  file is present and fall back to `GITHUB_TOKEN` otherwise, so milestone 1 can merge and be switched on
  without a gap. `make vm-init`'s credential check passes with either.
- [ ] A pull request opened by an agent after the switch shows the App as its author
  (`gh pr view --json author` gives `is_bot` `true` and the App's login), and its commits' author is the
  App's bot identity (`<bot user id>+<slug>[bot]@users.noreply.github.com`, the id of the bot user
  `<slug>[bot]`, not the App ID: ADR 0029), set by `make vm-init`. *Checked at runbook step 3.*
- [ ] Every `gh` call the skills make works with the App token: push, `pr create`, `pr view`, `pr checks`,
  `run list/view/watch`, `issue create/view`, and the `release-tag` skill's reads of rulesets, environments
  and releases. Any call that needs a permission the App lacks is listed with the permission, and the
  maintainer's App permissions in `docs/dev-environment.md` cover them. *The calls and their permissions
  are listed in ADR 0029 (d) and `docs/dev-environment.md` (Attestations read and Actions write added; the
  rulesets' bypass lists stay a maintainer check); that each works is checked at runbook step 3.*
- [ ] Branch commits are not signed; `main`'s commits stay GitHub-signed squash merges (`verified`
  `true`). No ruleset requires signed commits. *Nothing in milestone 1 signs a commit (ADR 0029); the
  repository's rulesets and `main`'s commits are checked at runbook step 3.*

### The gate (milestone 2)

- [ ] `.github/CODEOWNERS` names the maintainer as the owner of every path (`*`), including itself and
  `.github/`.
- [ ] A check (in the `release-tag` skill's step 2 and as a make target the maintainer can run) reads
  `Protect main` and fails unless: `required_approving_review_count` is at least 1,
  `require_code_owner_review`, `dismiss_stale_reviews_on_push` and `require_last_push_approval` are
  `true`, and `bypass_actors` is empty; and `.github/CODEOWNERS` on `main` names only the maintainer.
  Before the switch it reports the gaps without failing a release (a documented transition flag), after
  it, it fails closed.
- [ ] `pr-create` never approves a pull request and never merges one the maintainer has not approved;
  after the switch it reports "waiting for the maintainer's approval" instead of trying to merge.
- [ ] A Renovate pull request needs the maintainer's approval like any other; `.github/renovate.json5`
  keeps `automerge: false`.
- [ ] `docs/dev-environment.md` gives the maintainer's switch-over runbook (below) step by step, with a
  check after each step and how to roll back; CLAUDE.md and `docs/contributing/agents.md` say that the
  single human gate is the maintainer's approval and merge, that the AI reviewer's comments and change
  requests are advice the maintainer weighs, and that nobody pushes to an agent's branch by hand after the
  switch (a push by the maintainer could not be approved).

### After the switch (milestone 3)

- [ ] `GITHUB_TOKEN` is no longer in `VM_ENV`, and the fallback to it is removed: without the App token
  file, `gh` and `git push` in the VM fail with an error naming the refresh target.
- [ ] The docs no longer tell the maintainer to create a fine-grained personal token for the agents.

### The AI reviewer (milestone 4)

- [ ] A workflow reviews every pull request against `main` when it is opened, reopened, marked ready or
  pushed to (drafts are skipped; a new push cancels the review in progress). It runs in the `ai-review`
  environment, whose deployment branch policy is `main` only, so only `main`'s copy of the workflow ever
  holds the reviewer App's key and the model provider's key. It is triggered with `pull_request_target`
  and **never checks out or runs the pull request's code**: it reads the diff and changed files through
  the API as data. A pull request that edits the workflow, its prompt or the skills is reviewed by
  `main`'s versions of them.
- [ ] The workflow's job token has `contents: read` and `pull-requests: read` only; the review is posted
  with a token minted for the reviewer App (Pull requests write, Contents read, nothing else), whose ID and
  key are secrets of the `ai-review` environment only. The model provider's key is a secret of that
  environment only. No repository-level secret or variable holds either; the `release-tag` check lists
  them as it lists the release Apps' keys.
- [ ] The review uses `main`'s `review-change` and `security-audit` skills and CLAUDE.md. It posts one
  review: a summary, and inline comments on the lines it flags, each finding with a severity. A blocking
  finding (correctness, a broken architecture rule, a security issue) makes the review **request
  changes**; otherwise it is a **comment**. The workflow submits only the events `COMMENT` and
  `REQUEST_CHANGES`: the code that posts the review refuses any other event, and a test pins that.
- [ ] On a new push, the reviewer reviews the new head and dismisses its own earlier change request when it
  no longer stands; it never dismisses anyone else's review. The maintainer can always dismiss its change
  request.
- [ ] An approval from the reviewer App, the agents App or any account other than the maintainer does not
  satisfy `Protect main` (code-owner review), shown in the switch-over runbook's check.
- [ ] The reviewer treats the pull request's title, body, diff and comments as untrusted data, never as
  instructions; the prompt says so. Given a pull request whose diff contains instructions to the reviewer,
  it still reviews it and reports the attempt as a finding. Its tools are read-only (no shell, no network
  beyond the GitHub and model APIs, no write to the repository other than the review).
- [ ] The review states the head commit it reviewed. A failed or skipped review (the provider down, the diff
  over a documented size limit) is a visible check, not a silent pass, and never blocks the merge on its own:
  it is not a required check.
- [ ] Release-please's and Renovate's pull requests are reviewed like any other.
- [ ] `docs/dev-environment.md` gives the maintainer the steps to create the reviewer App, the
  `ai-review` environment and its secrets, and the cost and model used.

## Milestones

1. [x] **Agents App token plumbing and bot identity.** The host-side mint and refresh, the VM token file,
   the `gh` and git credential path with the `GITHUB_TOKEN` fallback, the bot git author,
   `make vm-env`/`vm-init` changes, tests, an ADR (identity, token model, why not a machine user, why the
   reviewer never approves), and `docs/dev-environment.md`. Merged under the old token; nothing is
   enforced yet.
2. [ ] **Gate checks, CODEOWNERS, skills and docs.** `.github/CODEOWNERS`, the `Protect main` check with
   its transition flag, `pr-create` and `release-tag` changes, CLAUDE.md, `docs/contributing/agents.md`,
   the switch-over runbook. Depends on 1.
3. [ ] **Remove the maintainer's token.** After the maintainer's runbook step 6: drop `GITHUB_TOKEN` from
   `VM_ENV` and the fallback, turn the check's transition flag off, update the docs. Opened by the App and
   approved by the maintainer: the first change through the new gate. Depends on 2 and the runbook.
4. [ ] **AI reviewer.** The review workflow, its prompt, the code that posts the review (comment or request
   changes only), its tests, an ADR (trigger, secrets, prompt injection, cost), `docs/dev-environment.md`
   and the release-tag check of its secrets. Depends on 2 (the code-owner gate, so no approval of the
   reviewer's could count); can run in parallel with 3.

## Switch-over runbook (maintainer)

Agreed with the maintainer on 2026-11-14. Each step is safe to stop at.

1. Create the agents App: installed on this repository only; permissions Contents, Pull requests and
   Issues read and write, Workflows write (agents edit `.github/workflows/`), Actions, Checks and Commit
   statuses read, and whatever else milestone 1 lists. Generate its private key; store the key and the IDs
   as the acceptance criteria say. Nothing changes yet.
2. Merge milestone 1 (still opened under the maintainer's token).
3. Run the token target, reload the VM's credentials and restart the deck **between tasks**. A test pull
   request proves the App is the author. Merge milestone 2 (the first PR the App opens).
4. Merge or close every open pull request **authored by the maintainer**: with no bypass actor, one left
   open after step 5 can never be approved. Release-please's and Renovate's pull requests are not affected.
5. Change `Protect main`: 1 required approval, require review from code owners, dismiss stale approvals on
   push, require approval of the most recent push. Run the gate check.
6. Remove `GITHUB_TOKEN` from the VM (milestone 3) and revoke that personal token.
7. Create the reviewer App (Pull requests write, Contents read, this repository only), the `ai-review`
   environment (deployment branches: `main` only) and its secrets, then merge milestone 4. Check that its
   first review is a comment or a change request, never an approval.

Roll back at any step before 5 by deleting the token file in the VM (the fallback takes over); after 5 by
setting the required approvals back to 0. The reviewer is rolled back by disabling its workflow: it is
never a required check.

## Decisions and open questions

**Decided (maintainer, 2026-11-14):**

- **A GitHub App, not a machine user.** No second account under GitHub's terms, no long-lived token, and
  permissions scoped to this repository.
- **The App's key stays on the Mac** (option B). The VM only ever holds a token that expires within the
  hour, so a compromised agent, or a model provider that sees a file, holds nothing that outlives it. Option
  A (the key in the VM, tokens minted there) was simpler but would give every agent a credential that
  never expires.
- **The credential lives in the maintainer's direnv `.envrc` above the workspace**, as `GITHUB_TOKEN`,
  the Claude token and the DeepSeek key do today (`docs/dev-environment.md`, Credentials).
- **Renovate pull requests are not exempt** from the approval (reconsidered and kept the same day). An
  exemption needs Renovate's App as a bypass actor, and a bypass skips every rule of its ruleset, the
  reviewer's change request included. A compromised GitHub Action merged to `main` would run in the
  `main`-only environments that hold the release Apps' keys (ADR 0021), and a compromised Go module or VM
  tool in the VM, beside the agents' tokens. Renovate opens a few grouped pull requests a night, so the
  approval costs little.
- **No commit signing on branches.** An App has no account to register a signing key with; only commits
  made through GitHub's API would be signed for it, which would mean rewriting how every agent commits.
  `main` stays signed: every commit on it is a squash merge GitHub creates and signs.
- **The hybrid: the bot opens, an AI reviewer comments or requests changes, the maintainer approves.**
  dot-agent-deck's model, where the maintainer's account opens pull requests and an AI reviewer App
  (`dot-agent-deck-reviewer[bot]`) casts the required approval, with an admin bypass for the owner, saves a
  click per pull request but lets a change reach `main` with no human reading it, and lets a prompt in a
  pull request talk the reviewer into approving it: the gap this PRD closes. Here the reviewer's job is to
  cut down what the maintainer has to read, and the code-owner rule makes sure its approval could never
  count, even if it were tricked into giving one.
- **The switch-over order** is the runbook above.

**Open (to settle in the milestone named, recorded in its ADR):**

- (1) How the refresh runs on the Mac: a launchd agent, or a loop the `deck` target starts. It must
  survive the Mac sleeping and say clearly when it stopped.
- (1) Whether `gh auth status` and `gh api user` (used by `make vm-init` and some skills) work with an
  installation token, and what replaces them where they do not.
- (1) Whether the deck itself (dot-agent-deck upstream) reads `GITHUB_TOKEN` for anything, and what it needs.
- (1) Which reads in the `release-tag` skill need permissions beyond those listed (rulesets with bypass
  actors, environments, deployment branch policies): if the App cannot be given them read-only, those
  checks stay maintainer checks and the skill says so, as it already does for the Actions secrets list.
- (4) Which model and runner the reviewer uses (a pinned action or a small script calling the provider's
  API), the diff size limit, and the expected monthly cost. A new third-party action needs a reason and a
  pinned SHA, like every other action.
- (4) Whether the reviewer's blocking threshold should start lower (comments only, no change requests) for
  the first weeks, so its false positives can be judged before it blocks anything.

Security-sensitive throughout: every milestone runs the `security-audit` skill. Refs #142, #157.
