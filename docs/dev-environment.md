# Development environment

The agents run inside a [Lima](https://lima-vm.io) virtual machine. The decision is recorded in
[ADR 0002](adr/0002-lima-vm-for-agent-isolation.md).

## Why Lima instead of devbox

dot-agent-deck's own repository uses devbox. Devbox gives reproducible tool versions, but it is a Nix
shell on the host: an agent started from it runs as you, with access to your whole home directory,
including `~/.ssh`, `~/.aws`, `~/.azure` and `~/.config/gcloud`. For a project whose job is to read
cloud estates, that is the wrong default.

Lima (Apache-2.0, a CNCF project, free, native on Apple Silicon through Apple's Virtualization.framework)
runs a real Linux VM. `make vm-up` starts it with `--mount-only <workspace>:w`, where the workspace is the
directory that holds this repository and nothing else, so the repository and the worktrees beside it are
the only host paths the VM can see ([ADR 0008](adr/0008-mount-a-workspace-directory-for-dispatcher-mode.md)).
dot-agent-deck publishes Linux arm64 and amd64 builds and is documented to run on Linux, so the whole deck
(daemon, TUI and agents) runs inside the VM.

## First-time setup

### On the Mac

1. Clone the repository into a directory of its own, the workspace. The VM mounts the workspace, and
   `make vm-up` refuses to start while it holds anything other than the repository and its dispatch
   worktrees ([Dispatcher mode](#dispatcher-mode)):

   ```bash
   mkdir shift2eu-ws && cd shift2eu-ws
   git clone https://github.com/timothymamo/shift2eu.git && cd shift2eu
   ```

2. Create the credentials ([Credentials](#credentials) explains each one). For Claude, generate a token
   for your subscription:

   ```bash
   claude setup-token        # opens the browser, then prints a token valid for a year
   ```

3. Export them from a direnv `.envrc` in a directory **above the workspace**, then `direnv allow`:

   ```bash
   export CLAUDE_CODE_OAUTH_TOKEN=...   # the token from step 1
   export DEEPSEEK_API_KEY=...          # the tester (pi)
   export GITHUB_TOKEN=...              # fine-grained, limited to this repository
   ```

4. Create the VM, copy the credentials into it and set it up:

   ```bash
   brew install lima          # the LIMA_VERSION in .github/workflows/vm-tools.yml (see below)
   make vm-create             # several minutes the first time
   ```

   `make vm-create` runs, in order:

   | Target | Does |
   |---|---|
   | `make vm-up` | Creates and provisions the VM, or starts it |
   | `make vm-env` | Copies the three variables into the VM ([Credentials](#credentials)) |
   | `make vm-claude-settings` | Claude Code user settings for the agents ([Agent permissions](#agent-permissions)) |
   | `make vm-init` | The once-only setup below, and the checks that the credentials work |

   Each can also be run on its own. The VM has 4 CPUs, 16 GiB of memory and a 40 GiB disk
   (`dev/lima.yaml`), enough for all six roles to run `make check` at once. Leave the Mac enough free
   memory to run it.

5. Open the deck: `make deck`.

### What `make vm-init` does

`make vm-init` (`dev/vm-init.sh`) runs in a login shell in the VM, because only a login shell loads the
credentials `make vm-env` wrote. It stops at the first step that fails, naming it, for example
`vm-init: pi: no DeepSeek key; set DEEPSEEK_API_KEY for make vm-env, ...`. Fix the cause and run it
again: it changes only what is not set up yet, so running it on a VM that is set up changes nothing.

| Step | What it does |
|---|---|
| `theme` | Sets `theme` in Claude Code's `~/.claude/settings.json` to `CLAUDE_THEME` (default `dark`; `make vm-init CLAUDE_THEME=light`) |
| `claude` | Marks Claude Code's onboarding done and trusts the workspace and the repository, in `~/.claude.json`, so the Claude Code roles start without first-run screens (see below) |
| `git` | Gives the VM your git identity, so the agents commit as you: `git config user.name` and `user.email` from the Mac, or `GIT_USER_NAME=` and `GIT_USER_EMAIL=`. An identity already set in the VM is kept. With the agents App's identity file in the VM ([The agents App](#the-agents-app)) the agents commit as the App's bot instead, which replaces any other identity; without the file, a bot identity left from earlier gives way to yours |
| `claude auth` | Checks that Claude Code uses the OAuth token (your subscription), not an API key |
| `pi` | `pi auth check --provider deepseek` |
| `gh` | `gh api repos/<owner>/<repo> --jq .full_name` for the main checkout's `origin`: gh can read the repository, with the agents App's token when its file is in the VM, otherwise `GITHUB_TOKEN`. Not `gh auth status`, which an installation token may fail (ADR 0029) |
| `deck` | `dot-agent-deck validate`: checks `.dot-agent-deck.toml` |
| `deck command` | Sets the deck's `default_command` to `claude --model opus --permission-mode auto`, the orchestrator's command: the New Agent form pre-fills it, and a dispatcher started with a blank command, and the single units it dispatches, run it. A command already set in the VM is kept (`dot-agent-deck config set default_command ...` changes it) |
| `daemon` | `dot-agent-deck daemon install`, unless `dot-agent-deck.service` is already installed |

It never prints a credential: the checks show only the field they test.

- **Run it while no Claude Code session runs in the VM**, as before the first `make deck`. Claude Code
  keeps `~/.claude.json` in memory and writes it back, so a session running at the same time can undo
  the trust flags `make vm-init` just wrote.

- **Trust follows the repository root.** Claude Code looks up a folder's trust by the session's git
  repository root, which for the repository, every dispatch worktree and every PR worktree is the main
  checkout, so `make vm-init` trusts the main checkout as well as the workspace. Trusting the workspace
  alone does not cover sessions in the repository.
- **The first-run flags are Claude Code's saved state, not documented settings.** `make vm-init` writes
  `hasCompletedOnboarding` and `projects["<directory>"].hasTrustDialogAccepted`, the flags a first manual
  run leaves behind, and a later Claude Code version may rename them. If a Claude Code pane then stops at
  a theme or trust screen, do the first run by hand, in the workspace and in the repository:

  ```bash
  make vm-shell
  (cd .. && claude)   # pick a theme, answer yes to trusting this folder, /exit
  claude              # the repository: answer yes to trusting this folder; /status must show the OAuth token, not an API key; /exit
  exit
  ```

- **`dot-agent-deck daemon install`** sets the deck's daemon up as a systemd user service and starts it,
  which is why `make vm-init` comes before the first `make deck`. If the TUI has already started a daemon
  of its own, the service is installed but not started: run `dot-agent-deck daemon stop` and
  `systemctl --user start dot-agent-deck.service` in the VM.
- **Agent panes have colour.** systemd gives the service no `TERM`, and every agent pane inherits the
  service's environment, so a second drop-in installed by `dev/lima.yaml`
  (`~/.config/systemd/user/dot-agent-deck.service.d/terminal.conf`) sets `TERM=xterm-256color` and
  `COLORTERM=truecolor`. A VM created before it has colourless panes; add the drop-in by hand, then
  restart the service (this stops the running agents):

  ```bash
  limactl shell --workdir / shift2eu sh -c 'printf "[Service]\nEnvironment=TERM=xterm-256color COLORTERM=truecolor\n" > ~/.config/systemd/user/dot-agent-deck.service.d/terminal.conf && systemctl --user daemon-reload'
  make deck-restart
  ```

### Every day

```bash
make vm-up && make deck    # the TUI attaches to the daemon service
```

- **Detach, don't stop.** Leave the TUI with `Ctrl+c` and **Detach**: the agents keep running in the
  service, and the next `make deck` shows them again. **Stop** ends the agents and the service until the
  VM restarts or you run `systemctl --user start dot-agent-deck.service`.
- **The service starts with the VM.** Lima enables lingering for the VM user, so no login is needed.
  `make vm-stop` stops the VM and every agent in it; `make vm-delete` removes the VM and everything
  inside it.

Tool versions are pinned in `dev/lima.yaml` under `param:`, and Renovate opens a PR when a new version
is released. A VM keeps the template it was created with, so after merging a version bump recreate it
with `make vm-delete vm-create`, then `make deck`: nothing has to be typed in the VM.

Lima itself runs on the Mac, so it is not pinned in the template: `dev/lima.yaml` accepts Lima 2.0 or
newer (`minimumLimaVersion`). CI runs `limactl validate dev/lima.yaml` with the Lima pinned as
`LIMA_VERSION` in `.github/workflows/vm-tools.yml`, installed from its release tarball once it is
checked against lima-vm/lima's GitHub build attestation (`gh attestation verify` with the runner's own
gh, pinned to Lima's `release.yml` and the commit its tag names) and against `LIMA_SHA256`, on every
pull request and weekly: a template Lima refuses to create a VM from fails the PR, not the next
`make vm-up` (only creating a VM runs Lima's checks; starting an existing one does not). Use that
Lima version on the Mac, so you and CI agree on what a valid template is; when Renovate bumps it,
`brew upgrade lima`. A warning counts as a refusal there: Lima only warns about an unknown
(for example misspelt) field, and still exits 0.

The same job proves the check can fail. Every template in `dev/testdata/lima-invalid/` is
`dev/lima.yaml` with one known breakage: a `param` read only through bash indirection, which Lima cannot
see (#120), an unknown field and a value of the wrong type. Beside each template, `<name>.expect` holds
one line of the reason Lima must give. CI validates each template and fails, naming it, if Lima accepts
it, which would mean a later Lima stopped catching that breakage, or if Lima refuses it without that
text in its message, which would mean it is refused for another reason than the one it tests.
`go test ./test/devenv` (part of `make check`) pins that each template differs from `dev/lima.yaml` by
its one-line breakage alone, so it cannot drift from the real template, that each `.expect` holds its
case's refusal text, and that the directory holds one template and one `.expect` per case and nothing
else. Renovate bumps the templates' pins together with `dev/lima.yaml`'s. After any other change to
`dev/lima.yaml`, rewrite them with `go test ./test/devenv -update` and commit the result. To add a case,
add an entry to `limaBreakages` in `test/devenv/limainvalid_test.go` (the template's name, the one line
it replaces and `refusal`, a substring of the message `limactl validate` prints for it), run
`go test ./test/devenv -update` to write its template and `.expect`, and check with `limactl validate`
that Lima refuses the new template with that text.

The same goes for the VM's size: `make vm-up` starts an existing VM with the CPUs, memory and disk it
was created with. To resize it without recreating it, keeping its tools, login and configuration:

```bash
make vm-stop
limactl edit --tty=false --memory=16 shift2eu   # memory in GiB; --cpus and --disk work the same way
make vm-up
```

## Dispatcher mode

A [dispatcher pane](https://agent-deck.devopstoolkit.ai/docs/dispatcher-mode.md) starts orchestrations
(or single agents) in the background, so several PRs can be worked on at once, each by its own
orchestrator. In the TUI press `Ctrl+n`, select the repository, set **Mode** to `dispatcher`, and ask it to
start work on an issue or PRD.

- **Where units run.** The deck creates each unit's worktree beside the repository, at
  `../shift2eu-dispatch-<name>` on branch `agent/dispatch-<name>`. That location is fixed, which is
  why the VM mounts the workspace rather than the repository
  ([ADR 0008](adr/0008-mount-a-workspace-directory-for-dispatcher-mode.md)).
- **PRs still get their own worktree.** A dispatched orchestrator follows the `start-work` skill like any
  other ([ADR 0004](adr/0004-one-worktree-per-pull-request.md)). Run from the dispatch worktree,
  `make worktree` creates the PR's branch and worktree under the dispatch worktree's own `.worktrees/`,
  for example `../shift2eu-dispatch-<name>/.worktrees/feat-12-short-name`, so the unit's agents
  never leave their working directory and never stop at a permission prompt
  ([ADR 0010](adr/0010-create-pull-request-worktrees-inside-the-dispatch-worktree.md)). `.worktrees/` is
  git-ignored, so the dispatch worktree stays clean. Its `agent/dispatch-*` branch never gets commits or
  a PR.
- **Cleaning up, in this order.** First remove each PR worktree inside the unit, which the `pr-create`
  merge step does: `git -C <main checkout> worktree remove <path>`. Then close the unit, with
  `dot-agent-deck close <unit>` or by closing its card or tab in the deck (in the TUI `Ctrl+W`, the
  card's `[Close]` or the tab's `[×]`; in the desktop app **Close agent** or **Close all N roles**). All
  of them run the same removal: they remove the unit's worktree, or keep it when `git status` there
  shows uncommitted changes. That check does not see the git-ignored `.worktrees/`, so a unit closed
  with a PR worktree still inside it loses that worktree, uncommitted changes included, even when the
  worktree is locked. The branch and its commits survive. `make worktree` then names the commands that
  clear that worktree's stale entry, `git worktree remove <path>`, after `git worktree unlock <path>`
  when the entry is locked, and brings the worktree back once they have run. Do not use
  `git worktree prune`: it clears every stale entry in the repository, including those of healthy
  worktrees on a disk that is not mounted.
  `dot-agent-deck worktree list` shows what is left. The `agent/dispatch-*` branches stay. Deleting
  local branches is left to you: an agent writes the list to a file and hands you the command to run,
  `xargs git branch -D < <file>` (CLAUDE.md, rule 8).

### Moving an existing checkout into a workspace

A checkout made before ADR 0008 sits directly in a shared directory, and its VM mounts only the
repository. Move it, then point the existing VM at the workspace; the VM keeps its tools, login and
configuration:

```bash
make vm-stop
mkdir ../shift2eu-ws && mv ../shift2eu ../shift2eu-ws/
cd ../shift2eu-ws/shift2eu
git worktree repair .worktrees/*                               # only if .worktrees/ holds any
limactl edit --tty=false --mount-only "$(dirname "$PWD"):w" shift2eu
make vm-up
direnv allow                                                   # direnv tracks .envrc files by path
```

Then, before `make deck`, run `make vm-init`, which trusts the new workspace and repository paths: the
trust given to the old paths does not carry over.

## Credentials

Nothing from the Mac's home directory is visible in the VM. `make vm-env` copies the variables named in
`VM_ENV` (Makefile) from the shell you run it in to `~/.config/dot-agent-deck/daemon.env` in the VM,
readable only by the VM user. The values travel over standard input, never on a command line; a variable
that is not set is skipped. Two things read the file:

- the deck's daemon service, through a systemd drop-in installed by `dev/lima.yaml`, so every agent it
  starts has the variables;
- every login shell in the VM (`/etc/profile.d/shift2eu.sh`), so `make vm-shell` has them too.

| Variable | Used by | Notes |
|---|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | Claude Code roles | From `claude setup-token`. Billed to your Claude subscription (Pro, Max, Ultra, Team or Enterprise), valid for a year, and limited to model requests. |
| `DEEPSEEK_API_KEY` | pi (tester) | Create a key for this project, so it can be revoked on its own, and keep the balance low. |
| `GITHUB_TOKEN` | `gh`, and `git push` through `gh auth git-credential` | A [fine-grained token](https://github.com/settings/personal-access-tokens) limited to this repository: contents, pull requests and issues read/write; actions read. |

- **Subscription, not pay-as-you-go.** Whenever `ANTHROPIC_API_KEY` is set, Claude Code uses it instead
  of the token and bills each call. `VM_ENV` leaves it out on purpose; do not add it. The five Claude
  Code roles share your subscription's usage limits.
- **Rotating a credential:** change it on the Mac, then `make vm-env deck-restart`. Restarting the service
  stops its running agents, so do it between tasks. Run `claude setup-token` again before the year is up.
- **Keep the values outside the workspace.** The VM mounts it, so a `.envrc` or `.env` in the
  workspace or this repository would be readable by every agent and by the model providers they send
  files to. A direnv `.envrc` in a directory above the workspace works: direnv loads it in this
  repository, and the VM cannot see it.
- **No token that can push images.** Never put a classic personal access token, or any token with
  `write:packages`, into `VM_ENV`: the token's scope is what stops agents pushing to GHCR.
- **Every agent gets every variable.** The tester holds the GitHub and Claude tokens, the Claude roles
  the DeepSeek key. Scope each one narrowly so it is cheap to revoke. The file stays on the VM's disk
  (under `~/.lima` on the Mac) until `make vm-delete`.
- **Cloud accounts (for integration tests and real analyses, later):** read-only identities only
  (`ReadOnlyAccess` or narrower on AWS, `Reader` on Azure, `roles/viewer` or narrower on GCP). Do not
  copy your personal admin credentials in.

### The agents App

PRD #161 moves the agents from your `GITHUB_TOKEN` to a GitHub App of yours, the **agents App**, so their
pull requests are the App's and you can approve them
([ADR 0029](adr/0029-agents-open-pull-requests-as-a-github-app-with-tokens-minted-on-the-mac.md)). Its
private key stays on the Mac: a job there mints installation tokens, valid for an hour, and writes only
those into the VM. Until you switch (the runbook in PRD #161; milestone 2 puts it here), nothing changes:
without a token file in the VM, `gh` and `git push` use `GITHUB_TOKEN` as before.

**Creating the App** (runbook step 1): install it on this repository only, with these repository
permissions:

| Permission | Access | Why |
|---|---|---|
| Contents | Read and write | Push branches; releases and their assets |
| Pull requests | Read and write | Open, view and merge pull requests |
| Issues | Read and write | File and read issues |
| Workflows | Write | Agents edit `.github/workflows/` |
| Actions | Read and write | Runs and environments (read); `gh run rerun` in the release-tag skill (write) |
| Checks | Read | `gh pr checks`, check-run annotations |
| Commit statuses | Read | `gh pr checks` |
| Attestations | Read | `gh attestation verify` in the release-tag skill |
| Metadata | Read | Required; rulesets, the repository itself |

Never give it Administration: the rulesets' bypass lists need Administration write, which could change
`Protect main`, so those checks stay yours (ADR 0029, d). Secrets and Variables are not needed either.
Generate a private key and keep the `.pem` **outside the workspace**, readable only by you
(`chmod 600`). The App's slug is never configured: the token script reads it from the App (`GET /app`).

**On the Mac**, in the direnv `.envrc` above the workspace, beside the other credentials:

```bash
export SHIFT2EU_AGENTS_APP_ID=...               # the App's settings page: App ID
export SHIFT2EU_AGENTS_APP_INSTALLATION_ID=...  # the number at the end of the installation's settings URL
export SHIFT2EU_AGENTS_APP_KEY_FILE=...         # the path of the .pem, outside the workspace
```

Never add them to `VM_ENV`: `make vm-env` refuses any `SHIFT2EU_AGENTS_APP_*` name, so neither the key
nor its path enters the VM.

| Command | Does |
|---|---|
| `make agents-token` | Mints one token and writes it, and the bot's identity, into the VM |
| `make agents-token-refresh` | Mints now and again before each token expires, in the foreground, until stopped |
| `make deck` | With the three variables set, also starts the refresh in the background, unless it already runs |

`make deck` keeps the refresh's pid and log in `${XDG_STATE_HOME:-~/.local/state}/shift2eu/` on the
Mac (`agents-token-refresh.pid`, `agents-token-refresh.log`) and prints where the log is. The refresh
checks the wall clock every minute and mints again when fewer than 20 minutes are left, so it carries on
after the Mac sleeps. When it stops, on a signal or after a mint fails with no valid token left, it writes
`agents App token refresh stopped` and the time to the log; the next `make deck` warns that it stopped,
shows the log's last lines and starts a new one. Stop it with
`kill "$(cat ~/.local/state/shift2eu/agents-token-refresh.pid)"`. No command prints a token, the JWT or the
key, and nothing writes them to a log or the workspace.

**In the VM**, both files are readable only by the VM user and replaced atomically:

| File | Holds | Read by |
|---|---|---|
| `~/.config/shift2eu/agents-app-token` | The token, then its expiry in Unix seconds | `gh`, on every call |
| `~/.config/shift2eu/agents-app-identity` | `<slug>[bot]`, then `<bot user id>+<slug>[bot]@users.noreply.github.com` | `make vm-init` (the `git` step) |

`/usr/local/bin/gh` is a root-owned wrapper (`dev/gh-app-token.sh`); the real gh is
`/usr/local/libexec/shift2eu/gh`. With the token file it runs gh with that token and without
`GITHUB_TOKEN`; without it, gh falls back to `GITHUB_TOKEN`; with a file that is expired, malformed or not
mode 0600, it fails and names `make agents-token-refresh`. The git credential helper calls the wrapper,
so `git push` follows the same rule, and a running agent's next call uses the newest token without
restarting the deck. A VM created before this change keeps the real gh at `/usr/local/bin/gh`: recreate it
(`make vm-delete vm-create`) to get the wrapper. After the first `make agents-token`, run `make vm-init`
so the agents commit as the App's bot.

**Rolling back**: delete the token file in the VM and gh falls back to `GITHUB_TOKEN`; delete the identity
file too and run `make vm-init` to commit as yourself again:

```bash
limactl shell --workdir / shift2eu sh -c 'rm -f "$HOME/.config/shift2eu/agents-app-token" "$HOME/.config/shift2eu/agents-app-identity"'
make vm-init
```

Stop the refresh first, or it writes a new token within a minute.

## Agent permissions

Each role's command in `.dot-agent-deck.toml` sets its permission mode explicitly, so no role depends
on the machine's default:

| Role | Agent | Starts in |
|---|---|---|
| orchestrator | Claude Code (Opus) | `--permission-mode auto` |
| coder, reviewer, auditor | Claude Code (Opus) | `--permission-mode auto --settings .claude/worker-settings.json` |
| releaser | Claude Code (Sonnet) | `--permission-mode auto --settings .claude/worker-settings.json` |
| tester | pi | no permission modes (see below) |

In auto mode Claude Code checks every action before it runs. It approves routine actions, such as
reading files, editing in the worktree and running `make`, `go`, `git` and `gh`, without a prompt, so a
worker does not stop mid-task to wait for a human. It still refuses actions it judges risky. A worker
reports a refused action rather than working around it. A single refusal does not wait for anyone, but
after 3 refusals in a row, or 20 in one session, auto mode pauses and asks a human. The coder used to
start in `acceptEdits`. That mode approves file edits only, so the coder stopped at almost every shell
command that was not on the allowlist (#16).

Everything below exists to keep the agents from waiting on you (#68). Three places hold it:

- **`.claude/settings.json`, checked in.** Permission rules apply on top of the mode: an allow rule skips
  the check, and a deny rule refuses the action. The deny rules (`--no-verify`, force pushes,
  `git branch -D` and `--delete`, see CLAUDE.md rule 8) and the allow rules that name no path (the fixed `make` targets, `go test/build/vet`,
  `golangci-lint run`, `dot-agent-deck work-done`, web search and the provider documentation sites)
  live here, so every checkout has them, including the dispatch worktrees the deck creates.
- **`.claude/worker-settings.json`, checked in.** Loaded by the workers only. It denies
  `AskUserQuestion`, `EnterPlanMode` and `ExitPlanMode`, the tools that stop a session until a human
  answers. Only the orchestrator talks to the user, so it keeps them.
- **`~/.claude/settings.json` in the VM, from `dev/claude-user-settings.json`.** `make vm-claude-settings`
  writes in the workspace path and merges the template into the VM user's settings: the template's keys
  replace the same keys there, its allow rules are added, and everything else is kept. Run it again after
  changing the template or recreating the VM. It holds what cannot be checked in as settings:
  - **One `cd` allow rule for the workspace.** It names the workspace's absolute path, which a checked-in
    file cannot know. There are no `git -C <path> ...` allow rules (#132): the `*` that stands for the
    path also matches git options, so `git -C <ws>/x -c core.pager=... log` or `diff --output=<file>`
    would skip the check, and the path varies, so no exact rule fits. Every Claude Code role runs in
    auto mode, where the classifier reviews those calls instead. For the same reason
    `.claude/settings.json` has no `make worktree BRANCH=*` rule: make accepts options such as
    `--eval` or `-f` after the target. Allow rules end in a `*` only where the trailing arguments cannot
    reach a shell, and never put one in the middle of a pattern. `make vm-claude-settings` only adds
    rules, so on a VM set up before #132, remove the old `Bash(git -C ...)` rules from
    `~/.claude/settings.json` by hand.
  - **`autoMode` context for the classifier:** that it runs in this VM, that tasks come from the
    orchestrator through the deck, which repository and domains are trusted, and that committing catalog
    snapshots from the providers' public pages to this public repository is normal work. Claude Code
    reads `autoMode` only from user settings, never from a repository, so that a repository cannot loosen
    its own checks. Every list keeps `"$defaults"`, so the built-in rules still apply. In the VM,
    `claude auto-mode config` shows the effective rules and `claude auto-mode critique` reviews them.
  - **A `PermissionDenied` hook** that appends each refusal (time, directory, tool, input, reason) to
    `~/.local/state/claude-code/permission-denied.jsonl` in the VM. Read it after a few PRs: a routine
    command refused again and again needs an allow rule, and a destination refused again and again needs
    an `autoMode.environment` entry. Add either to the template, not only to the VM.

Because the VM confines the agents to this repository's workspace, running the Claude Code workers with
`--dangerously-skip-permissions` inside it is a reasonable trade-off. It is not the default here: auto
mode already lets routine work run and still checks risky actions. Never use it for agents running
directly on the Mac.

pi, which runs the tester, has no permission prompts at all: it reads, edits and runs commands without
asking. Its `--approve` flag only makes it load the project's `.pi/settings.json` (and with it the
skills) without a trust prompt. For the tester the VM is the only boundary, so never start the deck
outside it.

## Containers and image verification

The VM can verify, run and build the shift2eu image, as the VM user, with no root daemon
([ADR 0017](adr/0017-run-a-rootless-container-runtime-in-the-lima-vm.md)). `dev/lima.yaml` installs,
each pinned under `param:` and authenticated before it is installed:

| Tool | For | Where | Verified by |
|---|---|---|---|
| nerdctl-full: `nerdctl`, containerd, BuildKit, RootlessKit | pulling, running and building images | `/usr/local/nerdctl` | `SHA256SUMS` signed by a release manager pinned by GPG fingerprint |
| `cosign` (v3 or later) | the image's signature | `/usr/local/bin` | SHA-256 pinned per architecture; each pin checked in CI against sigstore/cosign's keyless signature with an independently installed cosign (`vm-tools` workflow). In the VM, a self-check against its own bundle guards integrity only |
| `gh` from its release | `gh attestation verify`, which Ubuntu's older `gh` lacks | `/usr/local/bin` | SHA-256 pinned per architecture; each pin checked in CI against gh's build attestation (`gh attestation verify`, `vm-tools` workflow) |
| `crane` | resolving a tag to its digest without pulling the image | `/usr/local/bin` | `go install`, against the Go checksum database |
| `shellcheck` | `make lint`'s shell script lint, and actionlint's check of the workflows' `run:` scripts ([ADR 0022](adr/0022-lint-shell-scripts-and-workflows-with-shellcheck-and-actionlint.md)) | `/usr/local/bin` | SHA-256 pinned per architecture. The release has no signature or attestation; CI's `lint-shell` job checks the amd64 pin on every run, the arm64 one is checked only here, at provisioning, and `test/architecture` holds the VM's and CI's pins equal |

A VM created before shellcheck joined `dev/lima.yaml` has none: Lima copies the template when it creates
the VM, so later provisioning never adds it. `make lint` then fails with `shellcheck is not installed:
re-provision the VM or see docs/contributing/development.md`. Recreate the VM with
`make vm-delete vm-create`; the same holds after a Renovate bump of `shellcheckVersion`, which reaches
only VMs created after it.

A download that does not match its pin, or a cosign whose signature does not verify, stops provisioning
with a message naming the file and the `param:` involved; nothing is installed and the download is
deleted. In the VM the pin is what authenticates gh and cosign. CI is what makes a pin authentic:
`.github/workflows/vm-tools.yml` reads the pins from `dev/lima.yaml`'s `param:`, downloads each pinned
asset, verifies it with a cosign and a gh that do not come from the release under test, and fails when a
pin is not the verified asset's digest. It runs on every pull request and weekly, which catches an asset
replaced under a pinned tag ([ADR 0017](adr/0017-run-a-rootless-container-runtime-in-the-lima-vm.md),
"Amendment: authenticity is checked in CI"). Merge a change to `dev/lima.yaml` only when it is green:
the check is not yet a required status. To enforce it, add the status check
`Verify the gh and cosign pins` to the required checks of `main`'s ruleset (a repository setting, for
the maintainer).

### Updating gh and cosign

Renovate bumps `ghVersion` and `cosignVersion` together with their pinned digests (`ghSha256Amd64`,
`ghSha256Arm64`, `cosignSha256Amd64`, `cosignSha256Arm64`), in one PR per tool: each digest has a
`# renovate: datasource=github-release-attachments ... digestVersion=<tag>` comment, and Renovate takes
the new digest from the new release's checksum file, so the PR alone proves nothing about authenticity.
Merge it only when the **VM tools** check is green, and check that every `digestVersion` equals the new
version. A PR labelled `security-review` means an existing tag's asset changed: do not merge it without
finding out why. If Renovate could not map a digest, it leaves the old one, and the next
provisioning fails with `does not match ... in dev/lima.yaml`: the bump cannot install an unpinned
binary. To update a digest by hand, download the asset and check it before pinning it, on a machine
with a trusted cosign and gh (the Mac, or this VM), then let the VM tools check confirm it in the PR:

```bash
v=v3.1.4; a=arm64   # the new version, and amd64 or arm64
curl -fsSLO "https://github.com/sigstore/cosign/releases/download/${v}/cosign-linux-${a}"
curl -fsSLO "https://github.com/sigstore/cosign/releases/download/${v}/cosign-linux-${a}.sigstore.json"
cosign verify-blob --bundle "cosign-linux-${a}.sigstore.json" \
  --certificate-identity keyless@projectsigstore.iam.gserviceaccount.com \
  --certificate-oidc-issuer https://accounts.google.com "cosign-linux-${a}"
sha256sum "cosign-linux-${a}"                      # the value for cosignSha256<Arch>

v=v2.103.3
curl -fsSLO "https://github.com/cli/cli/releases/download/${v}/gh_${v#v}_linux_${a}.tar.gz"
# cli/cli builds releases on trunk, so the attestation names trunk and the commit the tag points at.
commit="$(gh api "repos/cli/cli/commits/refs/tags/${v}" --jq .sha)"   # needs a GitHub token, as below
gh attestation verify "gh_${v#v}_linux_${a}.tar.gz" -R cli/cli \
  --signer-workflow cli/cli/.github/workflows/deployment.yml \
  --source-ref refs/heads/trunk --source-digest "${commit}" --deny-self-hosted-runners
sha256sum "gh_${v#v}_linux_${a}.tar.gz"            # the value for ghSha256<Arch>
```

A VM installs a tool only when its version changes, so an existing VM keeps the binaries it has:
recreate it to get, and verify, the pinned ones. After recreating, confirm them:

```bash
/usr/local/bin/cosign version | grep GitVersion   # the cosignVersion in dev/lima.yaml
/usr/local/libexec/shift2eu/gh --version          # the ghVersion in dev/lima.yaml (/usr/local/bin/gh is the wrapper, ADR 0029)
sha256sum /usr/local/bin/cosign                   # cosignSha256<Arch> for this VM's architecture
# The provisioning output: one of these holds it. Check which one does on your VM. Not `journalctl -b`:
# that shows only the current boot, and the self-check runs on the boot that installs cosign.
sudo grep -E 'Verified OK|does not match|not signed' /var/log/cloud-init-output.log
sudo journalctl | grep -E 'Verified OK|does not match|not signed'
```

`Verified OK` is the cosign self-check, printed on the first boot after recreating the VM, the one that
installed cosign; there must be no `does not match` or `not signed`.

containerd and BuildKit run **rootless**, as the systemd user services `containerd.service` and
`buildkit.service`, started at boot like the deck's daemon. Check them, then run the
[Verify the image](guide/verify-releases.md#verify-the-image) commands in their VM form:

```bash
systemctl --user status containerd buildkit
nerdctl info                                  # answers from the VM user's own containerd
make image PLATFORMS=linux/amd64,linux/arm64  # builds both platforms with nerdctl
```

`make image` uses `docker` where it is installed and `nerdctl` otherwise (override with `DOCKER=`).
The `Dockerfile` cross-compiles, so building for both platforms needs no emulation, but the VM runs only
its own platform's image: no QEMU is registered, since that needs root.

**The isolation trade-off.** No `docker` group, no root daemon and no host socket: a container runs as the
VM user and its subordinate UIDs, so escaping it gains no more than the VM user already has. That holds
because the VM user already has passwordless `sudo` (Lima's default): there is no root left to protect
inside the VM. In exchange RootlessKit creates user namespaces, which Ubuntu otherwise restricts because
they are a common start of kernel privilege escalations; `dev/lima.yaml` adds an AppArmor profile that
allows it for `/usr/local/nerdctl/bin/rootlesskit` (the same profile Ubuntu ships for
`/usr/bin/rootlesskit`), and any VM process can run that program, so any VM process can create a user
namespace through it. `uidmap` adds the setuid-root `newuidmap` and `newgidmap`. If `sudo` is ever taken
away from the VM user, revisit the profile and `uidmap`: they would then be a path towards root. None of
it reaches the Mac. No registry login or credential helper is configured: pulls are anonymous, and agents never push
(`release.yml` does). Do not run `nerdctl login` in the VM.

In a rootless runtime, container UID 0 is the VM user, not root. To let a container read your `0600`
inventory, run it with `--user 0:0` rather than the `--user "$(id -u):$(id -g)"` of
[Run with Docker](guide/docker.md#mounts-users-and-reports), which in the VM is a subordinate UID.

## What leaves the VM

Each agent sends what it reads (source, command output, files) to its model provider: Anthropic for the
Claude Code roles, DeepSeek for the tester. Everything under the workspace directory is visible to
every agent: this repository, its dispatch worktrees and the git-ignored `inventories/` and `reports/`. Keep inventories and reports from
real cloud accounts outside the repository, and use only sanitised fixtures inside it. The same goes for
the environment: an agent that prints it sends the API keys and tokens above to its provider, which is
why `CLAUDE.md` forbids printing it.

## Stronger isolation: sync instead of mount

With a writable mount, agents change files on the Mac live. Lima 2.1+ can instead copy the repository
into a VM that has no host mounts and sync changes back only when you accept them:

```bash
limactl start --name=shift2eu-sync --mount-none dev/lima.yaml
limactl shell --sync . --sync-exclude .git shift2eu-sync
```

This suits one-off agent runs. For a long-lived orchestration that commits and opens PRs, the writable
mount is the practical choice.
