# 0029. Agents open pull requests as a GitHub App, with one-hour tokens minted on the Mac

- Status: Accepted
- Date: 2026-11-14

## Context

The agents in the Lima VM push branches, open pull requests and file issues with the maintainer's own
fine-grained token (`GITHUB_TOKEN`, `make vm-env`). Every agent pull request is therefore the
maintainer's, and GitHub never lets an author approve their own pull request, so `Protect main` cannot
require an approval: anything holding a token with Contents and Pull requests write can open a pull
request and land it once the checks pass (PRD #161, Problem).

PRD #161 settles the direction (Decisions, 2026-11-14): the agents act as a GitHub App of the
maintainer's, the **agents App**, installed on this repository only; its private key stays on the Mac
(option B); its inputs live in the maintainer's direnv `.envrc` above the workspace, beside the other
credentials; an AI reviewer, a second App, comments or requests changes but never approves; branch
commits are not signed. Milestone 1, this ADR, is the token plumbing and the bot's git identity. It merges
under the old token and enforces nothing: the switch is the maintainer's runbook (PRD #161).

The forces: the VM must never hold a credential that outlives an hour; tokens must reach running agents
without restarting the deck; a stopped refresh must be obvious, not a generic 401; nothing may print or
log a token, a JWT or the key; and it must run on a stock Mac (POSIX `sh`, `openssl`, `curl`, `date`,
`sed`) and be testable on Linux CI with invented keys.

## Decision

The agents authenticate to GitHub with **installation tokens of the agents App, minted on the Mac and
written into the VM over standard input; a `gh` wrapper in the VM reads the current token on every call**.
No step of this design copies the key into the VM. That protects the key only as far as the host-side code
the maintainer runs from the workspace (the `Makefile`, `dev/agents-app-token.sh`) is the reviewed code:
the agents can edit the workspace, and anything `make agents-token` or `make deck` runs on the Mac can read
every file the maintainer can (see Consequences).

- **Inputs, on the Mac only.** `SHIFT2EU_AGENTS_APP_ID`, `SHIFT2EU_AGENTS_APP_INSTALLATION_ID` and
  `SHIFT2EU_AGENTS_APP_KEY_FILE` (the path of the PEM key, outside the workspace, a regular file, not a
  symlink, of mode 0600, which the script checks) come from the `.envrc` above the workspace. `make vm-env`
  refuses any `SHIFT2EU_AGENTS_APP_*` name in `VM_ENV`, so neither the key nor its path can be copied into
  the VM.
- **Minting** (`dev/agents-app-token.sh mint`, `make agents-token`). An RS256 JWT (`iat` now − 60 s,
  `exp` `iat` + 600 s, `iss` the App ID) signed with `openssl dgst -sha256 -sign`, then
  `POST /app/installations/<id>/access_tokens`. The JWT reaches `curl` only through `--config -` on its
  standard input, after `-q`, so no `~/.curlrc` can log or redirect it, and the response is parsed with
  `sed` (a stock Mac has no `jq`). The API URL is `https://api.github.com`; the tests' hook,
  `SHIFT2EU_AGENTS_GITHUB_API_URL` (a name of its own, as other tools set `GITHUB_API_URL`), is refused
  when it holds an `@`, a quote, a backslash or whitespace (each could send the JWT to another host or add
  a line to curl's config), or is plain http to anything but `127.0.0.1` or `localhost`. The slug comes from
  `GET /app`, never hard-coded, and the bot user's id from `GET /users/<slug>[bot]` (with the JWT, and
  without one if GitHub refuses a JWT there: the bot user is public).
- **In the VM**, written by a fixed `sh -c` script that `limactl shell` runs, its content on standard
  input, atomically (a temporary file beside each, then `mv`) under umask 077:
  `~/.config/shift2eu/agents-app-token` (the token, then its expiry in Unix seconds: the earlier of mint
  time + 3600 s − 300 s and `expires_at` − 300 s, so a VM clock a few minutes behind the Mac's still
  stops using a token before GitHub does) and `~/.config/shift2eu/agents-app-identity` (`<slug>[bot]`,
  then `<bot user id>+<slug>[bot]@users.noreply.github.com`), both mode 0600. The script refuses, before
  it writes anything, a `~/.config/shift2eu` that is a symlink and a token or identity file that is a
  symlink or not a regular file, so an agent cannot point the token into the workspace mount. It prints
  the VM's clock, and `make agents-token` warns when it is more than 60 s off the Mac's; and it checks for
  the real gh at `/usr/local/libexec/shift2eu/gh`, warning that a VM without it predates the gh wrapper
  ("make vm-gh-wrapper (or make vm-delete vm-create)"), as its gh would ignore the token.
- **The gh wrapper** (`dev/gh-app-token.sh`). `dev/lima.yaml` installs the real gh as
  `/usr/local/libexec/shift2eu/gh`, off the `PATH`, and a root-owned copy of the wrapper as
  `/usr/local/bin/gh`, outside the workspace mount: an agent's edit of `dev/gh-app-token.sh` never changes
  the gh in use, and the template carries the copy, as a VM keeps the template it was created with. That
  is plumbing, not a boundary: the VM user has passwordless `sudo` (ADR 0017) and can run the real gh
  directly. The installed copy has the real gh's path pinned by one `sed` script on its `real=` line, so
  it reads no `SHIFT2EU_REAL_GH` (the tests' hook), and a variable leaking into a long-lived environment
  cannot hand the token to another binary. `make vm-gh-wrapper` (`dev/vm-gh-wrapper.sh`) gives a VM
  created before this change the same layout, over `limactl shell`'s standard input, without recreating
  it. The git credential helper is `!/usr/local/bin/gh auth git-credential`, by absolute path
  (`gh auth setup-git` would name the real binary). On every call the wrapper reads the token file:
  absent, it runs the real gh unchanged; well formed, the VM user's own, mode 0600 and not expired, it runs
  the real gh with `GH_TOKEN` set to the token and `GITHUB_TOKEN` unset, arguments and standard input
  passed through; anything else, it fails without running gh and asks for the maintainer to run
  `make agents-token-refresh` on the Mac, never printing the file's content, and never suggesting to
  delete the file: an agent that followed that advice would silently push as the maintainer again. A
  running agent's next `gh` or `git push` therefore uses the newest token, with no restart.
- **Fallback until milestone 3.** With no token file the wrapper falls back to `GITHUB_TOKEN`, so this
  change merges and is switched on without a gap; deleting the token file in the VM is the rollback
  (`docs/dev-environment.md`). The fallback is the agents' to take: until milestone 3 an agent can delete
  the token file and act as the maintainer's `GITHUB_TOKEN` again. Accepted for milestone 1, which
  enforces nothing; milestone 3 removes `GITHUB_TOKEN` from `VM_ENV` and the fallback.
- **Git author.** `make vm-init` runs `dev/vm-git-identity.sh` with the maintainer's identity as its
  fallback: it sets `user.name` and `user.email` from the identity file when it is there (replacing any
  other identity, and refusing a malformed file before changing anything), otherwise the fallback where
  the VM has none, and gives way to the fallback again when the file is removed and the VM still has a
  bot identity (the rollback).
- **The bot's email** is numbered with the bot **user's** id, not the App ID. GitHub links a commit to an
  account by its email; for an App that account is the bot user `<slug>[bot]`, whose id differs from the
  App's, and its noreply address is `<user id>+<login>@users.noreply.github.com` like any account's.
  An address with the App ID would link commits to no account, or to the wrong one.

### The open questions (1) of PRD #161

**a) How the refresh runs on the Mac: a wall-clock loop `make deck` starts, not launchd.**
`dev/agents-app-token.sh refresh` (`make agents-token-refresh` in the foreground) mints at once, then every
`SHIFT2EU_AGENTS_TOKEN_CHECK_SECONDS` (60) compares the wall clock (`date +%s`) with the expiry of the last
token it wrote and mints again when fewer than `SHIFT2EU_AGENTS_TOKEN_MARGIN_SECONDS` (1200) remain. It
survives the Mac sleeping because it never sleeps for the token's lifetime: after a wake the next check, at
most a minute later, sees how little is left. A wake usually comes before the network does, and a long
sleep leaves no valid token at all, so the loop tells failures apart. A **transient** one (no answer from
GitHub, HTTP 408, 429 or 5xx, `limactl` failing or the VM not running) never stops it, with or without a
valid token left: it logs one line and tries again, at every check at first, then doubling the wait up to
five checks. Only a **permanent** one stops it, with exit 1: GitHub refusing the mint with 401 (the JWT),
403 or 404 (the App or its installation) or another 4xx, a 4xx from the slug or bot-user lookup, a key
that cannot sign, a VM refusing to write through a symlink, or bad inputs at the start. One refresher
runs per VM instance: it holds the lock
`${XDG_STATE_HOME:-~/.local/state}/shift2eu/agents-token-refresh-<instance>.lock` (a directory, with its
pid inside, removed by its exit trap). A second refresher of the same instance, by hand or from
`make deck`, says that another refresher for that VM is running and exits **0**: the VM's token is being
refreshed, which is what was asked. A lock whose pid is no longer a running refresh (a refresher killed
without its trap) is taken over. `make deck` starts it in the background with `nohup` when the three App
variables are set and its own refresher of that instance is not running, its pid and log in
`agents-token-refresh-<instance>.{pid,log}` in the same directory (mode 0600), and says where the log is;
without the variables `deck` behaves as before. It says when it stopped, four ways: a line on its standard
error (so in the log) with `agents App token refresh stopped`, the time, until when the VM's token is valid
and `make agents-token-refresh`, on a signal and on a permanent failure; `make deck` warns when the pid
file is stale and shows the log's last lines before starting a new one; and once the token has expired the
wrapper's error asks for `make agents-token-refresh`.
launchd lost: a launch agent gets no direnv environment, so the key's path would have to be written into
a plist; it is a second install step outside `make`; and nothing of it can be tested on Linux CI.

**b) `gh auth status` and `gh api user` with an installation token.** An installation token cannot call
`GET /user` (it is not a user), so `gh api user` fails with it. Whether `gh auth status` succeeds with an
installation token in `GH_TOKEN` depends on how the pinned gh validates a token and can only be known with
the real App: `make vm-init` no longer depends on it. Its `gh` step reads the repository instead,
`gh api repos/<owner>/<repo> --jq .full_name` (Metadata read, which both credentials have), with
`<owner>/<repo>` from the main checkout's `origin`, and says which credential it used. No skill calls
`gh api user`. Checked at runbook step 3: `gh api repos/timothymamo/shift2eu --jq .full_name` and
`gh auth status` in the VM with the token file present, and whether the second fails.

