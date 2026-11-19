# 0031. Agents open pull requests as a GitHub App, with one-hour tokens minted on the Mac

- Status: Accepted
- Date: 2026-11-14

> **Update (milestone 3), 2026-11-18.** PRD #161 milestone 3 removed `GITHUB_TOKEN` from `VM_ENV` and
> removed the wrapper's fallback to it: without a valid App token file, `gh` and `git push` in the VM fail.
> `make vm-env-check` (thus `make vm-env` and `make vm-create`) now refuses a `VM_ENV` that names a GitHub
> token variable (`GITHUB_TOKEN`, `GH_TOKEN`, `GH_ENTERPRISE_TOKEN`, `GITHUB_ENTERPRISE_TOKEN`), a merge
> drops a stale line of theirs from the VM's `daemon.env`, and the wrapper unsets all but `GH_TOKEN` before
> it runs the real gh. The decision text below is kept as it was decided; where it describes the fallback
> or `GITHUB_TOKEN` in `VM_ENV` as current, this update supersedes it.

> **Update (issue #229), 2026-11-18.** Open question (1) a) is superseded in part by
> [ADR 0034](0034-run-the-agents-app-token-refresh-as-a-launchdaemon.md): a LaunchDaemon per VM instance
> (`make agents-token-refresh-install`) now runs the refresh at boot, with nobody logged in, and
> `make agents-token-refresh-start` warns and starts nothing while it is loaded. Where a) says launchd lost,
> ADR 0034 decides otherwise; the hand-started loop stays as the fallback.

