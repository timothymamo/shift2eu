# 0033. Run the agents App token refresh as a LaunchDaemon, so it survives a reboot

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
`test/devenv/launchdplist_test.go` on Linux, and by `plutil -lint` where it exists), prints it and every
`sudo` command, installs it as `/Library/LaunchDaemons/<label>.plist` (root, mode 0644) and bootstraps it
into the `system` domain; `make agents-token-refresh-uninstall` boots it out and removes that one file.
`make agents-token-refresh-start` stays as it was, but warns and starts nothing while the instance's
LaunchDaemon is loaded (`launchctl print system/<label>` succeeds), and needs no launchctl on Linux.

The job:

| Key | Value |
|---|---|
| `Label` | `io.github.timothymamo.shift2eu.agents-token-refresh.<LIMA_INSTANCE>`: one job per VM instance |
| `UserName` | the account that installed it (`id -un`): the job runs as the maintainer, not as root |
| `ProgramArguments` | `/bin/sh <host clone>/dev/agents-app-token.sh refresh` |
| `WorkingDirectory` | the host clone |
| `EnvironmentVariables` | exactly `LIMA_INSTANCE`, the three `SHIFT2EU_AGENTS_APP_*` (the key's **path**), `HOME`, and a `PATH` with `/opt/homebrew/bin` (Homebrew's `limactl`) |
| `RunAtLoad`, `KeepAlive` | true: it starts at boot and comes back whenever it exits |
| `ThrottleInterval` | 300 seconds |
| `StandardOutPath`, `StandardErrorPath` | `${XDG_STATE_HOME:-~/.local/state}/shift2eu/agents-token-refresh-<instance>.log`, the log `make agents-token-refresh-start` writes |

The open points of issue #229:

- **a) The plist, not direnv, supplies the variables.** launchd runs no shell and no direnv, and a job
  inherits no environment. The install target reads the three `SHIFT2EU_AGENTS_APP_*` from the host
  clone's `.envrc`, through the maintainer's shell, and the plist holds a copy of their values from that
  moment: after changing `.envrc` (a new key file, another installation), install again. The plist is
  world-readable, so it holds the App ID, the installation ID and the key's path, which are not secret,
  and never the key or a token; the script refuses any value with XML markup or a line break, so the job
  never sees a value the maintainer did not write.
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
  maintainer runs, followed by an install or a `launchctl kickstart -k` to run the new code).
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
and the job takes over once it is gone.

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
- The Linux tests pin the plist and the start target's warning; installing and booting it are checked by
  hand on the Mac, as the runbook says.