**c) Whether dot-agent-deck reads `GITHUB_TOKEN`.** Read-only, in `vfarcic/dot-agent-deck` on
2026-11-14 (`gh search code` and its `src/`): its Rust code never reads `GITHUB_TOKEN` or `GH_TOKEN`
itself; it runs `gh`, looked up on the `PATH` (`Command::new("gh")`): `gh pr list` for a unit's pull
request and CI time (`src/pull_request.rs`), `gh issue` and `gh api` for issue dispatch
(`src/issue_dispatch.rs`), and for a self-upgrade `gh auth status` then `gh attestation verify`
(`src/self_upgrade/verify.rs`), where a failing `gh auth status` only skips the provenance check. Its
release lookups call `api.github.com` unauthenticated (`src/repo_identity.rs`). `GITHUB_TOKEN` and
`GH_TOKEN` appear only in its own workflows and tests. Fairly sure, not certain: a code search can miss a
file. So the deck needs nothing of its own: its `gh` is the wrapper (systemd's default `PATH` holds
`/usr/local/bin`) and gets the App token. Checked at runbook step 3: the deck shows a unit's pull request
with the App token, and `dot-agent-deck` self-upgrade, if ever used, may report the provenance check as
skipped.

**d) Permissions the `release-tag` skill needs beyond the App's list.** The App has Contents, Pull requests
and Issues read and write, Workflows write, Actions, Checks and Commit statuses read, and Metadata read.
From GitHub's "Permissions required for GitHub Apps" (REST):

