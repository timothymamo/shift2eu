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
   ```

   The agents' GitHub credential is not one of them: it is the agents App's installation token, which a
   job on the Mac mints and writes into the VM ([The agents App](#the-agents-app)).

4. Create the VM, copy the credentials into it and set it up:

   ```bash
   brew install lima          # the LIMA_VERSION in .github/workflows/vm-tools.yml (see below)
   make vm-create             # several minutes the first time
   ```

   `make vm-create` runs, in order:

   | Target | Does |
   |---|---|
   | `make vm-env-check` | Checks on the Mac that every `VM_ENV` variable is set, before anything is created ([Credentials](#credentials)) |
   | `make vm-up` | Creates and provisions the VM, or starts it |
   | `make vm-env` | Copies the two variables into the VM ([Credentials](#credentials)) |
   | `make vm-claude-settings` | Claude Code user settings for the agents ([Agent permissions](#agent-permissions)) |
   | `make vm-init` | The once-only setup below, and the checks that the credentials work |

   Each can also be run on its own. The VM has 4 CPUs, 16 GiB of memory and a 40 GiB disk
   (`dev/lima.yaml`), enough for all six roles to run `make check` at once. Leave the Mac enough free
   memory to run it. `VM_CPUS`, `VM_MEMORY`, `VM_DISK` and `VM_SSH_PORT` set another size and a fixed SSH
   port when the VM is created ([Several VMs on one host](#several-vms-on-one-host)).

   On a new VM, `make vm-init`'s `gh` step fails until the agents App's token is in the VM: set up the App
   and the host clone ([The agents App](#the-agents-app)), run `make agents-token-refresh-start` from the
   host clone, then run `make vm-init` again.

5. Open the deck: type what `make deck` runs, from a directory outside the workspace, such as your home
   (`cd ~`), not `make deck` in the workspace: a git-aware prompt in the workspace runs git there with
   its `.git/config`, which the agents can edit (What the host clone does not do, in
   [The agents App](#the-agents-app)).

   ```bash
   limactl shell --workdir <path of the main checkout> shift2eu dot-agent-deck   # the VM instance's name, LIMA_INSTANCE
   ```

### What `make vm-init` does

`make vm-init` (`dev/vm-init.sh`) runs in a login shell in the VM, because only a login shell loads the
credentials `make vm-env` wrote. It stops at the first step that fails, naming it, for example
`vm-init: pi: no DeepSeek key; set DEEPSEEK_API_KEY for make vm-env, ...`. Fix the cause and run it
again: it changes only what is not set up yet, so running it on a VM that is set up changes nothing.

| Step | What it does |
|---|---|
| `theme` | Sets `theme` in Claude Code's `~/.claude/settings.json` to `CLAUDE_THEME` (default `dark`; `make vm-init CLAUDE_THEME=light`) |
| `claude` | Marks Claude Code's onboarding done and trusts the workspace and the repository, in `~/.claude.json`, so the Claude Code roles start without first-run screens (see below) |
| `git` | Gives the VM your git identity, so the agents commit as you: `git config user.name` and `user.email` from the Mac, or `GIT_USER_NAME=` and `GIT_USER_EMAIL=`. An identity already set in the VM is kept. With the agents App's identity file in the VM ([The agents App](#the-agents-app)) the agents commit as the App's bot instead, which replaces any other identity; without the file, a bot identity left from earlier gives way to yours (`dev/vm-git-identity.sh`). On a new VM the file is not there yet, so this step sets your identity; the first `make agents-token` writes the file and sets the bot's identity in the VM's git itself, so nothing has to be re-run |
| `claude auth` | Checks that Claude Code uses the OAuth token (your subscription), not an API key |
| `pi` | `pi auth check --provider deepseek` |
| `gh` | `gh api repos/<owner>/<repo> --jq .full_name` for the main checkout's `origin`: gh can read the repository with the agents App's token; without the token file the gh wrapper refuses to run gh at all. Not `gh auth status`, which an installation token may fail (ADR 0031) |
| `deck` | `dot-agent-deck validate`: checks `.dot-agent-deck.toml` |
| `deck command` | Sets the deck's `default_command` to `claude --model opus --permission-mode auto`, the orchestrator's command: the New Agent form pre-fills it, and a dispatcher started with a blank command, and the single units it dispatches, run it. A command already set in the VM is kept (`dot-agent-deck config set default_command ...` changes it) |
| `deck dir` | Sets the deck's `default_dir` to the main checkout, on every run (see below) |
| `daemon` | `dot-agent-deck daemon install`, unless `dot-agent-deck.service` is already installed. It never restarts an installed service (that would stop every running agent): when `deck dir` changed the deck's configuration and the service runs, it says to run `make deck-restart` between tasks so the daemon reads the new `default_dir` |

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

- **Agents start browsing in the main checkout.** `default_dir`, in the VM's
  `~/.config/dot-agent-deck/config.toml`, is where `Ctrl+N`, the Schedules manager's Add and the desktop
  app's New agent open the directory picker. `make vm-init` sets it to the main checkout it is given
  (for example `/Users/example/agents/shift2eu-ws/shift2eu`, mounted at the same path in the VM) with
  `dot-agent-deck config set default_dir`, before the daemon service starts. Without it the picker opens
  in the VM user's home, which holds only `go/`, and an agent opened there gets no `CLAUDE.md`, no roles
  from `.dot-agent-deck.toml` and no dispatcher memory, which is keyed by the checkout's path. It is a
  starting point, not a limit. A rebuilt VM gets it again from `make vm-create`, and a re-run of
  `make vm-init` sets the same value. `make vm-init` never restarts a running daemon: when it changed
  `default_dir`, run `make deck-restart` between tasks so the daemon reads it.
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
  limactl shell --workdir / shift2eu sh -c 'printf "[Service]\nEnvironment=TERM=xterm-256color COLORTERM=truecolor\n" > ~/.config/systemd/user/dot-agent-deck.service.d/terminal.conf'
  make deck-restart
  ```

