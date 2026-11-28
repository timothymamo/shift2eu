# PRD #730: The agent host's secrets in 1Password instead of the macOS Keychain

**Status:** Draft; decisions 1, 3, 4 and 6 to 9 answered by the maintainer, 2026-11-28; 2, 5 and 10 open
**Issue:** #730

## Problem

The agents run in a Lima VM on an unattended Mac host, driven over SSH (`docs/dev-environment.md`). Their
secrets start on that host, in the host clone's shell, from where `make vm-env` copies them into the VM's
`daemon.env`:

| Secret | Read by, on the host | Lifetime |
|---|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | `make vm-env` (default `VM_ENV`) | a year (`claude setup-token`) |
| `DEEPSEEK_API_KEY` | `make vm-env` (default `VM_ENV`) | until revoked |
| The agents App's private key, the file `SHIFT2EU_AGENTS_APP_KEY_FILE` names (or, in account mode, the role account's `agents-app.pem`) | `dev/agents-app-token.sh refresh`, run by a LaunchDaemon with nobody logged in (ADR 0034), or by `make agents-token*` | until rotated on the App's page |

Two more are coming, and the maintainer decided on 2026-11-28 that they go straight to 1Password, so this
PRD comes first:

| Secret | From | Reaches the VM through |
|---|---|---|
| The Honeycomb ingest key | PRD #646 | `make vm-telemetry-key` on the host clone, which reads one variable from the host clone's environment and writes it over standard input (PRD #646, decision 5, accepted) |
| `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` | #644 | a file on the VM only the maintainer writes (#644, Secrets); how it gets there is open |

Today the maintainer's host clone `.envrc` reads each secret from the login Keychain
(`security find-generic-password -s <repo>-<name> -w`) and direnv exports it into every shell opened in
the host clone. That costs three things:

- **A locked Keychain over SSH.** An SSH session has the login Keychain locked, so every lookup comes back
  empty and `make vm-env` refuses until `security unlock-keychain` is typed with the account password
  (Troubleshooting credentials). On a host that is only ever reached over SSH, that is every time.
- **Secrets in every host-clone shell.** direnv exports them for as long as a shell sits in the host
  clone, and every process started there inherits them, not just the one command that needs them.
- **One host, no record, no backup.** Adding a project means typing each secret into each host's Keychain
  again, nothing records which process read what, and the App's key has no copy anywhere but its file.

## Outcome

On the host, the secrets live in 1Password and are read only for the one command that needs them, through
`op run` with a read-only service account per repository and host. The host clone's `.envrc` holds no
secret and no secret reference, only names, paths and IDs. `make vm-env` and the coming
`make vm-telemetry-key` read their values from the environment exactly as today, so shift2eu stays
unaware of where they come from; the backend switch (`keychain` or `1password`) is a script in the
maintainer's private host-config repository. The App's key stays a file the LaunchDaemon reads. shift2eu's
host targets keep the service account's token out of every process they start (Lima, the VM, the
refresher), `make vm-env` refuses to copy it into the VM, the agents in the VM are refused `op` like any
other secret-reading command, and `docs/dev-environment.md` says how to set it up, rotate, survive an
outage and switch back to the Keychain.

## Scope