> **Update (issue #227 part 1), 2026-11-19.** The first open follow-up in Consequences is done: **every
> host target runs from the host clone**, not only the token targets. The host targets are every target
> that runs on the Mac as the maintainer: the `vm-*` targets, `deck`, `deck-restart`, `gate-check` and the
> `agents-token*` targets (the Makefile's `HOST_TARGETS`; `test/devenv` keeps the list complete against
> every recipe that calls `limactl`, `launchctl`, `sudo`, `gh`, `curl`, `openssl` or `plutil`, runs a script of
> `dev/` that works on the Mac, or runs a host target as a sub-make).
>
> - **`WORKSPACE_REPO`.** A new make variable, the absolute path of the repository's checkout inside the
>   workspace, set in the host clone's `.envrc` (or on the command line). The host targets reach the
>   workspace only through it, instead of `CURDIR` and the paths derived from the checkout make runs in:
>   `deck`, `vm-shell`, `vm-claude-settings` and `vm-init` open the VM at it, `vm-init` gets it and its
>   parent, and `vm-up` mounts its parent, the workspace, and checks that for stray entries. The VM_ENV
>   credentials now come from the host clone's `.envrc` too, so no `.envrc` in or above the workspace
>   holds a credential any more.
> - **The refusal.** make refuses a host target while it reads the Makefile, keyed on `MAKECMDGOALS`
>   (GNU make 3.81's `$(error)`, so before any prerequisite, sub-make or recipe line, even under `-i`,
>   `-k` or `-n`), when `WORKSPACE_REPO` is unset, holds a character other than letters, digits and
>   `. _ - / + @ # =` (an allow list: no whitespace, quote, `$`, `\`, `%`, no `:`, which Lima splits a
>   `--mount-only` value on, and no `,` or `~`, as `limactl`'s `--mount-only` is a pflag StringSlice that
>   splits on commas and expands a leading `~` of every part, so `/Users/x/ws,~/shift2eu` would mount the
>   home directory writable), or is not absolute, or when the checkout make runs in is the workspace,
>   lies inside it or holds it; no message shows a value that fails these checks. It refuses any run in
>   which `MAKECMDGOALS` or `.DEFAULT_GOAL` comes from the command line or the environment, which would
>   hide the real goals, and names that variable. Its message names the target, the host clone and
>   `docs/dev-environment.md`.
>   The targets the agents run in the VM never read `WORKSPACE_REPO` and are not refused. `vm-up` also
>   refuses, before any `limactl` call, a `WORKSPACE_REPO` that is no directory holding `.git` and a
>   workspace it cannot list, and matches the repository's name in the listing literally.
> - **Only the credentials a process needs.** The host targets that open a shell in the VM run `limactl`
>   without the App's three variables (`env -u`), and the background refresher runs without the default
>   `VM_ENV` credentials (`CLAUDE_CODE_OAUTH_TOKEN`, `DEEPSEEK_API_KEY`).
> - **A guard, not a boundary.** The refusal protects the maintainer from a mistake (a `make deck` typed
>   in the wrong directory). It is no security boundary: the workspace's `Makefile` is agent-editable, so
>   a make run there runs whatever that file says. The protection is that the maintainer never runs
>   `make` or `git` in the workspace on the Mac (`docs/dev-environment.md`, The host clone).
> - **`vm-up` no longer sets git hooks.** Its `git-hooks` prerequisite would set the host clone's hooks
>   from there, and running git in the workspace from the Mac is what this forbids. `make vm-init` turns
>   the workspace checkout's hooks on inside the VM (`git -C <WORKSPACE_REPO> config core.hooksPath
>   .githooks`); the setting lives in the mounted `.git/config`.
> - `make gate-check` is a host target, so the release-tag skill runs `dev/protect-main-check.sh` in the
>   VM and asks the maintainer for `make gate-check` from the host clone; a trusted fresh clone of `main`
>   on a machine with no workspace runs `dev/protect-main-check.sh` directly. `make deck`'s warning about
>   the App variables in "the workspace's shell" is gone: from the host clone they are the normal case,
>   and `make deck` hands them to no process in the VM.
>
> **What part 2 leaves open.** The App's key is still a file the maintainer's uid can read, so code that
> runs as the maintainer anywhere (agent-editable code run on the Mac by mistake, a git-aware prompt or an
> editor in the workspace, a tampered host clone) can read it silently; the Consequences below still hold
> for that. The maintainer ruled out prompting stores for the unattended Mac Mini (issue #227, comment of
> 2026-11-15): the option that fits is a separate macOS account that owns the key and runs the refresher,
> composing with [ADR 0034](0034-run-the-agents-app-token-refresh-as-a-launchdaemon.md) as the
> LaunchDaemon's `UserName`. As the Lima instance lives in the maintainer's `~/.lima`, that account would
> write the token into the VM over its SSH port with a key of its own, a change to
> `dev/agents-app-token.sh`. Where the decision or the Consequences below describe host targets run from
> the workspace, `make deck` typed by hand, or credentials in the `.envrc` above the workspace as current,
> this update supersedes them.

> **Update (issue #227 part 2), 2026-11-19.** This supersedes "What part 2 leaves open" above. **The App's
> key moves to a separate macOS account, `_shift2euagents`, which owns it, runs the refresh as the
> LaunchDaemon's `UserName` ([ADR 0034](0034-run-the-agents-app-token-refresh-as-a-launchdaemon.md)) from
> a clone of its own, and writes the token into the VM over the VM's SSH port with an SSH key of its own,
> authorised in the VM only to run one fixed writer.** The maintainer's uid can no longer read the key
> without a `sudo` password prompt. Today's mode, the refresh as the maintainer through `limactl`, stays
> as the **single-account mode**, the fallback until the maintainer has switched (6 below).
>
> 1. **The account.** `_shift2euagents`: a hidden, standard (not admin) account with no password and no
>    login shell. The underscore marks a role account, as macOS's own daemons have (`_www`), and keeps it
>    out of the login window with its uid below 500. Its shell is `/usr/bin/false`, so `sudo -u
>    _shift2euagents -i` (which ADR 0034 c named) cannot work: every command run as it is
>    `sudo -u _shift2euagents -H /usr/bin/env -i HOME=/Users/_shift2euagents
>    PATH=/usr/bin:/bin:/usr/sbin:/sbin <tool by full path>`, run from `/`, which runs the command directly,
>    never a login shell, with `HOME` set to its home (`-H`; macOS's sudoers keeps the caller's `HOME`
>    otherwise), none of the maintainer's environment (`env -i`: no `PATH`, `GIT_*`, pager or `DYLD_*` of
>    the maintainer's reaches it) and no tool the maintainer's uid owns (the full path; Homebrew's `git`
>    is the maintainer's). The make targets' own `sudo -u <account>` calls take the same form. Created once by
>    the maintainer with `sudo` (`docs/dev-environment.md` carries the runbook; no agent ever runs it,
>    and no make target creates or deletes an account): a group and a user of the same name and id, the
>    first free id from 450 to 499 (`dscl . -list /Users UniqueID`, `dscl . -list /Groups
>    PrimaryGroupID`), `UserShell /usr/bin/false`, `Password '*'`, `IsHidden 1`, `RealName "shift2eu
>    agents App"`, `NFSHomeDirectory /Users/_shift2euagents`, created with `dscl . -create`, then the
>    home created by `sudo mkdir -m 0700 /Users/_shift2euagents` and `sudo chown
>    _shift2euagents:_shift2euagents` on it. `sysadminctl -addUser` is not used: it creates a login
>    account with a password and a shell. The make targets take the name from `SHIFT2EU_AGENTS_ACCOUNT`
>    (2 below) and derive its home as `/Users/<account>`; they refuse a name that is empty, does not
>    begin with a letter or `_` (so no `-`, `.`, `..` or digit first), holds anything but letters, digits,
>    `.`, `_` and `-`, or is `root` or the maintainer's own (`id -un`). They check the account's kind too,
>    before any `sudo` (issue #227 part 2, the security audit's second pass): they refuse unless
>    `dscl . -read /Users/<account> UniqueID` reads a `UniqueID` from 1 to 499 and `dseditgroup -o
>    checkmember -m <account> admin` does not report it a member of `admin`, naming the check and
>    `SHIFT2EU_AGENTS_ACCOUNT`, never a value: a login account or an admin would put the key back in the
>    maintainer's silent reach. The runbook's passing check of the account confirms the same by hand.
> 2. **Where everything lives, and the new trust boundary.** Everything that holds or runs with the key
>    is owned by the account, inside its home, out of reach of the maintainer's uid:
>
>    | Path | What | Mode |
>    |---|---|---|
>    | `/Users/_shift2euagents` | its home | 0700 |
>    | `/Users/_shift2euagents/shift2eu-host` | **its host clone**: the code the job runs | owned by it |
>    | `/Users/_shift2euagents/.config/shift2eu/` | its secrets directory | 0700 |
>    | `.../agents-app.pem` | the App's private key (`SHIFT2EU_AGENTS_APP_KEY_FILE`) | 0600 |
>    | `.../agents-vm-ssh-key`, `.../agents-vm-ssh-key.pub` | its SSH key to the VM, ed25519, no passphrase | 0600, 0644 |
>    | `.../known-hosts-<instance>` | the VM's pinned host key, one file per VM instance | 0600 |
>    | `/Users/_shift2euagents/.local/state/shift2eu/` | the job's lock and log (ADR 0034's paths, under its `HOME`) | 0700, 0600 |
>
>    The paths under `.config/shift2eu` are fixed: in account mode the make targets derive them from the
>    account's home and pass them to the job, and set no `SHIFT2EU_AGENTS_APP_KEY_FILE` of the
>    maintainer's. **The boundary is now the account's files, not the maintainer's host clone.** Whoever
>    can write the account's clone, its secrets directory or any directory above them controls what runs
>    with the key, so the account's clone replaces the maintainer's: the maintainer's host clone keeps
>    running every other host target (the `vm-*` targets, `deck`, `gate-check`) and renders and installs
>    the plist under `sudo`, but no code of it ever runs with the key. **Trusted path rule:** a file and
>    each directory above it, up to `/`, is owned by the running uid or by root, and has no group or other
>    write bit, except a root-owned directory with the sticky bit (`/tmp`), where no one else can rename an
>    entry, and carries no ACL with an `allow` entry: a Mac's `ls -ldn` ends such an entry's mode with `+`,
>    or with `@` when the entry also carries extended attributes, and under either the rule reads the entries with `ls -lde` and refuses any `allow`, which could grant another
>    account what the mode bits deny, while a deny-only ACL (`group:everyone deny delete`, on every macOS
>    home) grants nothing and is accepted. The account mode checks it with `ls -ldn` (a stock Mac has no
>    GNU `stat`) and `id -u`, on the
>    App's key, the SSH key, the known-hosts file and the script itself (`$0`), at install time (as the
>    account, by `dev/agents-token-launchd.sh plist`) and again at every start of the refresh (by
>    `dev/agents-app-token.sh`, before it mints), and refuses with the path that fails and why ("owned by
>    uid <n>", "group-writable", "other-writable", "an ACL with an allow entry"), never a file's content. The key and the SSH key must
>    also have no group or other bit at all (0600 or 0400). **Updating the account's clone** is the
>    maintainer's step, under `sudo`, from GitHub only:
>    `cd / && sudo -u _shift2euagents -H /usr/bin/env -i HOME=/Users/_shift2euagents
>    PATH=/usr/bin:/bin:/usr/sbin:/sbin /usr/bin/git -c core.pager=cat -C /Users/_shift2euagents/shift2eu-host
>    pull --ff-only`, then `sudo launchctl kickstart -k system/<label>` to run the pulled code now. It is
>    cloned once the same way (`cd / && sudo -u _shift2euagents -H /usr/bin/env -i
>    HOME=/Users/_shift2euagents PATH=/usr/bin:/bin:/usr/sbin:/sbin /usr/bin/git -c core.pager=cat clone
>    https://github.com/timothymamo/shift2eu.git /Users/_shift2euagents/shift2eu-host`). `/usr/bin/git` is
>    a shim for the developer directory `xcode-select -p` names, which the runbook checks is root's. git runs as the account there, with the account's own (empty)
>    git config and the clone's own hooks, which the maintainer's uid cannot write.
>    **The key** is put there by the maintainer: a **new** private key generated on the App's page, moved
>    in with `sudo install -o _shift2euagents -g _shift2euagents -m 0600 <download>
>    /Users/_shift2euagents/.config/shift2eu/agents-app.pem`, the download then removed, and **every
>    older key deleted on the App's page**: a key the maintainer's uid ever held must be revoked, or
>    moving it gains nothing. The download sits in the maintainer's `~/Downloads` for that moment; that
>    window is accepted. For that moment the download is in the maintainer's reach, usually on another
>    machine too, carried to the Mini as a 0600 temporary file and removed on both (local Time Machine
>    snapshots taken in the window deleted; rotating the key closes it for good).
> 3. **Transport: SSH, not limactl.** The account cannot run `limactl` against the maintainer's
>    `~/.lima`, so it connects to the VM's forwarded SSH port on `127.0.0.1` itself. Four variables choose
>    and configure it in `dev/agents-app-token.sh`: `SHIFT2EU_AGENTS_VM_SSH_PORT`,
>    `SHIFT2EU_AGENTS_VM_SSH_USER` (the VM user, Lima's default the maintainer's Mac user name),
>    `SHIFT2EU_AGENTS_VM_SSH_KEY` and `SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS` (5 below). `vm_send` then runs,
>    in this order, with nothing else on its command line:
>
>    ```sh
>    ssh -F /dev/null -T -p <port> -l <user> -i <key> \
>      -o BatchMode=yes -o IdentitiesOnly=yes -o IdentityAgent=none \
>      -o StrictHostKeyChecking=yes -o UserKnownHostsFile=<known hosts> -o GlobalKnownHostsFile=/dev/null \
>      -o HostKeyAlias=shift2eu-vm-<instance> -o CheckHostIP=no -o UpdateHostKeys=no \
>      -o ControlMaster=no -o ControlPath=none -o ForwardAgent=no -o ForwardX11=no \
>      -o ClearAllForwardings=yes -o PermitLocalCommand=no \
>      -o ConnectTimeout=15 -o ServerAliveInterval=15 -o ServerAliveCountMax=2 -o LogLevel=ERROR \
>      127.0.0.1 agents-token-write
>    ```
>
>    `-F /dev/null` reads no user or system `ssh_config`; `HostKeyAlias` pins the host key under the
>    instance's name, not `[127.0.0.1]:<port>`, so a port change needs no new pin, and each instance's
>    key stays its own. The token, expiry, name and email go on ssh's standard input, four lines exactly
>    as on `limactl`'s today, never in an argument; `agents-token-write` is a placeholder the VM ignores
>    (the forced command, 4 below, runs instead). The test hook for the binary is `SHIFT2EU_AGENTS_SSH`
>    (default `ssh`), as `LIMACTL` is for limactl. The writer's exit statuses carry through ssh unchanged:
>    3 is permanent and 4 transient, as today; **255** is ssh's own failure (the VM down, the port closed,
>    the key not authorised, a host key that does not match) and is transient, with one line that names
>    every cause without a value: "could not write the <what> into the VM <instance> over SSH
>    (127.0.0.1:<port>, ssh exit 255): is it running (make vm-up)? If ssh said `Host key verification
>    failed` above, the VM's host key changed (a recreated VM): pin it again with make
>    vm-agents-token-ssh; if it said `Permission denied`, the VM does not authorise the account's key: run
>    make vm-agents-token-ssh". ssh's own standard error goes to the log as it is. A host key mismatch
>    aborts before ssh authenticates or opens a session, so nothing is sent to whatever answered.
>    `check_vm` reads the same `clock=` and `gh-wrapper=missing` lines. In SSH mode the script runs no
>    `limactl` at all, and `LIMA_INSTANCE` names only the alias, the lock, the log and the messages.
>    **The port must be fixed.** Lima picks a random SSH port at every start unless the instance sets
>    `ssh.localPort`, so the maintainer pins it once per instance while the VM is stopped:
>    `limactl stop <instance>`, `limactl edit --set '.ssh.localPort = 48022' <instance>`,
>    `limactl start <instance>` (48022 for the first instance, 48023 for the next, and so on: below
>    macOS's ephemeral range, 49152 and up, so no outgoing connection takes it while the VM is down).
>    `make vm-agents-token-ssh` refuses unless `limactl list --format '{{.SSHLocalPort}}' <instance>`
>    prints exactly `SHIFT2EU_AGENTS_VM_SSH_PORT`.
> 4. **The VM side: one root-owned writer, one key that can run only it.** The writer is today's
>    `vm_write` text, installed as **`/usr/local/libexec/shift2eu/agents-token-write`** (root, mode 0755),
>    beside the real gh. Its single source stays `dev/agents-app-token.sh`: `dev/agents-app-token.sh
>    vm-writer` prints `#!/bin/sh`, a line, then `vm_write` byte for byte, and nothing else (the tests
>    compare the two). The account's key is authorised in a root-owned file of its own, never in
>    `~/.ssh/authorized_keys`, which cloud-init and Lima manage: the drop-in
>    **`/etc/ssh/sshd_config.d/50-shift2eu-agents-token.conf`** holds the single line
>    `AuthorizedKeysFile .ssh/authorized_keys .ssh/authorized_keys2 /etc/ssh/shift2eu-agents-token/%u.keys`
>    (Ubuntu's `sshd_config` includes the drop-ins first, and nothing else sets the keyword, so Lima's own
>    key still works), and **`/etc/ssh/shift2eu-agents-token/<vm user>.keys`** (root, 0644, in a root
>    0755 directory; `%u` makes the file apply to that user only, so the key cannot log in as root or
>    anyone else) holds exactly one line:
>
>    ```
>    restrict,command="/usr/local/libexec/shift2eu/agents-token-write /usr/local/libexec/shift2eu/gh" ssh-ed25519 <base64> shift2eu-agents-token
>    ```
>
>    `restrict` turns off port, agent and X11 forwarding, the pty and `~/.ssh/rc`; the forced command runs
>    whatever the client asked for, with the real gh's path as the writer's `$1` as today. No `from=`:
>    Lima's port forward is relayed by its host agent, so every connection from the Mac, the
>    maintainer's `limactl shell` included, arrives from the same user-mode network address; a `from=`
>    would tell nothing apart, and is not used. **`make vm-agents-token-ssh`**, a new host target run by
>    the maintainer from the maintainer's host clone (in `HOST_TARGETS`), sets it up and is safe to run
>    again (after `make vm-delete vm-create`, after a new SSH key). It needs `SHIFT2EU_AGENTS_ACCOUNT` and
>    `SHIFT2EU_AGENTS_VM_SSH_PORT`, and in this order, stopping at the first failure:
>    1. checks the port (3 above);
>    2. reads the account's public key with `sudo -u <account> -H /bin/cat
>       /Users/<account>/.config/shift2eu/agents-vm-ssh-key.pub` (printed with a `+ ` before it, like
>       every `sudo` the targets run) and refuses anything but one line `ssh-ed25519 <base64>` with an
>       optional comment of letters, digits and `@ . _ -`: no options, quotes, commas or second line can
>       reach the keys file. The comment is replaced with `shift2eu-agents-token`;
>    3. generates, with `sh dev/vm-agents-token-ssh.sh <writer file> <key line> <vm user>` (the writer
>       from `dev/agents-app-token.sh vm-writer`, in a temporary file), a script whose last line is the
>       call `vm_agents_token_ssh`, sends it only when it is whole, over `limactl shell --workdir /
>       <instance> sh -s`'s standard input, exactly as `make vm-gh-wrapper` does, and fails unless the
>       VM's output holds the line `vm-agents-token-ssh: done`. In the VM the script, with `sudo`,
>       installs the writer as `/usr/local/libexec/shift2eu/.agents-token-write.new` after `sh -n`, then
>       `mv`s it into place; writes the keys file and the drop-in the same way (a `.new` beside each,
>       then `mv`); runs `sshd -t` and, only when that passes, `systemctl try-reload-or-restart
>       ssh.service` (implementation note below; on a failing
>       `sshd -t` it removes the drop-in again and fails); and prints the keys line it wrote and the VM's
>       `/etc/ssh/ssh_host_ed25519_key.pub` as `host-key=ssh-ed25519 <base64>`;
>    4. pins the host key: writes `shift2eu-vm-<instance> ssh-ed25519 <base64>` to
>       `/Users/<account>/.config/shift2eu/known-hosts-<instance>` as the account, through `sudo -u
>       <account> -H /bin/sh -c 'umask 077 && cat >"$1.new" && mv -f "$1.new" "$1"' pin <path>`, the line
>       on standard input. The host key is read through `limactl shell`, which reaches the real VM, so the
>       pin needs no trust on first use;
>    5. prints the keys line, the pin, and the next step (`make agents-token-refresh-install`, or
>       `sudo launchctl kickstart -k system/<label>` when the job is loaded).
>
>    **`make vm-agents-token-ssh-uninstall`** removes the drop-in, the keys file and the writer (with
>    `sudo`, in the VM, over `limactl shell`'s standard input, the same whole-script check and a
>    `vm-agents-token-ssh-uninstall: done` line; its script comes from `sh dev/vm-agents-token-ssh.sh
>    --uninstall <vm user>`), reloads sshd, and leaves the account's files alone.
>    Residual: the agents run as the VM user, who has passwordless `sudo` (ADR 0017), so they can change
>    the writer, the keys file or sshd. That gains them nothing they lack: the token lands in their own
>    home either way, and the key never enters the VM.
> 5. **Mode choice, configuration and refusals.** `dev/agents-app-token.sh` is in **SSH mode** when all
>    four `SHIFT2EU_AGENTS_VM_SSH_*` variables are set and not empty, in **limactl mode** (today's, the
>    single-account mode) when none is, and refuses anything between, before it mints, as a permanent
>    failure (status 1, with `agents App token refresh stopped` under `refresh`): "SSH mode needs all of
>    SHIFT2EU_AGENTS_VM_SSH_PORT, SHIFT2EU_AGENTS_VM_SSH_USER, SHIFT2EU_AGENTS_VM_SSH_KEY and
>    SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS; not set: <the names>". In SSH mode it also refuses, the same way and
>    naming the variable, never its value when it fails: a port that is not 1 to 65535 in digits; a user
>    that is not letters, digits, `.`, `_` and `-` beginning with a letter, digit or `_`; a key or
>    known-hosts path that is not absolute, is missing, a symlink or not a regular file, is not owned by
>    the running uid, or fails the trusted path rule (2 above); a key with any group or other bit; and
>    the App's key and the script itself failing that rule. The make side has its own switch:
>    **`SHIFT2EU_AGENTS_ACCOUNT`**, set in the maintainer's host clone's `.envrc` with
>    `SHIFT2EU_AGENTS_VM_SSH_PORT` (and `SHIFT2EU_AGENTS_VM_SSH_USER` when the VM user is not the
>    maintainer's `id -un`, its default), turns on the **account mode** of the install targets (ADR 0034,
>    update of 2026-11-19), which derive the key, SSH key and known-hosts paths from the account's home.
>    In account mode `make agents-token`, `make agents-token-refresh` and
>    `make agents-token-refresh-start` refuse before they run anything: "the refresh runs as <account>,
>    which holds the App's key; the maintainer cannot mint: run sudo launchctl kickstart -k
>    system/<label> to mint now (docs/dev-environment.md)". A `SHIFT2EU_AGENTS_APP_KEY_FILE` still set
>    in account mode gets a warning from every token target: the account's key is the derived path, the
>    variable should go from `.envrc`, and the file it names should be deleted and its key deleted on the
>    App's page.
> 6. **Single-account mode stays as the fallback until the switch, then goes.** It is what runs today and
>    what a Mac without the account still needs, so this change keeps it and its tests unchanged. Once
>    the maintainer has switched the Mac Mini and the runbook's checks have passed across a reboot, a
>    follow-up issue removes it: limactl mode from `dev/agents-app-token.sh`, the maintainer-uid paths of
>    the install, and `make agents-token-refresh-start` with them, so the docs describe one way and no
>    `.envrc` names a key file again. Until then the docs present account mode as the way for the
>    unattended Mac and single-account mode as the fallback.
> 7. **What the maintainer's uid can and cannot still do.**
>    - It **cannot** read the key, the SSH key or the known-hosts file, write the account's clone or its
>      secrets directory, or signal the job: all are another uid's, in a 0700 home. It cannot change the
>      LaunchDaemon's plist, which is root's.
>    - It **can**, as an admin, with `sudo` and its password: a prompt the maintainer sees, not a silent
>      read. sudo's timestamp (5 minutes on macOS, per terminal) is the one silent window: code started in
>      the same terminal within 5 minutes of a `sudo` can `sudo` without a prompt. So the account-mode
>      targets end with `sudo -k`, on success and on failure, and the runbook says to run `sudo -k` after
>      any hand-typed `sudo` and never to leave a sudo-enabled terminal to agent-run code. Root, and any
>      process the maintainer runs under `sudo`, can of course read everything.
>    - It **can** reach the VM's SSH port, and its own Lima host agent relays every connection to it, but
>      sees only ciphertext: the session is end to end with the pinned host key. It could write a fake
>      token into the VM, or take the VM's host key and pose as the VM, but it controls the VM already,
>      where the agents hold every token anyway; neither gives it the key.
>    - It **can bind** `127.0.0.1:<port>` while the VM is down and answer the account's ssh. The host key
>      check fails before any authentication, so it receives no token and no signature it could replay;
>      the job logs ssh's `Host key verification failed` (exit 255, transient) until the VM is back.
>    - It **can** render a plist that points the job elsewhere: from a tampered maintainer's host clone at
>      install time. That plist reaches launchd only through the install's `sudo`, a prompt the maintainer
>      answers after the install printed the plist, its SHA-256 and the script it runs; the runbook's check
>      after install looks for `/Users/_shift2euagents/shift2eu-host/dev/agents-app-token.sh` in
>      `sudo launchctl print system/<label>`. Accepted.
>    - Backups: Time Machine keeps the account's files with their owners and modes, but a restore or a
>      mounted backup volume can expose them; the runbook excludes `/Users/_shift2euagents/.config/shift2eu`
>      with `sudo tmutil addexclusion -p`.
>    - The other credentials of the maintainer's host clone (`CLAUDE_CODE_OAUTH_TOKEN`, `DEEPSEEK_API_KEY`
>      in `VM_ENV`) stay in the maintainer's uid's reach: out of this issue's scope.
> 8. **Sleep, restart, VM down or recreated, port change.**
>    - **Sleep and wake:** unchanged (ADR 0031 a): the next check after a wake sees the expiry and mints;
>      an ssh that fails in the meantime is transient.
>    - **Reboot, power cut:** the LaunchDaemon starts as the account with nobody logged in, needs no
>      password and no keychain, and writes once the VM's sshd answers on the pinned port.
>    - **VM down:** ssh exits 255 (connection refused), transient, retried with the usual back-off.
>    - **VM recreated** (`make vm-delete vm-create`): new host keys, no writer, no keys file, and a random
>      port again. The job logs `Host key verification failed` (or `Connection refused`) and keeps
>      trying. Recovery: pin the port (3 above), then `make vm-agents-token-ssh`; the job's next try, at
>      most five checks later, works without a restart.
>    - **Port change:** set `SHIFT2EU_AGENTS_VM_SSH_PORT` in the host clone's `.envrc`, pin it in Lima,
>      run `make vm-agents-token-ssh` and `make agents-token-refresh-install` again (the port is in the
>      plist); the pin needs no change, thanks to `HostKeyAlias`.
>    - **A new account SSH key:** `cd / && sudo -u _shift2euagents -H /usr/bin/env -i
>      HOME=/Users/_shift2euagents PATH=/usr/bin:/bin:/usr/sbin:/sbin /usr/bin/ssh-keygen -t ed25519 -N '' -C
>      shift2eu-agents-token -f /Users/_shift2euagents/.config/shift2eu/agents-vm-ssh-key` (after
>      removing the old pair as the account), then `make vm-agents-token-ssh`.
> 9. **Back to single-account mode.** From the maintainer's host clone: `make
>    agents-token-refresh-uninstall` (it boots out the job whichever mode installed it); `make
>    vm-agents-token-ssh-uninstall`, while `SHIFT2EU_AGENTS_VM_SSH_USER` (which names the keys file) is
>    still in `.envrc`; generate a new key on the App's page and put it where
>    `SHIFT2EU_AGENTS_APP_KEY_FILE` names, outside the workspace, 0600, as before this update (never copy
>    the account's key out: delete it on the App's page once the new one works); remove
>    `SHIFT2EU_AGENTS_ACCOUNT` and the `SHIFT2EU_AGENTS_VM_SSH_*` variables from `.envrc`; then `make
>    agents-token-refresh-install` (or `make agents-token-refresh-start`). The account itself is removed by hand, with `sudo dscl .
>    -delete /Users/_shift2euagents`, `sudo dscl . -delete /Groups/_shift2euagents` and removing its home.
>
> **Implementation notes (same date).** Where the implementation settled what the points above left open:
>
> - sshd is reloaded with `systemctl try-reload-or-restart ssh.service`, not `reload`: under Ubuntu's
>   socket-activated ssh (`ssh.socket`) `ssh.service` can be inactive, where `reload` fails; the try form
>   reloads a running sshd and leaves an inactive one to the next connection, which reads the new
>   configuration. The runbook checks it on the real VM.
> - The account's public key is checked (4, step 2) by `dev/vm-agents-token-ssh.sh` itself, before it prints
>   anything, so the check runs before anything is sent or pinned.
> - Commands run as the account in the runbook name their tool by path (`/usr/bin/git`, `/usr/bin/ssh-keygen`,
>   `/usr/bin/ssh`): macOS's sudoers sets no `secure_path`, so `sudo` can keep the caller's `PATH`, where a bare
>   name could find Homebrew's, which the maintainer's uid owns.
> - The generator (ADR 0034, update of the same date, c, step 4) applies the trusted path rule to itself and,
>   when it is there, to the clone's `dev/agents-app-token.sh`; the token script applies it to itself at every
>   start in any case.
> - In SSH mode the refresh's stop line keeps naming `make agents-token-refresh-start` for the single-account
>   mode and adds `sudo launchctl kickstart -k system/<label>` for the account's job.
>
> **Alternatives considered for part 2.** A Keychain item or a password-manager read that prompts: ruled
> out by the maintainer for the unattended Mac Mini (issue #227, 2026-11-15), every mint after a reboot
> would wait for a click. The account calling `limactl` with `LIMA_HOME` pointed at the maintainer's
> `~/.lima`: it would need that directory readable and the instance's SSH key with it, and ties the account
> to Lima's private layout. The key in `~/.ssh/authorized_keys`: Lima and cloud-init manage that file, and
> it would let the key log in as any user whose file it reached. A shared group between the account and
> the maintainer: any group access gives the maintainer's uid what the account has. The account's clone
> kept in a shared place the maintainer can write (`/Users/Shared`): it fails the trusted path rule, which
> is the point of the account. The account running `git pull` itself on a schedule: it would move the
> trusted code with no human looking; pulling stays the maintainer's step.

## Context

The agents in the Lima VM push branches, open pull requests and file issues with the maintainer's own
fine-grained token (`GITHUB_TOKEN`, `make vm-env`). Every agent pull request is therefore the
maintainer's, and GitHub never lets an author approve their own pull request, so `Protect main` cannot
require an approval: anything holding a token with Contents and Pull requests write can open a pull
request and land it once the checks pass (PRD #161, Problem).

PRD #161 settles the direction (Decisions, 2026-11-14): the agents act as a GitHub App of the
maintainer's, the **agents App**, installed on this repository only; its private key stays on the Mac
(option B); its inputs live in a direnv `.envrc` on the Mac (the PRD first said the one above the
workspace; milestone 1's review moved them to a host clone outside the workspace, below); an AI reviewer, a second App, comments or requests changes but never approves; branch
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
that runs with it is the reviewed code, so the token targets run from the **host clone**: a second clone
of the repository on the Mac, outside the workspace and never mounted into the VM, updated only with
`git pull --ff-only` from GitHub. Anything run from the workspace instead can read every file the
maintainer can, and the agents can edit the workspace. The host clone keeps the key's path out of the
workspace's shells and the token targets on code the agents cannot edit; it does not put the key out of
reach of code run from the workspace on the Mac (see Consequences).

- **Inputs, on the Mac only.** `SHIFT2EU_AGENTS_APP_ID`, `SHIFT2EU_AGENTS_APP_INSTALLATION_ID` and
  `SHIFT2EU_AGENTS_APP_KEY_FILE` (the path of the PEM key, outside the workspace, a regular file, not a
  symlink, of mode 0600, which the script checks) come from the host clone's own direnv `.envrc`, outside
  the workspace, and never from the `.envrc` above the workspace, so a shell in the workspace does not
  have the key's path in its environment (code run from the workspace can still find it: Consequences).
  `make deck` warns when any `SHIFT2EU_AGENTS_APP_*` variable reaches it (and still opens the
  deck), and `make vm-env` refuses any such name in `VM_ENV`, so neither the key nor its path can be copied
  into the VM.
- **Minting** (`dev/agents-app-token.sh mint`, `make agents-token`). An RS256 JWT (`iat` now − 60 s,
  `exp` `iat` + 600 s, `iss` the App ID) signed with `openssl dgst -sha256 -sign`, then
  `POST /app/installations/<id>/access_tokens`. The JWT reaches `curl` only through `--config -` on its
  standard input, after `-q`, so no `~/.curlrc` can log or redirect it, and the response is parsed with
  `sed` (a stock Mac has no `jq`). The API URL is `https://api.github.com`; the tests' hook,
  `SHIFT2EU_AGENTS_GITHUB_API_URL` (a name of its own, as other tools set `GITHUB_API_URL`), is refused
  when it holds an `@`, a quote, a backslash or whitespace (each could send the JWT to another host or add
  a line to curl's config), or is plain http to anything but `127.0.0.1` or `localhost`. The slug comes from
  `GET /app`, never hard-coded, and the bot user's id from `GET /users/<slug>[bot]`, asked **after** the
  mint with the installation token just minted: GitHub refuses the App's JWT there, and an unauthenticated
  call would share the 60-an-hour limit of everything behind the Mac's address. The token is written into
  the VM as soon as it is minted, before that lookup, so a failed lookup never discards it; the identity
  file follows once a lookup works, and the identity is kept for the life of the process. The refresh
  retries a failed lookup at its next check with the token it holds, without minting another one; a lookup
  refused with a 4xx other than a rate limit stops it, and the token already written stays in the VM until
  it expires, within the hour.
- **In the VM**, written by a fixed `sh -c` script that `limactl shell` runs, its content on standard
  input, atomically (a temporary file beside each, then `mv`) under umask 077:
  `~/.config/shift2eu/agents-app-token` (the token, then its expiry in Unix seconds: the earlier of mint
  time + 3600 s − 300 s and `expires_at` − 300 s, so a VM clock a few minutes behind the Mac's still
  stops using a token before GitHub does) and `~/.config/shift2eu/agents-app-identity` (`<slug>[bot]`,
  then `<bot user id>+<slug>[bot]@users.noreply.github.com`), both mode 0600. The script refuses, before
  it writes anything, a `~/.config/shift2eu` that is a symlink or that resolves (`pwd -P`, as does
  `~/.config`) outside the VM user's real home, and a token or identity file that is a symlink or not a
  regular file. That keeps the token from being written through a symlink left by mistake; it is not a
  boundary against the VM user, who can read the token anyway and could swap a path between the check and
  the `mv`. It prints
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
  it, whole or not at all: the script is one function called on its last line, so a stream cut short runs
  nothing; it checks the wrapper with `sh -n` and for its final `exec` line, installs it as
  `/usr/local/bin/.gh.new` and `mv`s it into place, and ends with `vm-gh-wrapper: done`. The recipe
  generates the script first and sends it only when its last line is that call, so a generator that fails
  fails `make` before the VM is touched, and `make` also fails unless the VM's output, which it prints,
  holds `vm-gh-wrapper: done`, so a connection that drops after the stream arrived is no success. The git credential helper is `!/usr/local/bin/gh auth git-credential`, by absolute path
  (`gh auth setup-git` would name the real binary). On every call the wrapper reads the token file:
  absent, it runs the real gh unchanged; well formed, the VM user's own, mode 0600 and not expired, it runs
  the real gh with `GH_TOKEN` set to the token and `GITHUB_TOKEN` unset, arguments and standard input
  passed through; anything else, it fails without running gh and asks for the maintainer to run
  `make agents-token-refresh-start` in the host clone on the Mac, never printing the file's content, and never suggesting to
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

**a) How the refresh runs on the Mac: a background loop started from the host clone by
`make agents-token-refresh-start`, not launchd and not `make deck`.**
`dev/agents-app-token.sh refresh` (`make agents-token-refresh` in the foreground) mints at once, then every
`SHIFT2EU_AGENTS_TOKEN_CHECK_SECONDS` (60) compares the wall clock (`date +%s`) with the expiry of the last
token it wrote and mints again when fewer than `SHIFT2EU_AGENTS_TOKEN_MARGIN_SECONDS` (1200) remain. It
survives the Mac sleeping because it never sleeps for the token's lifetime: after a wake the next check, at
most a minute later, sees how little is left. A wake usually comes before the network does, and a long
sleep leaves no valid token at all, so the loop tells failures apart. A **transient** one (no answer from
GitHub, HTTP 408, 429 or 5xx, a 403 that is a rate limit, `limactl` failing or the VM not running) never
stops it, with or without a valid token left: it logs one line and tries again, at every check at first,
then doubling the wait up to five checks. A 403 is a rate limit when its body mentions one or its headers
carry `x-ratelimit-remaining: 0` or a `retry-after`. A rate limit, of the mint or of the bot-user lookup,
is waited out, as GitHub asks: the loop never tries again before the `retry-after`, nor, when
`x-ratelimit-remaining` is 0, before the `x-ratelimit-reset` time (Unix seconds), whichever is later,
each honoured up to 3600 s however many checks that is (a longer one, or one too long to be a number of
seconds, is cut to 3600 s). Only a **permanent** one stops it, with exit 1: GitHub refusing the mint
with 401 (the JWT), any other 403 or 404 (the App or its installation) or another 4xx, such a 4xx from the
slug or bot-user lookup, a key that cannot sign, a VM refusing to write through a symlink, or bad inputs at
the start. Its stop line names the token's expiry only while that is still ahead.
One refresher runs per VM instance: it holds the lock
`${XDG_STATE_HOME:-~/.local/state}/shift2eu/agents-token-refresh-<instance>.lock`, a directory with its
pid inside, removed by its exit trap. The lock is created whole (a temporary directory with the pid
already inside, renamed to the lock), so no reader sees a lock without its pid. A second refresher of the
same instance says that another refresher for that VM is running and exits **0**: the VM's token is being
refreshed, which is what was asked. A lock whose pid is no longer a running refresh (a refresher killed
without its trap) is taken over atomically: it is renamed aside, which only one contender can do, checked
to still hold the stale pid, removed, and the lock is created again. A contender that moved aside a lock
holding another pid puts it back only while no lock exists; when a third contender has created one
meanwhile, it drops what it moved and exits as a losing refresher, so a lock never lands inside another
(issue #288). Before each mint, a refresher checks that the lock still holds its own pid: a lock that reads
empty is read again once a second for up to five seconds, as a contender may have it aside for a moment
(and after such a wait the refresher reads the clock again before deciding to mint); a lock that holds
another pid makes it stop without minting, and a lock still missing after the retries makes it stop
without minting and say that the lock is gone and a refresh must be started again. This narrows the race
but does not close it, and two residues are accepted. First, a lock that turns over while its old owner
is already past that check and minting can still cost one duplicate mint; that owner stops at its next
check. That is harmless: every write into the VM is atomic, and each token expires within the hour.
Second, a contender stalled for more than about five seconds with the lock moved aside (or killed in that
moment) can leave the other refresher giving up on a lock it reads as gone, and a lock holding a dead pid
or none; then no refresher runs until the next `make agents-token-refresh-start` (or launchd's restart of
it), which takes the stale lock over. A refresher's exit trap removes only a lock that holds its own pid.
Its TERM and INT traps also kill a sleep started just before its pid was stored (issue #289), and kill a
pid only while `ps` names the refresher as its parent: a sleep the shell has already reaped, whose pid the
system may have given to another process, is not killed, short of its pid being reused in the instant
between that check and the kill (the check and the kill are not atomic, and the shell may reap the sleep
while it waits for `ps`). If `ps` fails or is missing, the traps kill nothing, and the worst case is a
harmless `sleep` that outlives the refresher and runs out on its own.
`make agents-token-refresh-start` starts the refresh in the background with `nohup`, unless the lock's pid,
or the pid it recorded itself, is a running refresh, in which case it names that pid; its pid and log are
`agents-token-refresh-<instance>.{pid,log}` in the same directory (mode 0600), and it says where the log
is. It says when the refresh stopped, four ways: a line on the refresh's standard error (so in the log)
with `agents App token refresh stopped`, the time, the token's state and where to start it again (from the
host clone on the Mac with `make agents-token-refresh-start`), on a signal and on a permanent failure; `make agents-token-refresh-start` warns when its pid file is stale and
shows the log's last lines before starting a new one; and once the token has expired the wrapper's error
asks the maintainer to run `make agents-token-refresh-start` in the host clone on the Mac.
Why there: the refresh needs the App's variables, which live only in the host clone's `.envrc`, and runs
the clone's own reviewed code; `make deck` runs in the workspace, where neither holds. `make deck` starts
nothing (the first version of this change had it start the refresh). launchd lost: a launch agent gets no
direnv environment, so the key's path would have to be written into a plist; it is a second install step
outside `make`; and nothing of it can be tested on Linux CI.

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
- **The key is only as safe as the host-side code that runs with it, so it runs from the host clone.**
  The workspace is mounted read-write into the VM, `.git` included, so an agent can change its
  `Makefile` (or add a `GNUmakefile`, which make reads first), `dev/`, and `.git/config` (`core.fsmonitor`
  and `core.pager` run commands, hooks run on `git fetch`, the remote URL decides what `origin/main` is).
  Any `git` or `make` command run in the workspace on the Mac runs what those say, as the maintainer, so
  no check run there can vouch for the workspace: the check itself would be agent-controlled code (an
  earlier draft offered `git diff --stat origin/main`; `--assume-unchanged` hides a change from it, and
  `core.fsmonitor` makes it run a command). Hence the host clone: outside the workspace, its own `.git`,
  updated only with `git -C <clone> pull --ff-only` from GitHub, the only place the three App variables
  are set (its own `.envrc`), and where `make agents-token`, `make agents-token-refresh` and
  `make agents-token-refresh-start` run; their recipes use only `limactl` and `LIMA_INSTANCE`, nothing
  of the workspace, but like every host target they refuse to run without `WORKSPACE_REPO` set (the
  host-clone refusal).
- **The host clone does not put the key out of reach of code run from the workspace.** It keeps the key's
  path out of the workspace's shells and keeps the token targets on code the agents cannot edit. But every
  host target the maintainer still runs from the workspace (`make deck`, `make vm-env`, `make vm-init`,
  `make vm-gh-wrapper`) runs agent-editable code on the Mac as the maintainer's uid. That code needs no
  variable: it can read the key file, whose path is in the host clone's `.envrc` and in the running
  refresher's environment, and it can tamper with the host clone itself (`.git/hooks`, `.git/config`, its
  files), which `git pull --ff-only` does not undo, so the next token target there would run the agents'
  code with the key. The App key is therefore in the same class as the credentials `make vm-env` copies
  from the `.envrc` above the workspace (`GITHUB_TOKEN`, the Claude token; milestone 3 removes
  `GITHUB_TOKEN`), and it never expires. This change narrows the easiest route (the key's path in a
  workspace shell's environment), not the class. Mitigations until the follow-ups: open the deck by typing
  `limactl shell --workdir <path of the main checkout> <instance> dot-agent-deck` (what `make deck` runs)
  directly, not through `make` in the workspace, and run `make vm-env`, `make vm-init` and
  `make vm-gh-wrapper` only rarely, and only after reading the diff of the `Makefile` and `dev/` on
  GitHub. No git check run in the host clone is a safeguard: once the clone is tampered with, git there
  runs the tampered hooks and config too. **Open follow-ups, not in this change, tracked in #227:** run
  every host target from the host clone (`make deck` needs a variable for the workspace path instead of
  `CURDIR`), and keep the key where the maintainer's uid cannot read it silently (a Keychain item or a
  password-manager read that prompts, or a separate macOS account running the refresher).
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
- `make agents-token-refresh-start` leaves a background process on the Mac; it is stopped with
  `kill "$(cat ~/.local/state/shift2eu/agents-token-refresh-shift2eu.lock/pid)"` (the instance's name in
  the path). `make deck` is unchanged but for a warning when an App variable reaches it.
- The maintainer's App gets one permission beyond the PRD's list (Attestations read); `gh run rerun` and
  the rulesets' bypass checks become maintainer steps after the switch (milestone 2).
- Questions only the real App can answer are checks at the runbook's step 3: the pull request's author,
  the commit author, `gh auth status`, the reads of d), and the deck's own `gh` calls.