| Call in the skill | Permission | With the App's list |
|---|---|---|
| `GET repos/{o}/{r}/rulesets`, `.../rulesets/<id>` | Metadata read | Readable, **but** `bypass_actors` is returned only to a caller with write access to the ruleset (Administration write) |
| `GET .../environments/<env>`, `.../deployment-branch-policies` | Actions read | Covered |
| `GET .../actions/secrets` | Secrets read | Not covered: stays a maintainer check (the skill already says so on a 403) |
| `GET .../actions/variables` | Variables read | Not covered: stays a maintainer check, as above |
| `gh release view/download`, `GET .../releases/latest` | Contents read | Covered |
| `GET .../check-runs/<id>/annotations` | Checks read | Covered |
| `gh run list/view/watch/rerun` | Actions read (rerun: Actions write) | Read covered; `gh run rerun` needs **Actions write**, which the App does not get: the maintainer re-runs |
| `gh pr view/checks/diff/merge`, `gh pr list` | Pull requests, Contents | Covered |
| `gh attestation verify` (`GET .../attestations/<digest>`) | **Attestations read** | Not covered: add it |

So the maintainer adds **Attestations: read** (the release check verifies images and archives with
`gh attestation verify`; a narrow read). **Actions stays read**, as in the PRD's list. `gh run rerun`,
which the release-tag skill uses to recover a failed release run, needs Actions write, and for an
installation token Actions write grants far more than a re-run: cancelling any run (a required check's
included), deleting runs and their logs (the record of what an agent did) and artifacts, disabling and
enabling workflows (turning off a scan nobody requires), dispatching any `workflow_dispatch` workflow
added later, re-running with debug logging, and approving workflow runs from fork pull requests. None of
that is worth a convenience: a re-run stays a maintainer step, and milestone 2's release-tag change makes
the skill hand it to the maintainer (`gh run rerun <id>`) instead of running it. The rulesets' `bypass_actors` cannot be read
without Administration write, which the App must never have (it could change `Protect main`): the
release-tag skill's ruleset checks fall to the maintainer after the switch, and milestone 2 makes the
skill say so, as it already does for the Actions secrets. Secrets and Variables read are not added: they
would widen the App for a check that is the maintainer's anyway. Checked at runbook step 3: each call of
the table with the App token, and that `bypass_actors` is absent or empty for the App.