| | In scope | Out of scope |
|---|---|---|
| Source providers | None: this PRD changes the development environment, not the product | AWS, Azure, GCP |
| Target providers | None | OVHcloud, Scaleway, UpCloud |
| Taxonomy categories | None | All |
| Hosts | The Mac host that runs the VM and its token refresher | Debian or any other Linux host (the maintainer: unlikely, never planned); the admin machine's own credentials, beyond the one-time steps it runs |
| shift2eu changes | `docs/dev-environment.md` (Credentials, The host clone's `.envrc` lines, Troubleshooting credentials, the refresh and account mode where they name the key's backup); the Makefile's host targets (`VM_ENV` refusals, `VM_SHELL_ENV`, the refresh's environment, the refusal hint); `.claude/settings.json` and `.pi/deny-env-printing.json` deny entries for `op`; `test/devenv` tests; one ADR | The product (`cmd/`, `internal/`, `site/`), `infra/`, CI workflows |
| Stays on the host (the maintainer's private host-config repository, not shift2eu) | The switch script `agent-host-secret <repo> <name>` and its backend setting (`keychain` or `1password`); the per-repo env file of secret references; the service account's token file; the host bootstrap and its `new-project` step | Designing the bootstrap, `new-project` or the separate macOS setup user (this PRD only names what they need from 1Password) |
| Secrets | Those in the two tables above | Cloud credentials for integration tests (none exist yet; they will follow the same rule when they do); the maintainer's admin token and personal GitHub login |

shift2eu never calls `op` itself: nothing in the Makefile or `dev/` reads 1Password. Every target that
needs a secret keeps reading it from its environment, and the maintainer chooses what fills that
environment. That keeps the Keychain fallback a host-side switch with no shift2eu change, and keeps the
test suite free of any 1Password dependency.

## What 1Password's documentation says

Read for this PRD on 2026-11-28. `developer.1password.com/docs/...` now answers with a 301 to
`www.1password.dev/...`; the links below are the pages that answered. Only what a page states is used; what
no page states is listed under "Not documented" and checked on the host in milestone 2.

| Behaviour relied on | Link | What it states |
|---|---|---|
| Service accounts exist, read-only is possible, vault access is fixed | `https://www.1password.dev/service-accounts/get-started/` | A service account "is a token that gives scripts, CI/CD pipelines, and servers access to specific vaults and Environments"; vault permissions are `read_items`, `write_items` (needs `read_items`) and `share_items`; "Service account permissions, vault access, and Environment access are immutable", so a change means a new service account; the token is shown once ("The service account creation wizard only shows the service account token once"); it cannot be granted the built-in Personal, Private or Employee vault, nor the default Shared vault; the CLI needs version 2.18.0 or later and reads the token from `OP_SERVICE_ACCOUNT_TOKEN`; `op user get --me` shows `Type: SERVICE_ACCOUNT` when the token works; the CLI can create one with an `--expires-in` duration. It names no plan: an account owner or administrator grants the right to create them |
| How many | `https://www.1password.dev/service-accounts/` | "You can create up to 100 service accounts"; rotate a token "on a schedule or after a potential exposure"; subject to rate limits |
| Rate limits | `https://www.1password.dev/service-accounts/rate-limits/` | Per token, per hour, reads and writes counted apart: Business 10,000 reads and 1,000 writes; "1Password, Families, Teams" 1,000 reads and 100 writes. Per 1Password account, every service account together, reads and writes together, per 24 hours: Business 50,000, Teams 5,000, "1Password and Families" 1,000. The hour starts at the first request. "Some CLI commands make more than one request." Over a limit a request fails with 429 ("Too Many Requests", retry in up to 59 minutes; a daily limit, in about 24 hours). `op service-account ratelimit` shows the limits, used and remaining for the token in `OP_SERVICE_ACCOUNT_TOKEN`. No Enterprise row |
| Rotate and revoke | `https://www.1password.dev/service-accounts/manage-service-accounts/` | **Rotate Token** "generates a new token with the same permissions", and the old token's expiry is chosen then (examples: now, 1 hour, 3 days); **Revoke Token** "immediately removes its access to 1Password vaults"; **View Item Usage Report** shows the vaults and items it can access, which it accessed, when last, and the action |
| The token itself | `https://www.1password.dev/service-accounts/security/` | "It's up to you to save and protect the service account token"; it is Base64-encoded key material, not encrypted, so whoever holds it holds the access; it starts `ops_` so scanners find it; owners and administrators cannot view it after creation; a service account cannot create service accounts or manage users |
| `op run` | `https://www.1password.dev/cli/reference/commands/run/` | Secrets are "made available as environment variables only for the duration of the subprocess"; "Secrets printed to stdout or stderr are concealed by default" (`<concealed by 1Password>`), `--no-masking` turns that off; precedence: `--environment`, then `--env-file` (the last file wins), then the shell's variables, so a reference in the env file overrides a stale exported value |
| Secret references in the environment and `--env-file` | `https://www.1password.dev/cli/secrets-environment-variables/` | `op run --env-file=<file> -- <command>`; one `KEY=VALUE` per line, the value may be a secret reference; `#` comments and blank lines skipped; `$VAR` and `${VAR}` expanded unless single-quoted; references resolved after the file is parsed; and "You should assume that processes on your computer can access the environment of other processes run by the same user" |
| Reference syntax | `https://www.1password.dev/cli/secret-reference-syntax/` | `op://<vault>/<item>/[section/]<field>`, case-insensitive, letters, digits, `-`, `_`, `.` and whitespace (quoted); any other character needs the part's ID; a file attachment by its file name in place of the field |
| `op read` | `https://www.1password.dev/cli/reference/commands/read/` | `op read <reference>` prints to stdout; `--out-file` writes a file instead, `--file-mode` sets its mode (default 0600, ignored without `--out-file`); `-n` drops the newline; attachments by file name |
| Personal account with biometrics | `https://www.1password.dev/cli/app-integration/` | The CLI authenticates through the unlocked 1Password desktop app (Touch ID on a Mac), which must be running in the menu bar; it says nothing of SSH sessions or of no logged-in user |
| Install and verify on a Mac | `https://www.1password.dev/cli/get-started/`, `https://www.1password.dev/cli/verify/` | `brew install 1password-cli` (a cask maintained by Homebrew and 1Password) or the `.pkg`; macOS 11 or later. The installer's signer must read "Developer ID Installer: AgileBits Inc. (2BUA8C4S2C)", and the SHA-256 in its Details must match the page; the ZIP's `op` is checked with `gpg --verify op.sig op` against key `3FEF9748469ADBE15DA7CA80AC2D62742012EA22` |

**Not documented** in what could be read, so never assumed here and checked on the host in milestone 2
(host steps 7 to 10):

- whether `op run` starts the command at all when a reference cannot be resolved, and its exit status then;
- whether `op run` removes `OP_SERVICE_ACCOUNT_TOKEN` from the command's environment (assume it does not);
- how many requests one `op run` with N references makes;
- which plan the maintainer's account is: the rate-limit table implies service accounts on the individual
  and Families plans too, but the get-started page names no plan; the maintainer checks for
  **Developer > Service accounts** in their account (decision 1);
- any network or host restriction of a token: none is described, so a copied token works from anywhere
  until it is rotated or revoked.

## Design

### How a secret reaches a command

The host clone's `.envrc` keeps only what is not secret: `WORKSPACE_REPO`, the App's ID and installation
ID, the key file's path (or, in account mode, the account and port). The secrets are listed in a per-repo
env file of **secret references**, `op://<vault>/<item>/<field>` lines, kept in the host's config
directory beside the token file, outside the workspace and outside the host clone. A reference names where
a secret is, not its value, but the file stays out of shift2eu's git and out of anything the VM mounts all the same (whether
it goes into the maintainer's private host-config repository is decision 10).

The maintainer runs the commands that need secrets through the switch script, which for the `1password`
backend amounts to:

```text
OP_SERVICE_ACCOUNT_TOKEN="$(cat <the host's token file>)" op run --env-file <the repo's env file> -- make vm-env deck-restart
```

and for the `keychain` backend sets the same variables from `security find-generic-password` for that one
command, never in `.envrc`. The values exist in `op`'s and make's environment for the length of that
command (and in the processes it starts: see "The token stays out of what the command starts").

**Settled (decision 2 confirms the backend):** secrets in memory for one command, **not** exported by
direnv. direnv would run `op` at every `cd` into the host clone, spend the rate limit on shells that need
nothing, and put every secret into every process started there, which is the second cost in Problem.

The targets that need a secret, and so the only ones run through the switch: `make vm-env-check`,
`make vm-env`, `make vm-create` (it runs `vm-env`), later `make vm-telemetry-key` (PRD #646) and #644's
delivery of the Telegram pair (decision 8). `make deck-restart` needs none but follows `vm-env` in the same
command. No other host target is run under `op run`; the agents App targets in particular never are (next
section).

### The agents App's key and the LaunchDaemon

**Settled:** the key stays a local file, read by `dev/agents-app-token.sh` as today, in single-account and
in account mode. The refresher runs at boot with nobody logged in and restarts every 5 minutes at worst
(`KeepAlive`, `ThrottleInterval` 300, ADR 0034): an `op read` at each start would make the token's
renewal, which the agents need every hour, depend on 1Password's service and the network at boot, spend
reads for nothing, and need the service account's token in the refresher's reach. In account mode that
token would have to be readable by the role account `_shift2euagents` or by the maintainer's uid, and
either way the boundary account mode built (the maintainer's uid cannot read the key without `sudo`)
would leak through 1Password. So the LaunchDaemon reads nothing from 1Password, and nothing changes in
ADR 0034's job.

1Password holds a **backup** of the key, as a file attachment, used only to restore a lost key
(`op read --out-file <path> --file-mode 0600`). The backup must not sit in the vault the host's service
account reads: the maintainer's uid can read that token, and so would read the App's key through it, which
is exactly what account mode forbids. Where the backup lives is decision 5.

### The token stays out of what the command starts

`op run` hands its own environment to the command, so `OP_SERVICE_ACCOUNT_TOKEN` must be assumed to reach
make and everything make starts (Not documented, above). The token is worth more than any one secret it
reads: it reads every item of its vault, from any machine, until rotated. shift2eu's host targets
therefore:

- **refuse it in `VM_ENV`**: `make vm-env-check`, and so `vm-env` and `vm-create`, refuse any `OP_*` name
  in `VM_ENV`, as they refuse a GitHub token or a `SHIFT2EU_AGENTS_APP_*` name today, naming the variable
  and never its value; a merge drops a stale `OP_*` line from `daemon.env` and warns, as it does for a
  GitHub token;
- **start Lima without it**: `VM_SHELL_ENV`, which every `limactl` call of the Makefile goes through,
  also unsets `OP_SERVICE_ACCOUNT_TOKEN` and every other `OP_*` variable set in its environment (the
  CLI's other credentials, such as a signed-in session, are `OP_`-prefixed too; the milestone's coder
  checks the names against the CLI's reference rather than this PRD), so neither the VM nor Lima's long-running host agent (started by `limactl start`
  under `make vm-up`) holds it; `limactl start` and `limactl shell --start` also run without the default
  `VM_ENV` credentials, which only `make vm-env`'s write needs, so the host agent stops holding the
  Claude token and the DeepSeek key it inherits today;
- **start the refresh without it**: `make agents-token-refresh-start`'s background refresh, and the
  LaunchDaemon's job (which launchd starts with its plist's environment, not the caller's), never get an
  `OP_*` variable; `make agents-token-refresh-install` refuses to write one into the plist.

### An outage of 1Password

The VM keeps running: its `daemon.env`, the App token and the LaunchDaemon need nothing from 1Password.
Only a run of the switch fails. Whatever `op run` does with an unresolved reference (Not documented), the
result is closed: either `op run` exits before make starts, or make starts with the variable unset or
empty and `make vm-env-check` refuses with `vm-env: not set or empty: <NAME>`, leaving `daemon.env` byte
for byte as it was (today's behaviour). The refusal's hint, which today names only the locked Keychain,
also names 1Password: check `op run`'s own error, `op user get --me` for the token and
`op service-account ratelimit` for the limits, and that the VM's credentials are untouched. To work
through a long outage the maintainer switches the backend to `keychain` for as long as it lasts, as long
as the Keychain items are kept (decision 6).

### Rate limits

| Reader | How often | Reads |
|---|---|---|
| `make vm-env` (or `vm-create`) | a few times a week: rotation, a rebuild, a new variable | one `op run` with 2 references today, 3 after PRD #646 |
| `make vm-telemetry-key`, #644's Telegram delivery | once per VM rebuild or rotation | one `op run` with 1 or 2 references |
| The refresher (LaunchDaemon) | every 5 minutes at worst | **none**: it reads the key file |
| The maintainer's checks (`op user get --me`, `op service-account ratelimit`) | at setup and in an outage | one each |

The tightest limit that can apply, the individual and Families plans' 1,000 reads and writes a day for the
whole account and 1,000 reads an hour per token, is orders of magnitude above this even if one `op run`
made ten requests. How many it makes is measured in milestone 2 (host step 9) with
`op service-account ratelimit` before and after, and recorded in the docs. A script that looped `op run`
(a retry loop, a direnv hook) is what could reach the limit; the docs forbid both.

### No secret printed, logged or written

CLAUDE.md rule 3 holds for every agent and every change of this PRD:

- shift2eu's targets still print names only, never values (today's `vm-env` rules); `op run`'s masking is
  a second layer on the host: a `<concealed by 1Password>` in a target's output means a value was printed,
  and the docs say to treat it as a leak (rotate the secret);
- the agents in the VM are refused `op` like `env`: `.claude/settings.json` denies `Bash(op *)` and
  `Bash(*/op *)`, `.pi/deny-env-printing.json` lists `op`, and `OP_SERVICE_ACCOUNT_TOKEN` is already
  covered by the `$*TOKEN` expansion deny; the VM never has `op` installed (`dev/lima.yaml` pins no such
  tool), and a test keeps it so;
- no agent ever runs `op` or `security`, reads the env file, the token file or `.envrc`, or asks for a
  value: every 1Password step is the maintainer's, on the host;
- the tests hold it: `test/devenv` runs the targets with fake `limactl`, `launchctl` and refresher, with
  sentinel values in every secret variable and in `OP_SERVICE_ACCOUNT_TOKEN`, and asserts that no
  sentinel appears in any output, in the fake `limactl`'s recorded environment, in the refresher's
  environment, in a written `daemon.env` (for the token) or in a rendered plist.

### The Keychain fallback

Both backends stay documented. Switching is the host config's one setting; shift2eu needs no change for
it. The Keychain items stay until 1Password has served for the soak time of decision 6; until then a switch
back is one setting and a re-run of `make vm-env`. After the soak, either the Keychain items are deleted
and the fallback means restoring them from 1Password by hand, or they are kept and refreshed at each
rotation (decision 6).

### A new project, and the setup user

Per project, from 1Password, the host bootstrap's `new-project` step needs (this PRD names them; it does
not design the step):

1. a vault `agent-host-<repo>` holding that project's items (decision 3);
2. a read-only service account `<host>-<repo>` on that vault alone (decision 4), and its token in a mode
   0600 file in the host's per-repo config directory;
3. an env file of the project's secret references beside it;
4. the items themselves, created by the maintainer (a Claude token from `claude setup-token` for that
   project, a DeepSeek key of its own, and so on).

Creating a vault and a service account takes a person: a service account cannot create service
accounts, and its vault access is fixed when it is made. So `new-project` runs those two steps with the
maintainer's own sign-in (the desktop app with Touch ID on the admin machine, or the web), not with any
host token. The separate macOS setup user with its own Claude token needs its own item, in a vault no
project's service account reads, and no write-capable service account at all (decision 9).

## Acceptance criteria

Each criterion is observable from outside the code; `make check` runs the tests that pin the first eight.

- [ ] **AC1.** Given `make vm-env-check VM_ENV=OP_SERVICE_ACCOUNT_TOKEN` (or any `OP_*` name, alone or
  beside valid ones), it exits non-zero before any `limactl` call, names the variable and not its value,
  and `make vm-env` with the same `VM_ENV` leaves `daemon.env` byte for byte as it was.
- [ ] **AC2.** Given a VM `daemon.env` holding an `OP_SERVICE_ACCOUNT_TOKEN=` line, a merging
  `make vm-env VM_ENV=...` drops it and warns on stderr, naming the variable and never its value.
- [ ] **AC3.** Given `OP_SERVICE_ACCOUNT_TOKEN` and other `OP_*` variables set in make's environment, every `limactl` call of every host target (fake `limactl` recording its environment) sees
  none of them.
- [ ] **AC4.** Given the default `VM_ENV` credentials set, `make vm-up`'s `limactl start` and
  `limactl shell --start` see neither of them; `make vm-env`'s write still delivers both.
- [ ] **AC5.** Given `OP_SERVICE_ACCOUNT_TOKEN` set, the refresh `make agents-token-refresh-start` starts
  has no `OP_*` variable, and `make agents-token-refresh-install` writes none into the plist it renders.
- [ ] **AC6.** Given an unset or empty default credential, `make vm-env`'s refusal hint names both the
  Keychain and 1Password causes (`op run`'s error, `op user get --me`, `op service-account ratelimit`) and
  says the VM's credentials are unchanged.
- [ ] **AC7.** `.claude/settings.json` denies `op` commands and `.pi/deny-env-printing.json` lists `op`;
  the existing deny-rule tests cover the new entries, and `dev/lima.yaml` installs no `op`.
- [ ] **AC8.** With sentinel values in every secret variable and the token, no host target's output,
  the fake `limactl`'s recorded environment (but for the `VM_ENV` write's standard input), the
  refresher's environment, a rendered plist, or (for the token's sentinel) a `daemon.env` written by a
  replacing or a merging `make vm-env` contains a sentinel.
- [ ] **AC9.** `docs/dev-environment.md` documents the `1password` and `keychain` backends, the `.envrc`
  with no secret lines, the env file of references, the one-command form, the targets that need it and
  those that never run under it, the token file, rotation, an outage, the rate limits with the figure
  measured on the host, switching back, and what a new project needs; every 1Password behaviour it relies
  on links the page above that states it.
- [ ] **AC10.** On the host (milestone 2, the maintainer's steps), `make vm-env deck-restart` run through
  the `1password` backend writes both credentials, the agents' `claude auth` and `pi auth check` pass in
  `make vm-init`, and with an unreachable 1Password (network off, or a revoked test token) the same
  command refuses and the VM keeps working.

## Milestones

Each milestone is one PR with its tests, and each gets the security audit (skill `security-audit`) before
it is ready. The maintainer's host steps are listed per milestone; agents never run them.

1. [ ] **M1: the token stays out of the VM, Lima and the refresher.** AC1 to AC8. The `OP_*` refusal and
   merge drop in `vm-env-check` and `vm-env`; `VM_SHELL_ENV` unsets the `OP_*` variables; `limactl start`
   and `limactl shell --start` without the `VM_ENV` credentials; the refresh and the plist without
   `OP_*`; the refusal hint; the deny entries in both agents; the `test/devenv` tests; one ADR (skill
   `adr`) recording the decisions of this PRD: secrets for one command through a host-side switch,
   shift2eu backend-neutral, the key a file with a backup outside the host's vault, the token kept out of
   everything the command starts. Behaviour change and security-sensitive: an orchestration (tester first).
   **Host steps:** none. The maintainer may answer the decisions and install nothing yet.
2. [ ] **M2: the docs, and the switch on the host.** AC9 and AC10. `docs/dev-environment.md`: a section
   *Secrets from 1Password* under Credentials, the host clone's `.envrc` lines without secrets,
   Troubleshooting credentials for 1Password, the key's backup in The agents App and The key under a
   separate account, the figure host step 9 measures. Docs only; a single unit, after the maintainer's
   host steps report back. **Host steps** (the maintainer, on the host unless named):
   1. Confirm the plan and that **Developer > Service accounts** exists in the account (decision 1).
   2. Install the CLI (`brew install 1password-cli`, or the `.pkg` with its signer and SHA-256 checked as
      the verify page says); `op --version` is 2.18.0 or later.
   3. Create the vault `agent-host-shift2eu` (decision 3) and, in it, the items `claude` and `deepseek`
      from the current values; put the App key's backup where decision 5 says.
   4. Create the read-only service account `<host>-shift2eu` on that vault alone (decision 4), with the
      expiry of decision 7; save its token in 1Password at once (it is shown once) and in a mode 0600 file
      in the host's per-repo config directory, outside the workspace and the host clone.
   5. Write the repo's env file of references (`CLAUDE_CODE_OAUTH_TOKEN=op://...`,
      `DEEPSEEK_API_KEY=op://...`) beside it, and add the `1password` backend to the switch script in the
      host-config repo.
   6. Remove the secret lines from the host clone's `.envrc`, `direnv allow`, and check that a new shell
      in the host clone has neither variable.
   7. `op user get --me` with the token: `Type: SERVICE_ACCOUNT`.
   8. Run `make vm-env-check` and then `make vm-env deck-restart` through the switch; `make vm-init`'s
      `claude auth` and `pi` steps pass.
   9. `op service-account ratelimit` before and after one `make vm-env`: note the reads used.
   10. With a reference to an item that does not exist, run `make vm-env-check` through the switch: note
       whether `op run` stops before make and its exit status, and that the VM's `daemon.env` is unchanged.
       Check that the command's environment holds the token or not (`op run ... -- sh -c 'test -n "${OP_SERVICE_ACCOUNT_TOKEN-}" && echo present || echo absent'`), and that no output shows `<concealed by 1Password>`.
   11. Report 9 and 10 to the dispatcher in words (never a value), so M2's docs state them.
3. [ ] **M3: the coming secrets, and the end of the soak.** The Honeycomb key's and the Telegram pair's
   rows in the docs' table and env file (their PRD and issue then read them from the environment like
   `make vm-env`); the Keychain fallback as decision 6 settles it; what a new project needs, as the
   section above. Docs only, a single unit, after the soak time of decision 6. **Host steps:** add the
   items `honeycomb-ingest` and `telegram` to the vault when #646 and #644 reach their key step; after
   the soak, delete or keep the Keychain items as decided; revoke nothing that still serves.

PRD #646 and #644 then start with this PRD's M1 merged, and their docs point at *Secrets from 1Password*
for where their keys live on the host.

## Decisions for the maintainer

Each is the maintainer's alone; the recommendation is this PRD's.

1. **Your 1Password plan and account type.** (a) 1Password (individual), (b) Families, (c) Teams,
   (d) Business. It sets the limits above and whether service accounts are offered at all.
   *Recommendation:* keep the plan you have if **Developer > Service accounts** is there: the tightest
   limits (1,000 a day for the account) are far above this use. Without it, Teams is the smallest plan
   the docs name with 5,000 a day. *Answered by the maintainer, 2026-11-28: (b) Families*, which offers
   service accounts. Its limits (rate-limits page): 1,000 reads an hour per token, and 1,000 reads and
   writes together per 24 hours for the whole account, every service account of every repo included.
2. **Service accounts, or your personal account with biometrics.** (a) a read-only service account per
   repo and host, its token in a file on the host; (b) your own account through the desktop app with Touch
   ID; (c) both: service accounts on the host, your own account on the admin machine for setup and
   restores. *Recommendation:* (c). (b) alone needs the desktop app unlocked in a logged-in session and a
   touch per command, which an SSH-only unattended host does not have (the app-integration page describes
   only the desktop app, nothing for SSH); creating vaults and service accounts needs a person anyway.
3. **Vault layout.** (a) one vault per repo, `agent-host-<repo>`; (b) one vault per host for every repo.
   *Recommendation:* (a): service account vault access is immutable, so per-repo vaults let each project's
   token read only its own secrets, and a leaked token costs one project. *Accepted by the maintainer,
   2026-11-28: (a).*
4. **One service account per repo and host, or per host.** (a) `<host>-<repo>`, read-only on its one
   vault; (b) one per host on every repo vault. *Recommendation:* (a), your plan: the same blast-radius
   reason, well inside the 100 service accounts allowed; a second host gets its own, revoked on its own.
   *Accepted by the maintainer, 2026-11-28: (a).*
5. **Where the App key's backup lives.** (a) in the repo's host vault as `agents-app-key`, your starting
   plan; (b) in a vault only your personal account reads, never granted to a host's service account; (c)
   no backup: a lost key is replaced by generating a new one on the App's page. *Recommendation:* (b).
   (a) lets any code running as your uid on the host read the key through the token file, which undoes
   account mode; (c) is safe too, since a new key costs one runbook, but (b) keeps your restore path.
6. **What stays in the Keychain, and for how long.** (a) nothing after a two-week soak: delete the items;
   (b) keep them as the fallback, refreshed at every rotation; (c) keep only the service account's token in
   the Keychain instead of a file. *Recommendation:* (a), with the soak starting at M2's host steps; the
   fallback is then "restore the items from 1Password with your own account". (c) brings back the locked
   Keychain over SSH; (b) means rotating every secret twice. *Accepted by the maintainer, 2026-11-28: (a).*
7. **The service account token's expiry and rotation.** (a) no expiry, rotated with the Claude token once
   a year and at once on any suspicion; (b) an expiry (the CLI's `--expires-in`) such as 90 days.
   *Recommendation:* (a), with the yearly rotation in the same calendar reminder as `claude setup-token`,
   and the item usage report read then. A run of `make vm-env` is always by hand, so an expired token
   shows as a refusal at the worst moment (a rebuild) without adding protection a rotation does not.
   *Accepted by the maintainer, 2026-11-28: (a).*
8. **How #644's Telegram pair reaches the VM.** (a) a host make target like PRD #646's
   `make vm-telemetry-key`, reading both from the environment and writing the sender's own file over
   standard input; (b) typed by hand in the VM. *Recommendation:* (a): it survives a rebuild, reuses the
   same one-command switch and never touches `daemon.env`. #644 settles the details. *Accepted by the
   maintainer, 2026-11-28: (a).*
9. **The setup user's Claude token, and who may create vaults.** (a) its token in a vault only your own
   account reads, read by you when the bootstrap needs it; (b) a service account for the setup user that
   may create vaults. *Recommendation:* (a): a service account cannot create service accounts, so the
   bootstrap needs you for that step anyway, and a write-capable token on the host would be the most
   valuable thing on it. *Accepted by the maintainer, 2026-11-28: (a).*
10. **The env file of references: in the host-config repo or only on the host.** (a) committed to your
    private host-config repo (it holds names and `op://` paths, no value); (b) only on the host.
    *Recommendation:* (a): it is the list `new-project` writes and a rebuilt host needs, and it holds no
    secret; keep it private, since vault and item names describe your setup.

## Decisions and open questions

Settled by this PRD, with the reason, unless a decision above overturns them:

- Secrets in memory for one command through a host-side switch, not exported by direnv (Design, How a
  secret reaches a command).
- shift2eu never calls `op`; it reads its environment as today and stays backend-neutral (Scope).
- The App key stays a file; the LaunchDaemon reads nothing from 1Password (Design, The agents App's key).
- The service account's token never reaches the VM, Lima's host agent or the refresher; `VM_ENV` refuses
  `OP_*` (Design, The token stays out).
- An outage fails closed at `make vm-env-check` and leaves the VM running (Design, An outage).

Open until milestone 2's host steps: what `op run` does with an unresolved reference, whether it passes the
token on, and how many requests one run makes (What 1Password's documentation says, Not documented).
