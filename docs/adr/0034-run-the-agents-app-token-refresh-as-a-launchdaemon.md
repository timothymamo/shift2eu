# 0034. Run the agents App token refresh as a LaunchDaemon, so it survives a reboot

- Status: Accepted
- Date: 2026-11-18

Supersedes in part [ADR 0031](0031-agents-open-pull-requests-as-a-github-app-with-tokens-minted-on-the-mac.md),
open question (1) a): the refresh can now also run under launchd. Everything else in ADR 0031 stands.

## Context

ADR 0031 a) runs the agents App token refresh as a background loop that the maintainer starts from the
host clone with `make agents-token-refresh-start`, and turned launchd down: a launch agent gets no direnv
environment, it is a second install step outside `make`, and none of it can be tested on Linux CI.

Since then the deck has moved to an unattended Mac Mini (issue #229). A reboot or a power cut kills the
hand-started loop, but the VM comes back on its own (`limactl autostart enable --condition=boot`). Within
the hour the VM's token expires, and since PRD #161 milestone 3 the gh wrapper has no fallback: every `gh`
call and `git push` fails, and the agents stall until the maintainer logs in over SSH and starts the loop
again. The refresh must come back after a boot with nobody logged in, and stay testable on Linux CI.

The refresh itself needs nothing new: `dev/agents-app-token.sh refresh` already rides out a VM that is
still starting (a transient failure it retries), and exits 0 when another refresh of the instance holds
its lock.

## Decision

**A LaunchDaemon per VM instance runs `dev/agents-app-token.sh refresh` in the foreground, as the
maintainer, at boot.** `make agents-token-refresh-install`, run from the host clone, renders its plist
with `dev/agents-token-launchd.sh plist` (a pure function of its inputs, pinned by
`test/devenv/launchdplist_test.go` on Linux), refuses a clone that a mount of the VM reaches (d) and a
clone whose `dev/` or `dev/agents-app-token.sh` is a symlink, lints the plist with `plutil -lint`, prints
it with its SHA-256, the script the job runs and every `sudo` command, installs it as
`/Library/LaunchDaemons/<label>.plist` (root, mode 0644), checks the installed file's SHA-256 and
bootstraps it into the `system` domain; `make agents-token-refresh-uninstall` boots it out and removes
that one file. The directory is the Makefile's `AGENTS_TOKEN_LAUNCHD_DIR`, a hook only for the tests,
which point it into their sandbox; since root writes there, both targets refuse a value that is not
absolute or holds anything but letters, digits, `/`, `.`, `_` and `-`, or a `..` component. The value may
come only from the `make` command line or from the Makefile: both targets refuse, before any `sudo`, one
whose `$(origin)` is `environment` (or `environment override`), since a stale export, from a test or an
earlier session, would have root install the plist where launchd never looks at boot, and the job would
quietly be gone after the next reboot.

**The install fails closed.** Over a loaded job it runs `launchctl bootout` and waits, at most 10 seconds,
while `launchctl print` still finds the job; a job still loaded then stops the install before it installs
anything (the old job keeps running, and the message says so, never that no job is loaded). After
`sudo install` it reads the destination back, without sudo, and compares its SHA-256 with the one it
printed: on a mismatch, or when the file cannot be read, it removes the file again with `sudo rm -f`,
loads nothing and fails, saying no token refresh job of the instance is loaded. A plist left behind would
be loaded at the next boot without anyone having checked it, so a warning is not enough. A failing
`launchctl bootstrap` is tried once more; when that fails too, `launchctl print system/<label>` decides,
since bootstrap can load the job and still exit non-zero: a loaded job is a success and the install says
it is loaded; otherwise the install fails, says the checked plist stays in place and loads at the next
boot, and names the two ways to restore a job now: the install again, or
`make agents-token-refresh-start`. The uninstall boots the job out, waits at most 10 seconds, as the install does, while
`launchctl print` still finds it, and removes the plist either way; a job still loaded is reported with
`sudo launchctl bootout system/<label>`, which the maintainer runs by hand (it otherwise runs until the
next reboot), and the uninstall exits 0, since the plist is gone and running it again would change
nothing. `make agents-token-refresh-start` stays as it was, but warns and starts nothing
while the instance's LaunchDaemon is loaded (`launchctl print system/<label>` succeeds), says so when the
loaded job runs another clone's script, and needs no launchctl on Linux.

The job:

| Key | Value |
|---|---|
| `Label` | `io.github.timothymamo.shift2eu.agents-token-refresh.<LIMA_INSTANCE>`: one job per VM instance |
| `UserName` | the account that installed it (`id -un`): the job runs as the maintainer, not as root |
| `ProgramArguments` | `/bin/sh -c 'umask 077 && mkdir -p "${1%/*}" && exec /bin/sh "$0" refresh >>"$1" 2>&1' <host clone>/dev/agents-app-token.sh <log>`: the script and the log are arguments of their own, never spliced into the shell text |
| `WorkingDirectory` | the host clone |
| `EnvironmentVariables` | exactly `LIMA_INSTANCE`, the three `SHIFT2EU_AGENTS_APP_*` (the key's **path**), `HOME`, `XDG_STATE_HOME` when it is set and not empty, and a `PATH` with `/opt/homebrew/bin` (Homebrew's `limactl`) |
| `RunAtLoad`, `KeepAlive` | true: it starts at boot and comes back whenever it exits |
| `ThrottleInterval` | 300 seconds |
| `StandardOutPath`, `StandardErrorPath` | `/dev/null`: the job opens its log, `${XDG_STATE_HOME:-~/.local/state}/shift2eu/agents-token-refresh-<instance>.log`, itself |

**The job opens its own log.** launchd may open `StandardOutPath` and `StandardErrorPath` as root, before
`UserName` applies, and the log lives in the maintainer's state directory: a symlink planted there would
make root append to any file. So both are `/dev/null`, and the shell line opens the log as the maintainer,
with the maintainer's permissions, the log `make agents-token-refresh-start` and a hand-started refresh
write. It sets `umask 077` and creates the log's directory first, so a log it recreates is 0600 and a
state directory removed since the install does not make the job exit before it writes a line (launchd's
`last exit code` and `runs` in `launchctl print` are then the only trace). `XDG_STATE_HOME` goes into the plist when the host clone sets it, so the job's log and lock are the
same files the Makefile's targets and a refresh started by hand use; an empty one counts as unset, as
`${XDG_STATE_HOME:-...}` reads it. Losing launchd's own messages about the job is the price; `launchctl
print` still shows its state and last exit.

The open points of issue #229:

- **a) The plist, not direnv, supplies the variables.** launchd runs no shell and no direnv, and a job
  inherits no environment. The install target reads the three `SHIFT2EU_AGENTS_APP_*` from the host
  clone's `.envrc`, through the maintainer's shell, and the plist holds a copy of their values from that
  moment: after changing `.envrc` (a new key file, another installation), install again. The plist is
  world-readable, so it holds the App ID, the installation ID and the key's path, which are not secret,
  and never the key or a token; the script refuses any value with XML markup or a control character
  (0x01-0x1F, 0x7F), naming the variable, so the job never sees a value the maintainer did not write.
- **b) No make under launchd.** The job runs `/bin/sh dev/agents-app-token.sh refresh` directly, so GNU
  make against `/usr/bin/make`, and a Makefile's behaviour at boot, never come up. make runs only at
  install, in the maintainer's shell, as every other host-clone target does.
- **c) Composition with #227 part 2** (a separate macOS account running the refresher, so the
  maintainer's uid cannot read the key). That account would own the job: `UserName` becomes it, and the
  key, the host clone and the state directory move into its home. The plist generator needs no change for
  that: run as that account (`sudo -u <account> -i`, then `make agents-token-refresh-install` from its
  host clone), it renders that account's name, `HOME` and paths.
- **d) The trust boundary is unchanged.** The plist is root-owned in `/Library/LaunchDaemons`, so an
  unprivileged process cannot rewrite the job, but the job runs the host clone's `dev/agents-app-token.sh`
  as the maintainer. Whoever can change the host clone controls what runs with the key, at every boot now
  instead of at the next `make agents-token-refresh-start`: the host clone's integrity is still the
  boundary (ADR 0031, Consequences), and the job never pulls (`git pull --ff-only` stays a step the
  maintainer runs, followed by an install or a `launchctl kickstart -k` to run the new code). The job runs
  the script as it is on disk when the job starts, not as it was at install time.
  An install from the wrong clone, the workspace the VM mounts, would widen the exposure from a one-off run
  of code the agents can edit to a persistent job that runs it unattended at every boot, as the
  maintainer, with the key in reach. Four guards stand against that: the install asks `limactl list` once
  for the instance's name and mounts and refuses, before any `sudo`, a clone that is a mount, lies under
  one or holds one (a mount inside the clone lets the agents edit its `dev/`; compared by path components,
  `~` and `~/...` expanded against `HOME` and symlinked locations resolved; the comparison is
  case-sensitive while APFS usually is not, which holds because Lima writes mount locations in their
  canonical case and the clone's path comes from `pwd -P`; a location typed in another case in
  `dev/lima.yaml` would slip past it), and refuses too when limactl
  fails or its first line is not exactly the instance, since the check cannot be done then: limactl may
  print nothing and exit 0 for an instance it does not know, and another instance's mounts prove nothing
  (create the VM first). It also refuses a clone whose `dev/` or `dev/agents-app-token.sh` is a symlink,
  which would point the job at a file the install never looked at; it prints, before any `sudo`, the script the
  job will run and that it must be the host clone's; `make agents-token-refresh-start` warns when the
  loaded job runs another clone's script; and the runbook's check after install looks for the host
  clone's script in `sudo launchctl print system/<label>`.
- **e) The note from #237.** The job never starts the VM: it only writes into a VM that runs, and treats
  one that does not as a transient failure, so it can never start a Broken instance and create a default
  VM. Its logs are in the state directory, never under `~/.lima/<instance>/`, which a log launchd creates
  would otherwise bring back after every removal.

**KeepAlive and the lock.** A refresh that finds another one holding the instance's lock exits 0, and
`KeepAlive` restarts any job that exits. `ThrottleInterval` is what keeps that quiet: launchd starts the
job again at most once every 300 seconds, so while a refresh started by hand holds the lock, the job costs
one log line every five minutes and takes over within five minutes of that refresh stopping. A permanent
failure (a revoked key, a removed installation) likewise costs one refused mint every five minutes, each
logged with `agents App token refresh stopped`. Five minutes is well inside the 20 minutes before expiry
at which the refresh mints, so a refresh killed for any reason is back before the VM's token runs out.
The install does not stop a refresh started by hand: it says one holds the lock and names its pid to stop,
and the job takes over once it is gone. It reads the lock before `launchctl bootstrap`, after the old job
is booted out, since the job's own refresh takes the lock as soon as it loads: after the bootstrap it warns
only about the refresh that held the lock then, if it still holds it and is not the pid `launchctl print`
reports for the job, and never sends the maintainer to kill the job's own loop.

**Unprivileged `launchctl print`.** Every decision the targets make about whether the job is loaded (the
start target's warning, the install's bootout and its wait, the check after a failing bootstrap, the
uninstall's wait and its warning) rests on `launchctl print system/<label>` run without `sudo`, while the
runbook checks with `sudo launchctl print`. The assumption is that macOS lets an unprivileged user print a
job in the system domain; it cannot be checked on the Linux VM the tests run in, so the runbook's check
after install asks the maintainer to confirm it on the Mac. Were it wrong, the targets would see no job
where one is loaded (start would start a second refresher beside it, which waits on the lock; install
would skip the bootout, and its bootstrap would fail over the loaded job): the runbook then says to report
it and rely on the `sudo` commands. The targets add no `sudo` of their own for it: every `sudo` they run is
printed and pinned by the tests.

## Alternatives considered

- **Keep the hand-started loop only** (ADR 0031 a). Simple, but a reboot of the unattended Mac stalls the
  agents until someone logs in.
- **A LaunchAgent** (`~/Library/LaunchAgents`). It runs only in a login session, so a Mac nobody is logged
  in to after a power cut never starts it: the problem of issue #229.
- **Run `make agents-token-refresh` under launchd, or a shell that loads direnv.** It would need a make and
  a direnv at boot, read `.envrc` from a job (a file the maintainer edits, read by root's launchd context
  as the user), and run the Makefile's code at every boot. The script alone is all the job needs.
- **`KeepAlive` only on a failed exit** (`SuccessfulExit` false). It would leave the job stopped for good
  after it found a hand-started refresh's lock, so the instance would have no refresher once that one
  ended. A plain `KeepAlive` with a long `ThrottleInterval` covers both cases.
- **Let the install kill a hand-started refresh.** Nothing done silently: it prints the pid instead.

## Consequences

- After a reboot with nobody logged in, the refresh starts with the system and writes a fresh token as soon
  as the VM answers; the maintainer no longer has to start it after a power cut.
- A second install step outside the daily `make` flow, with `sudo`: `make agents-token-refresh-install`
  once per VM instance, again after a change to the three variables or to the host clone's code (or a
  `sudo launchctl kickstart -k system/<label>` after a pull, to run the pulled code).
- The refresh now runs continuously as the maintainer, with the key's path in its environment, from boot
  on, as the hand-started one did while it ran (ADR 0031, Consequences: the key stays readable by the
  maintainer's uid until #227).
- `docs/dev-environment.md` carries the install, check, reboot test, uninstall and rollback runbook.
- The Linux tests pin the plist, the start target's warnings, and the install's and uninstall's checks and
  `sudo` commands against fake `sudo`, `launchctl`, `plutil` and `limactl`; the real install and a boot are
  checked by hand on the Mac, as the runbook says.