## Alternatives considered

- **A machine user** (a second GitHub account with its own token). Its pull requests could be approved by
  the maintainer, but it is a second account under GitHub's terms, with its own login, password and 2FA
  to keep, and its token is long-lived, rotated by hand. An App is scoped to this repository's installation and its tokens expire
  within the hour (PRD #161, Decisions).
- **The key in the VM, tokens minted there** (option A). Simpler, but every agent, and every model
  provider that sees a file, would hold a credential that never expires.
- **launchd** for the refresh: see a) above.
- **`gh auth login --with-token`** each refresh, or the token in `daemon.env`. gh's stored login and the
  daemon's environment are read once per process: a running agent would keep a dead token until the deck
  restarted, which stops every agent.
- **Signed branch commits.** An App has no account to register a signing key with; only commits made
  through GitHub's API are signed for it, which would mean rewriting how every agent commits. `main`
  stays signed, as every commit on it is a squash merge GitHub creates and signs, and no ruleset requires
  signed commits.
- **An AI reviewer that approves**, dot-agent-deck's own model (its reviewer App casts the required
  approval, with an admin bypass for the owner). It saves a click per pull request, but lets a change reach
  `main` with no human reading it, and lets a prompt in a pull request talk the reviewer into approving.
  The reviewer of PRD #161 milestone 4 only comments or requests changes, and the code-owner rule of
  milestone 2 makes sure an approval of any App could never count, even if it were tricked into one.

## Consequences

- The VM holds only a token that expires within the hour; a token leaked from the VM is worth at most an
  hour, and revoking the App's key stops every new one.
- **The key is only as safe as the host-side code the maintainer runs from the workspace.** `make deck`
  and `make agents-token` run the workspace's `Makefile` and `dev/agents-app-token.sh` on the Mac, as the
  maintainer, with `SHIFT2EU_AGENTS_APP_KEY_FILE` (and every other credential of the `.envrc`) in the
  environment. The workspace is mounted read-write into the VM, `.git` included, so an agent can change
  that code, or hide a change (a local `main` ref, `git update-index --assume-unchanged`), and the next
  run on the Mac can read the key and send it anywhere. No scoping of the environment helps: that code
  can read every file the maintainer can. The same is already true of every credential `make vm-env`
  copies (`GITHUB_TOKEN`, the Claude token), so this is not a new class of exposure, but the App key never
  expires. The mitigation is procedural: run the token targets, and `make deck` with the App's variables,
  only from a checkout whose `Makefile` and `dev/` match reviewed `main` (in the main checkout,
  `git fetch && git diff --stat origin/main -- Makefile dev/` prints nothing; `docs/dev-environment.md`
  gives the check). **Open follow-up, not in this change:** run the host targets from a pinned copy
  outside the mount (installed from a reviewed commit and checked by its SHA-256), with the App's variables
  exported only to that program.
- Every `gh` in the VM, the deck's included, goes through the wrapper. `/usr/local/bin/gh` is still the
  absolute path the release-tag skill calls; it is root-owned, as is the real gh behind it. Like
  everything else in the VM, an agent with the VM user's passwordless `sudo` could change either (ADR
  0017): the wrapper is plumbing, not a boundary.
- The wrapper's text exists twice: `dev/gh-app-token.sh`, which the tests run, and its copy in
  `dev/lima.yaml`, between the `GHWRAPPER` lines, which a VM installs. They must stay identical; the tests
  compare them byte for byte and pin the `sed` script that pins the installed copy's `real=` line.
- An existing VM gets the wrapper from `make vm-gh-wrapper`, or from a new template
  (`make vm-delete vm-create`); until then it keeps the real gh at `/usr/local/bin/gh` and `GITHUB_TOKEN`,
  which is the fallback anyway, and `make agents-token` warns about it.
- `make deck` on the Mac now starts a background process when the App variables are set; it is stopped
  with `kill "$(cat ~/.local/state/shift2eu/agents-token-refresh-shift2eu.pid)"` (the instance's name in
  the file name).
- The maintainer's App gets one permission beyond the PRD's list (Attestations read); `gh run rerun` and
  the rulesets' bypass checks become maintainer steps after the switch (milestone 2).
- Questions only the real App can answer are checks at the runbook's step 3: the pull request's author,
  the commit author, `gh auth status`, the reads of d), and the deck's own `gh` calls.