- **`make deck-restart` reloads the drop-ins.** It runs `systemctl --user daemon-reload` before
  `systemctl --user restart dot-agent-deck.service`, so a changed drop-in takes effect at that restart
  and systemd never warns that the unit changed on disk. `dev/lima.yaml`'s provisioning rewrites a
  drop-in only when its content differs, and then reloads the user manager when it is reachable.

### Every day

```bash
make vm-up    # from the workspace
cd ~ && limactl shell --workdir <path of the main checkout> shift2eu dot-agent-deck   # the TUI attaches to the daemon service
```

Type the second line from outside the workspace, not `make deck` in it: a git-aware prompt in the
workspace runs git there with its `.git/config`, which the agents can edit (What the host clone does not
do, in [The agents App](#the-agents-app)).

- **Detach, don't stop.** Leave the TUI with `Ctrl+c` and **Detach**: the agents keep running in the
  service, and the deck shows them again the next time you open it. **Stop** ends the agents and the
  service until the VM restarts or you run `systemctl --user start dot-agent-deck.service`.
- **The service starts with the VM.** Lima enables lingering for the VM user, so no login is needed.
  `make vm-stop` stops the VM and every agent in it; `make vm-delete` removes the VM and everything
  inside it.

Tool versions are pinned in `dev/lima.yaml` under `param:`, and Renovate opens a PR when a new version
is released. A VM keeps the template it was created with, so after merging a version bump recreate it
with `make vm-delete vm-create`, then, once the agents use the App ([The agents App](#the-agents-app)),
`make agents-token-refresh-start` from the host clone, then `make deck`: nothing has to be typed in the VM.
The refresh's first mint writes the bot identity and sets it as the VM's git identity, so the agents
commit as the bot without another step.

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

### Several VMs on one host

A host that runs one Lima VM per repository gives each VM a size and an SSH port of its own. `make vm-up`
and `make vm-create` (which hands them on to `make vm-up`) take four optional variables:

| Variable | Value | Passed to `limactl start` as |
|---|---|---|
| `VM_CPUS` | A whole number from 1 to 999 | `--cpus=N` |
| `VM_MEMORY` | A whole size in GiB, at most 5 digits, such as `7GiB` | `--memory=N` (GiB) |
| `VM_DISK` | A whole size in GiB, at most 5 digits, such as `58GiB` | `--disk=N` (GiB) |
| `VM_SSH_PORT` | A port from 1024 to 65535 | `--ssh-port=N` |

Give them on the command line or in the environment: an exported `VM_CPUS` in your shell sizes every VM
`make vm-up` creates from that shell, and a value on the command line wins over the environment's.
Unset, `dev/lima.yaml`'s 4 CPUs, 16 GiB and 40 GiB stand and Lima picks the SSH port, as before. Any other
value fails before `limactl` runs, naming the variable. They apply only when the VM is created: an
existing VM is never resized, and `make vm-up` warns that the variables it was given were ignored.

To resize an existing VM without recreating it, keeping its tools, login and configuration, stop it and
edit it with `limactl edit` (sizes in GiB; Lima can grow a disk but not shrink it):

```bash
make vm-stop
limactl edit --tty=false --cpus=5 --memory=7 --disk=58 shift2eu
limactl edit --tty=false --set '.ssh.localPort = 60022' shift2eu   # only to change the SSH port
make vm-up
```

The alternative is to rebuild it, which loses everything inside it (its login, tools and configuration);
`make vm-create` sets it up again:

```bash
make vm-delete && make vm-create VM_CPUS=5 VM_MEMORY=7GiB VM_DISK=58GiB VM_SSH_PORT=60022
```

A client that reaches the VM through the host, with `ProxyJump` to `127.0.0.1:<port>` in its
`~/.ssh/config`, must use the same port as `VM_SSH_PORT`, so pick one per VM and keep it. Its
`IdentityFile` is the client's copy of the host's Lima key, `~/.lima/_config/user` on the host:

```text
Host shift2eu-vm
  HostName 127.0.0.1
  Port 60022
  ProxyJump mac-mini.example.com
  IdentityFile /Users/example/.lima/_config/user
```

### Troubleshooting: a Broken instance

`limactl list` shows an instance as `Broken` when Lima cannot load it, for example when its directory (such
as `/Users/example/.lima/shift2eu`, or `$LIMA_HOME/shift2eu` when `LIMA_HOME` is set) has no `lima.yaml`.
`limactl start shift2eu` on an instance without a `lima.yaml` reads the name as a template and falls back
to Lima's `template:default`, a VM that mounts the whole home directory instead of only the workspace;
without a terminal it does so without asking. So `make vm-up` starts an existing instance only when it is
`Stopped` or `Running` and its `lima.yaml` is there: for `Broken` it stops before any `limactl start` and
names the directory, for a `Stopped` or `Running` instance whose `lima.yaml` is gone or a dangling symlink
it stops too, for any other status it stops and names the status, and when it cannot list the instances or
read the status at all it stops and says so. Before any of that it refuses a `LIMA_INSTANCE` that is not
letters, digits, `.`, `_` and `-`, starting with a letter or digit.

Which fix applies depends on whether the directory still holds a `lima.yaml`:

- **`lima.yaml` is missing.** `make vm-delete` cannot clean it up: `limactl delete --force` refuses a
  directory without `lima.yaml`. Remove the directory by hand, once no `limactl` job uses it.
- **`lima.yaml` is there**, even as a dangling symlink. The instance is broken for another reason, such as
  a `lima.yaml` Lima cannot load, and its disk may well be intact. Do not delete it: see why with
  `limactl list` and `limactl start --debug shift2eu`, fix the `lima.yaml` (or whatever the debug output
  names), then run `make vm-up` again.

The rest of this section is about the first case.

If the directory comes back after `rm -rf`, even across a reboot, a launchd job (a LaunchDaemon or
LaunchAgent running `limactl start shift2eu`, for example one that starts the VM at boot) is recreating
it: launchd creates the job's `StandardOutPath` and `StandardErrorPath` log files, which sit in the
instance directory, every time it runs the job. Find it with `launchctl print system | grep -i lima` and
in `~/Library/LaunchAgents` and `/Library/LaunchDaemons`, unload it (`launchctl bootout`), then remove the
directory and run `make vm-up` to create the VM from `dev/lima.yaml`.

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
readable only by the VM user. The values travel over standard input, never on a command line. Two things
read the file:

- the deck's daemon service, through a systemd drop-in installed by `dev/lima.yaml`, so every agent it
  starts has the variables;
- every login shell in the VM (`/etc/profile.d/shift2eu.sh`), so `make vm-shell` has them too.

| Variable | Used by | Notes |
|---|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | Claude Code roles | From `claude setup-token`. Billed to your Claude subscription (Pro, Max, Ultra, Team or Enterprise), valid for a year, and limited to model requests. |
| `DEEPSEEK_API_KEY` | pi (tester) | Create a key for this project, so it can be revoked on its own, and keep the balance low. |

`gh`, and `git push` through `gh auth git-credential`, use the agents App's installation token, which is
not in `VM_ENV`: the mint on the Mac writes it into the VM ([The agents App](#the-agents-app)). No personal
GitHub token of yours enters the VM (PRD #161 milestone 3).

- **Subscription, not pay-as-you-go.** Whenever `ANTHROPIC_API_KEY` is set, Claude Code uses it instead
  of the token and bills each call. `VM_ENV` leaves it out on purpose; do not add it. The five Claude
  Code roles share your subscription's usage limits.
- **Every variable must be set, or nothing is written.** `make vm-env` first checks every name in
  `VM_ENV`. If one is unset or empty, it names it (never a value), exits non-zero without calling
  `limactl`, and leaves the VM's `daemon.env` byte for byte as it was. `make vm-create` runs the same
  check (`make vm-env-check`, which `make vm-env` runs first too) before `make vm-up`, so a missing
  credential, or any `VM_ENV` `make vm-env` would refuse, stops it before any `limactl` call: it creates
  and provisions nothing, instead of leaving a half-set-up VM. A `VM_ENV` on its command line is checked
  the same way. This keeps a locked login Keychain or an unloaded `.envrc` from wiping the credentials the
  agents need ([Troubleshooting](#troubleshooting-credentials)). An empty `VM_ENV` (`make vm-env VM_ENV=`)
  names no credential and is refused the same way; a name listed twice is written once. A `VM_ENV`
  holding anything but variable names, commas and spaces (a quote, a `$`, a `*`) is refused before any
  shell reads it; the refusal names `VM_ENV` but never shows its value, which may be a credential typed
  by mistake.
- **The default `VM_ENV` replaces the whole file.** Plain `make vm-env` writes exactly the two
  variables to `daemon.env`, so a stale line, such as an old `ANTHROPIC_API_KEY` or the personal GitHub
  token a VM was set up with before PRD #161 milestone 3, is removed. Only a
  `VM_ENV` on the make command line merges; an exported `VM_ENV` replaces like the default, so a `VM_ENV`
  left in a `.envrc` cannot keep a stale credential.
- **A `VM_ENV` on the command line merges.** `make vm-env VM_ENV=...` (or `make vm-create VM_ENV=...`)
  replaces the lines of the names in `VM_ENV` and keeps the other variable lines already in `daemon.env`
  (it creates the file when there is none). So `make vm-env VM_ENV=DEEPSEEK_API_KEY` replaces only
  `DEEPSEEK_API_KEY` and keeps the OAuth token. Only plain `NAME=value` lines are kept:
  any other line (a command, a comment, a value the shell would expand) is dropped, with a warning that
  says how many lines were dropped, never what they held. A merge prints the names of the variable lines
  it kept, never their values: `vm-env: kept (unchanged): CLAUDE_CODE_OAUTH_TOKEN DEEPSEEK_API_KEY`.
  Either way the file is written to a temporary file beside `daemon.env`, mode 0600, and moved into
  place, so a failure does not leave half a file. An interrupted run removes its temporary file, and a
  leftover from a killed run is removed by the next `make vm-env` once it is older than a minute, so a
  concurrent run's fresh temporary file is left alone (running `make vm-env` twice at once is not
  otherwise supported). A run whose `daemon.env` exists but cannot be read, merge or replace, is refused
  with a `vm-env:` message naming `daemon.env` (it cannot check the file for a GitHub token), and nothing is
  written: make the file readable and run it again.
- **Rotating a credential:** change it on the Mac, then `make vm-env deck-restart`. Restarting the service
  stops its running agents, so do it between tasks. Run `claude setup-token` again before the year is up.
- **Keep the values outside the workspace.** The VM mounts it, so a `.envrc` or `.env` in the
  workspace or this repository would be readable by every agent and by the model providers they send
  files to. A direnv `.envrc` in a directory above the workspace works: direnv loads it in this
  repository, and the VM cannot see it.
- **No GitHub token in `VM_ENV`.** Never put a personal access token, or any token with
  `write:packages`, into `VM_ENV`: the agents' only GitHub credential is the agents App's token, whose
  permissions (no Packages) are what stop agents pushing to GHCR. `make vm-env-check`, and so `make vm-env`
  and `make vm-create`, refuses a `VM_ENV` that names a GitHub token variable gh reads (the personal one,
  `GH_TOKEN`, `GH_ENTERPRISE_TOKEN` or `GITHUB_ENTERPRISE_TOKEN`) before any `limactl` call: it names the
  variable, never its value, points at `make agents-token`, and writes nothing. A merge drops a stale line
  of any of them from the VM's `daemon.env` instead of keeping it (a replace rewrites the whole file), and
  either one then warns on stderr: it names the variable it removed, never its value, and says to revoke
  that token at GitHub, as every agent in the VM could read it. The warning covers any line that assigns
  one of those names (spaces around the `=`, behind `export` or `declare -x`), not a longer name that
  only ends in one (a `MY_` prefix).
- **Every agent gets every variable.** The tester holds the Claude token and the App's token, the Claude
  roles the DeepSeek key. Scope each one narrowly so it is cheap to revoke. The file stays on the VM's disk
  (under `~/.lima` on the Mac) until `make vm-delete`.
- **Cloud accounts (for integration tests and real analyses, later):** read-only identities only
  (`ReadOnlyAccess` or narrower on AWS, `Reader` on Azure, `roles/viewer` or narrower on GCP). Do not
  copy your personal admin credentials in.

### Troubleshooting credentials

**`make vm-env` or `make vm-create` refuses: `vm-env: not set or empty: CLAUDE_CODE_OAUTH_TOKEN ...`.**
The shell you ran it in does not have the variables. A refused `make vm-create` has created nothing yet,
and says so: `vm-create: nothing was created: the credential check runs before the VM is created`. Fix it
in this order:

1. If your credentials come from the macOS login Keychain (a `.envrc` that reads them with `security`),
   an SSH session has the Keychain locked, so every lookup comes back empty. Unlock it; it asks for the
   account password:

   ```bash
   security unlock-keychain ~/Library/Keychains/login.keychain-db
   ```

2. Leave and re-enter the checkout, so direnv reads the `.envrc` again:
   `cd ~ && cd /Users/example/agents/shift2eu-ws/shift2eu`. On a host without the Keychain, check that the
   `.envrc` above the workspace exists, sets the variables and is allowed (`direnv allow`), or export
   them in the shell yourself.
3. Re-run `make vm-env`, or the target that ran it (`make vm-create`).

The VM's `daemon.env` is untouched by the refused run, so the running agents keep their credentials.

### The agents App

PRD #161 moved the agents from your personal GitHub token to a GitHub App of yours, the **agents App**,
so their pull requests are the App's and you can approve them
([ADR 0031](adr/0031-agents-open-pull-requests-as-a-github-app-with-tokens-minted-on-the-mac.md)). Its
private key stays on the Mac: a job there mints installation tokens, valid for an hour, and writes only
those into the VM. The App's token is the agents' only GitHub credential: without a token file in the VM,
`gh` and `git push` fail (PRD #161 milestone 3, [The switch-over runbook](#the-switch-over-runbook)).

**Creating the App** (runbook step 1): install it on this repository only, with these repository
permissions:

| Permission | Access | Why |
|---|---|---|
| Contents | Read and write | Push branches; releases and their assets |
| Pull requests | Read and write | Open, view and merge pull requests |
| Issues | Read and write | File and read issues |
| Workflows | Write | Agents edit `.github/workflows/` |
| Actions | Read | Runs and environments. Not write: a re-run (`gh run rerun`) stays yours, as write would also let an agent cancel runs, delete runs and logs, and disable or dispatch workflows (ADR 0031, d) |
| Checks | Read | `gh pr checks`, check-run annotations |
| Commit statuses | Read | `gh pr checks` |
| Attestations | Read | `gh attestation verify` in the release-tag skill |
| Metadata | Read | Required; rulesets, the repository itself |

Never give it Administration: the rulesets' bypass lists need Administration write, which could change
`Protect main`, so those checks stay yours (ADR 0031, d). Secrets and Variables are not needed either.
Generate a private key and keep the `.pem` **outside the workspace**, a regular file (not a symlink: the
script refuses one), readable only by you (`chmod 600`). The App's slug is never configured: the token script reads it from the App (`GET /app`).

**The host clone.** The token targets run on the Mac, as you, with the key's path in their environment,
so they must run code the agents cannot edit. The workspace is mounted read-write into the VM, `.git`
included: an agent can change its `Makefile` (or add a `GNUmakefile`, which make reads first), `dev/`, and
`.git/config`, whose `core.fsmonitor`, `core.pager`, hooks and remote URL make `git` itself run commands
or fetch another `main`. So no check run in the workspace helps: a `git diff` against `origin/main`, or any
other `git` or `make` command there, is agent-controlled code the moment an agent wants it to be. Keep a
second clone outside the workspace instead, which the VM never mounts, for example `~/src/shift2eu-host`:

```bash
git clone https://github.com/timothymamo/shift2eu.git ~/src/shift2eu-host   # once
git -C ~/src/shift2eu-host pull --ff-only                                  # before each use: reviewed main from GitHub
```

Update it only like that, never from the workspace, and never edit it. Give it its own direnv `.envrc`
(`~/src/shift2eu-host/.envrc`, then `direnv allow` there) with the App's three variables, and only there,
**never in the `.envrc` above the workspace**, so a shell in the workspace does not have the key's path in
its environment (code run from the workspace can still find it and read the key: see below):

```bash
export SHIFT2EU_AGENTS_APP_ID=...               # the App's settings page: App ID
export SHIFT2EU_AGENTS_APP_INSTALLATION_ID=...  # the number at the end of the installation's settings URL
export SHIFT2EU_AGENTS_APP_KEY_FILE=...         # the path of the .pem, outside the workspace and the clone
```

Never add them to `VM_ENV`: `make vm-env` refuses any `SHIFT2EU_AGENTS_APP_*` name, so neither the key
nor its path enters the VM. `make deck` warns when one of them reaches it (it still opens the deck).

Run these from the host clone (`cd ~/src/shift2eu-host`); they need only `limactl` and `LIMA_INSTANCE`,
nothing of the workspace:

| Command | Does |
|---|---|
| `make agents-token` | Mints one token and writes it, and the bot's identity, into the VM, and sets that identity as the VM user's git identity (`user.name` and `user.email` in `~/.gitconfig`) |
| `make agents-token-refresh` | Mints now and again before each token expires, in the foreground, until stopped |
| `make agents-token-refresh-start` | Starts that refresh in the background, unless one already runs for the VM, and prints where its log is |

And from the workspace, as before:

| Command | Does |
|---|---|
| `make vm-gh-wrapper` | Gives a VM created before the gh wrapper the wrapper, without recreating it |
| `make deck` | Opens the deck; it starts no refresh. Rather type `limactl shell --workdir <main checkout> shift2eu dot-agent-deck` from outside the workspace, such as your home (What the host clone does not do, below) |

**What the host clone does not do.** It keeps the key's path out of the workspace's shells and keeps the
token targets on code the agents cannot edit. It does not put the key out of reach of code run from the
workspace. Every host target you still run from the workspace (`make deck`, `make vm-env`, `make vm-init`,
`make vm-gh-wrapper`) runs agent-editable code on the Mac as you, and so with your uid, which can read the
key file: its path is in the host clone's `.envrc` and in the running refresher's environment. That code
can also tamper with the host clone itself (its `.git/hooks`, its `.git/config`, its files), which
`git pull --ff-only` does not undo, so the next token target there would run it with the key. The App key
is therefore one of the credentials code run from the workspace on the Mac can reach, with every
credential `make vm-env` copies from the `.envrc` above the workspace:

- the Claude token and the DeepSeek key;
- the agents App's private key, the file `SHIFT2EU_AGENTS_APP_KEY_FILE` names, which never expires.

Until the follow-ups below, keep that code's chances few:

- Open the deck by typing what `make deck` runs, directly, not through `make` in the workspace, and type
  it from a directory outside the workspace, such as your home: a git-aware prompt in the workspace runs
  git there with its `.git/config`, which the agents can edit:

  ```bash
  limactl shell --workdir <path of the main checkout> shift2eu dot-agent-deck   # the VM instance's name, LIMA_INSTANCE
  ```

- Run `make vm-env`, `make vm-init` and `make vm-gh-wrapper` only rarely, and only after reading the diff
  of the `Makefile` and `dev/` on GitHub.

No git check in the host clone is a safeguard: once the clone is tampered with, git there runs the
tampered hooks and config too. The open follow-ups (ADR 0031, Consequences): run every host target from
the host clone (`make deck` needs a variable for the workspace path instead of `CURDIR`), and keep the key
where your uid cannot read it silently (a Keychain item or a password-manager read that prompts, or a
separate macOS account running the refresher).

`make agents-token-refresh-start` keeps the refresh's pid and log in
`${XDG_STATE_HOME:-~/.local/state}/shift2eu/` on the Mac, named after the VM instance
(`agents-token-refresh-shift2eu.pid`, `agents-token-refresh-shift2eu.log`, mode 0600), and prints where
the log is. The refresh checks the wall clock every minute and mints again when fewer than 20 minutes are
left, so it carries on after the Mac sleeps. A failure that goes away by itself (no network yet after a
wake, GitHub answering 5xx or 429, a 403 that is GitHub's rate limit, the VM stopped) never stops it, even
once the token has expired: it logs one line and tries again, every minute at first and at most every
five. A rate limit is waited out: never before its `retry-after`, nor, when no request is left, before its
`x-ratelimit-reset`, whichever is later, each up to an hour. The token is written into the VM as soon as
it is minted; if the bot user's lookup then fails, the token stays and the lookup is tried again at the
next check, without a new mint. It stops only on what you have to fix (GitHub refusing the App's key, the
App, its installation or the bot user, a key that cannot sign, a symlink in the VM's `~/.config/shift2eu`),
or on a signal, and then writes `agents App token refresh stopped`, the time and where to start it again
(the host clone on the Mac, `make agents-token-refresh-start`) to the log; the next `make agents-token-refresh-start`
warns that it stopped, shows the log's last lines and starts a new one. One refresh runs per VM: it holds
the lock `agents-token-refresh-shift2eu.lock` in the same directory, with its pid inside. A second one (a
`make agents-token-refresh` while the background one runs, say) says that another refresher for that VM
is running and exits with status 0, and `make agents-token-refresh-start` names the pid in the lock and
starts nothing. A lock left by a killed refresh is taken over, by one refresh only. Stop it with
`kill "$(cat ~/.local/state/shift2eu/agents-token-refresh-shift2eu.lock/pid)"`. No command prints a token,
the JWT or the key, and nothing writes them to a log or the workspace.

**In the VM**, both files are readable only by the VM user and replaced atomically; the write refuses
a `~/.config/shift2eu` that is a symlink or resolves outside the VM user's home, or a token or identity
file or a `~/.gitconfig` that is a symlink or not a regular file (a guard against a symlink left by
mistake, not against the VM user). Whenever it writes the identity file it also sets the VM user's git
identity to it (`user.name` and `user.email` in `~/.gitconfig`, replacing every value there), and says
so ("set the VM shift2eu's git identity ..."). That write is atomic and takes git's own lock: a copy
of `~/.gitconfig` in `~/.gitconfig.lock` gets the bot's identity, and replaces the file only when git,
following the copy's `[include]` files, reads the bot's identity back; otherwise `~/.gitconfig` stays as
it was. A rewritten `~/.gitconfig` is mode 0600; one that already holds exactly the bot's identity is not
rewritten at all, and then no lock is looked at, so a stale `~/.gitconfig.lock` blocks only a mint that
must still write the identity. A `~/.gitconfig.lock` another git writer holds is left alone: that mint
writes nothing and the next one tries again. If no git is running in the VM, the lock is a crashed git's:
remove `~/.gitconfig.lock` in the VM. A `~/.gitconfig.lock` that is a symlink or not a regular file is
no lock git wrote: the mint refuses it and leaves it alone, and the refresh stops; remove it in the VM.
It refuses a name or address that is not a bot's (a slug begins with a letter or digit), a VM without
git, an `[include]` that would override the bot's name or address, and any `[includeIf]` (the check
cannot evaluate a conditional include, which applies only inside the workspace's repositories). Such an
`[include]` or any `[includeIf]` added to `~/.gitconfig` while a refresh runs makes the next mint refuse
and the refresh stop: remove it in the VM and start the refresh again (`make agents-token-refresh-start`).
Once the bot identity is known, every mint writes it, so a running refresh applies it again at each mint;
only a mint before the bot user's lookup has worked writes the token alone, and that write never touches
the git configuration.

| File | Holds | Read by |
|---|---|---|
| `~/.config/shift2eu/agents-app-token` | The token, then its expiry in Unix seconds, five minutes before GitHub's | `gh`, on every call |
| `~/.config/shift2eu/agents-app-identity` | `<slug>[bot]`, then `<bot user id>+<slug>[bot]@users.noreply.github.com` | Written by the mint, which applies it to the VM's git at once; `make vm-init` applies it again (the `git` step, `dev/vm-git-identity.sh`) |

`/usr/local/bin/gh` is a root-owned wrapper (`dev/gh-app-token.sh`, with the real gh's path pinned); the
real gh is `/usr/local/libexec/shift2eu/gh`. With the token file it runs gh with that token as `GH_TOKEN`
and unsets the other GitHub token variables gh reads (the personal one, `GH_ENTERPRISE_TOKEN` and
`GITHUB_ENTERPRISE_TOKEN`) first; without the file, or with one that is expired, malformed or not mode
0600, it fails without running gh, names the file, and asks for you to run
`make agents-token-refresh-start` in the host clone on the Mac. There is no fallback (PRD #161 milestone 3).
The git credential helper calls the wrapper, so `git push` follows the same rule, and a running agent's
next call uses the newest token without restarting the deck. The wrapper is plumbing, not a boundary: the
VM user has `sudo` and can run the real gh, but with no GitHub credential in the VM other than the App's
token, that gh has nothing else to act as.

A VM created before this change keeps the real gh at `/usr/local/bin/gh`, and `make agents-token` warns
that it "predates the gh wrapper". Run `make vm-gh-wrapper` (it moves the real gh, installs the wrapper
and points the git credential helper at it, whole or not at all, and ends with `vm-gh-wrapper: done`, without which `make` fails; safe
to run again), or recreate the VM with
`make vm-delete vm-create`, **before runbook step 3**. `make agents-token` also warns when the VM's clock
is more than a minute off the Mac's. The first `make agents-token` (from the host clone) after
`make vm-delete vm-create` writes the identity file and applies it to the VM's git, so the agents commit
as the App's bot with no further step. To check, in the VM: `git -C <repo> var GIT_AUTHOR_IDENT` in a
workspace repository prints the identity a commit there uses, the bot's name and noreply address,
`<bot user id>+shift2eu-agents[bot]@users.noreply.github.com`, then a date (a repository's own
`user.email` would win over the global one, which `git config --global` alone would not show).

**Rolling back**: stop the refresh first, and do not start it again from the host clone: it mints again
whenever the token in the VM is within 20 minutes
of expiring, so up to 40 minutes after a rollback it would quietly write a new one, and every mint it makes
sets the bot's git identity again, undoing `make vm-init`. Only once the refresh is stopped, delete the
token file in the VM; delete the identity file too and run `make vm-init` to commit as yourself again.
Since milestone 3 the agents have no other GitHub credential, so without the token file `gh` and
`git push` fail in the VM (and `make vm-init`'s `gh` step with them) until a token file is back:

```bash
kill "$(cat ~/.local/state/shift2eu/agents-token-refresh-shift2eu.lock/pid)"
limactl shell --workdir / shift2eu sh -c 'rm -f "$HOME/.config/shift2eu/agents-app-token" "$HOME/.config/shift2eu/agents-app-identity"'
make vm-init
```

### The gate

After the switch, `Protect main` lets a change reach `main` only with **your approval and your merge**,
the single human gate (PRD #161). `.github/CODEOWNERS` names you as the owner of every path (`*`), itself
and `.github/` included, and `Protect main` requires:

| Rule | Setting |
|---|---|
| Required approvals | at least 1 |
| Require review from Code Owners (`require_code_owner_review`) | on: only your approval counts; one from the agents App, the AI reviewer or any other account does not |
| Dismiss stale approvals on push (`dismiss_stale_reviews_on_push`) | on: a push after your approval needs a new one |
| Require approval of the most recent push (`require_last_push_approval`) | on: whoever pushed last cannot approve it |
| Bypass list | empty: no App, bot, role or token gets past the rules, Renovate's and release-please's pull requests included |

Because of the last push rule, **nobody pushes to an agent's branch by hand after the switch**: a push of
yours would need another approver, and there is none. Ask the agents for the change instead. The AI
reviewer (milestone 4) comments and may request changes; its findings are advice you weigh, its change
request is yours to dismiss, and it never approves.

**The gate check**, `make gate-check` (`dev/protect-main-check.sh`), reads the repository
`timothymamo/shift2eu` (spelled out, so a fork's remote or `GH_REPO` cannot change what it checks), its
`Protect main` ruleset, `.github/CODEOWNERS` on `main` and GitHub's CODEOWNERS errors for that file, and
prints one line per check: `ok:`, `gap:`, or `unverified:` for what this token may not read. A gap is
`Protect main` not enforced, not targeting branches, not including `main` or excluding it, an exclude
pattern it cannot read (one with `[`, `]`, `{`, `}` or `\`, which GitHub may match against `main`:
`gap: Protect main cannot read exclude pattern <pattern>, so it may exclude main`), without a pull
request rule, with fewer than 1 required approval or one of the three rules above off, a bypass actor, a
bypass list missing on a repository admin's token, a token that may bypass `Protect main`
(`current_user_can_bypass` other than `never`), or a CODEOWNERS file on `main` without rules, without a
`*` rule, with a rule that has no owner, with an owner other than you, or with errors GitHub reports. The
`release-tag` skill runs it in its step 2; you can run it whenever you like. Its exit status:

| Exit | Means |
|---|---|
| 0 | The gate holds |
| 1 | The gate has gaps: the release-tag skill stops the release |
| 2 | It could not check: a `gh api` read failed (no access), not exactly one ruleset is named `Protect main`, an answer it did not expect, or an argument (it takes none) |
| 3 | No gap, but a check is `unverified:`: run `make gate-check` yourself, with your own admin token |

GitHub returns a ruleset's bypass list only to a token that may change the ruleset, which the agents App
never gets (ADR 0031, d): GitHub leaves the list out or returns it empty, and either proves nothing. The
check therefore reads the repository's `permissions.admin` first, and on a token that is not a repository
admin it prints `unverified: bypass_actors cannot be verified with this token (not a repository admin):
run make gate-check as the maintainer` instead of a verdict. That part is yours: run `make gate-check` from
the host clone, logged in to `gh` as yourself with an admin token (`gh auth login` on the Mac), as runbook
step 5 does. A fine-grained personal token reports your role on the repository in `permissions.admin`, not
what the token itself may do: without Administration on the token, GitHub leaves the bypass list out and
the check reports `gap: bypass_actors is missing although this token is a repository admin`. A token that
returned `[]` without being able to see the bypass actors would make the check pass falsely, so run the
gate check only with `gh auth login`'s token, whose answer runbook step 5 checks once by hand.

A gap always fails the check: the mode that reported gaps without failing a release before the switch-over
is gone (PRD #161 milestone 3), and its old flag is refused like any other argument (exit 2).

### The switch-over runbook

Agreed with the maintainer in PRD #161. Each step ends with a check; stop at any step and the ones
before it still hold. Rolling back is under each step.

1. **Create the agents App** as [The agents App](#the-agents-app) says (Creating the App), with its key
   and IDs in the host clone's `.envrc`. Nothing changes yet.
   *Check:* the installation's settings page says "Only select repositories" with only this one, and its
   permissions are exactly the table's (no Administration, Actions read).
   *Roll back:* delete the App.
2. **Merge milestone 1**, still opened under your personal token.
   *Check:* after `git -C ~/src/shift2eu-host pull --ff-only`, `make -n agents-token` in the host clone
   prints its commands instead of "No rule to make target".
   *Roll back:* nothing is enforced; until milestone 3, without a token file the VM went on with your
   personal token, `GITHUB_TOKEN`.
3. **Switch the agents to the App.** Give a VM created before milestone 1 the gh wrapper
   (`make vm-gh-wrapper`, or `make vm-delete vm-create`). From the host clone, run `make agents-token`,
   then `make agents-token-refresh-start`; the mint sets the bot as the VM's git identity, so the agents
   commit as the bot (in the VM, `git -C <repo> var GIT_AUTHOR_IDENT` in a workspace repository prints
   its noreply address), and restart
   the deck **between tasks**. Have an agent open a test pull request, then open
   milestone 2's, the first the App opens, and approve and merge it.
   *Check* on the test pull request `<n>`:
   - `gh pr view <n> --json author --jq '.author.is_bot, .author.login'` prints `true` and
     `app/shift2eu-agents`;
   - `gh pr view <n> --json commits --jq '.commits[].authors[].email'` prints only
     `<bot user id>+shift2eu-agents[bot]@users.noreply.github.com`, with the id of the bot user
     (`gh api users/shift2eu-agents%5Bbot%5D --jq .id`), not the App ID;
   - in the VM, each call of ADR 0031 (d)'s table works with the App token, and
     `gh api repos/timothymamo/shift2eu/rulesets/<id> --jq '.bypass_actors'` gives `null` or `[]`: the
     App cannot read the bypass lists;
   - `gh api repos/timothymamo/shift2eu/commits/main --jq .commit.verification.verified` prints `true`,
     and no ruleset holds a `required_signatures` rule.
   *Roll back:* as [The agents App](#the-agents-app) says (Rolling back): stop the refresh first (a
   running one sets the bot's git identity again at every mint), then delete the token and identity
   files in the VM, `make vm-init`.
4. **Close out your own pull requests.** Merge or close every open pull request authored by you: once
   step 5 is in place, with no bypass, nobody can approve one. Release-please's and Renovate's are not
   affected.
   *Check:* `gh pr list --state open --author timothymamo` prints nothing.
   *Roll back:* nothing to undo.
5. **Turn on the gate.** In the repository's settings, edit `Protect main`: 1 required approval, require
   review from Code Owners, dismiss stale pull request approvals when new commits are pushed, require
   approval of the most recent reviewable push. Leave the bypass list empty.
   *Check:* from the host clone (`git -C ~/src/shift2eu-host pull --ff-only`), logged in to `gh` as
   yourself with an admin token, `make gate-check` exits 0 and prints no `gap:` and no
   `unverified:` line (an exit of 3 means the token is not a repository admin: `gh auth login` and run it
   again). Once, by hand, with that same `gh auth login` token: the ruleset id from
   `gh api repos/timothymamo/shift2eu/rulesets --jq '.[] | select(.name == "Protect main") | .id'`, then
   `gh api repos/timothymamo/shift2eu/rulesets/<id> --jq 'has("bypass_actors"), .bypass_actors'` prints
   `true` and `[]`: the field is there and empty, so this token sees the bypass actors. Run the gate check
   only with this token. An open agent pull request shows "Review required" (`gh pr view <n> --json reviewDecision`
   gives `REVIEW_REQUIRED`) until you approve it.
   *Roll back:* put the `pull_request` rule of `Protect main` back as it was before this step, turning
   off all four settings: required approvals 0, no review from Code Owners, no dismissal of stale
   approvals, no approval of the most recent reviewable push. *Check:* `make gate-check`
   reports those review gaps again and exits 1, and an open agent pull request's
   `reviewDecision` (`gh pr view <n> --json reviewDecision`) is no longer `REVIEW_REQUIRED`.
6. **Remove your token from the VM** (milestone 3, approved and merged by you: the first change through
   the gate). Once it is merged, the default `VM_ENV` no longer names `GITHUB_TOKEN`, your personal
   GitHub token, and the gh wrapper has no fallback to it. Then, on the Mac:
   - delete the `GITHUB_TOKEN` line from the `.envrc` above the workspace (`direnv allow` there again),
     and `GITHUB_TOKEN` from any `VM_ENV` override you keep (a `VM_ENV` exported in a `.envrc`, or a
     habit of `make vm-env VM_ENV=...`);
   - run `make vm-env` with the default `VM_ENV`: it replaces the VM's whole `daemon.env`, so the old
     `GITHUB_TOKEN` line is gone (it says so: `vm-env: warning: removed GITHUB_TOKEN ...`); then
     `make deck-restart` between tasks;
   - revoke the personal token that `GITHUB_TOKEN` held on GitHub
     ([personal access tokens](https://github.com/settings/personal-access-tokens)).

   *Check:* the token no longer appears on GitHub's list of your personal access tokens; `make vm-init`'s
   `gh` step still passes, on the App token; and from the host clone, as yourself with an admin token,
   `make gate-check` exits 0 (it has no transition flag any more: a gap always fails it).
   *Roll back:* revert milestone 3, which brings the fallback and the `VM_ENV` entry back, create a new
   personal token limited to this repository, put it back in the `.envrc` above the workspace and run
   `make vm-env`.
7. **Add the AI reviewer** (milestone 4): create the reviewer App (Pull requests write, Contents read,
   this repository only), the `ai-review` environment (deployment branches: `main` only) and its secrets,
   then approve and merge milestone 4.
   *Check:* `gh api repos/timothymamo/shift2eu/environments/ai-review/deployment-branch-policies --jq '.branch_policies[] | .type + " " + .name'`
   prints only `branch main`; on the next pull request,
   `gh api repos/timothymamo/shift2eu/pulls/<n>/reviews --jq '.[] | .user.login + " " + .state'` shows
   the reviewer's review as `COMMENTED` or `CHANGES_REQUESTED`, never `APPROVED`, and an approval by
   anyone but you leaves `reviewDecision` short of `APPROVED`.
   *Roll back:* disable the review workflow; it is never a required check.

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
/usr/local/libexec/shift2eu/gh --version          # the ghVersion in dev/lima.yaml (/usr/local/bin/gh is the wrapper, ADR 0031)
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
