# 0038. Start the Lima VM at boot with a LaunchDaemon of our own, never `limactl autostart`

- Status: Accepted
- Date: 2026-11-20

Corrects the Context of [ADR 0034](0034-run-the-agents-app-token-refresh-as-a-launchdaemon.md), which says the
VM comes back on its own after a reboot (`limactl autostart enable --condition=boot`). Everything ADR 0034
decided stands.

## Context

The deck runs on an unattended Mac Mini. After a reboot or a macOS update nothing starts its Lima VM: the
maintainer logs in over SSH and runs `make vm-up` by hand, and until then the agents are down and the token
refresh's LaunchDaemon (ADR 0034) comes back on a host whose VM is not running (issue #398).

Lima has a way to start an instance at boot, `limactl autostart enable [--condition=boot]`, and
`docs/dev-environment.md` forbids it (issue #237). Its launchd job (`io.lima-vm.autostart.<instance>` in the
user's domain, or with `--condition=boot` the LaunchDaemon `io.lima-vm.daemon.<instance>`) runs
`limactl start <instance> --foreground`. `limactl start <name>` on an instance whose directory has lost its
`lima.yaml` reads the name as a template and falls back to `template:default`, a VM that mounts the whole home
directory; and on an instance that is gone it creates one. A job that runs at every boot makes that a standing
risk. `make vm-up` stopped starting an existing instance with `limactl start` for the same reason (issue
#254): it runs `limactl shell --start --workdir / <instance> true`, which since Lima 2.0 starts a `Stopped`
instance, only runs `true` in a `Running` one, and fails with `does not exist` for one that is gone. It never
creates a VM. On 2026-11-20 the Mini still listed `io.lima-vm.daemon.shift2eu => enabled` in
`launchctl print-disabled system`, a leftover enable override with no job behind it, so an autostart job is not a
hypothetical.

## Decision

**Never `limactl autostart`. A LaunchDaemon of the project's own per VM instance,
`io.github.timothymamo.shift2eu.vm-boot.<instance>`, runs `limactl shell --start --workdir / <instance> true`
once at boot, as the maintainer.** `dev/vm-boot-launchd.sh plist` renders it; `make vm-boot-install` and
`make vm-boot-uninstall` install and remove it the way ADR 0034's targets install and remove the refresh's:
host targets run from the host clone, as the maintainer and never under `sudo make`, with `plutil -lint`, the
plist and its SHA-256 printed, every `sudo` printed before it runs, a loaded job booted out first, the
installed file read back and compared, `launchctl bootstrap system <plist>`, and `VM_BOOT_LAUNCHD_DIR`
(default `/Library/LaunchDaemons`) as a test hook that may come only from make's command line or the Makefile.

| Key | Value |
|---|---|
| `Label` | `io.github.timothymamo.shift2eu.vm-boot.<instance>` |
| `UserName` | the account that renders the plist (`id -un`); the generator and the install refuse root |
| `ProgramArguments` | `/usr/bin/env -i HOME=<home> PATH=<limactl's directory>:/usr/bin:/bin:/usr/sbin:/sbin LIMA_INSTANCE=<instance>` (and `LIMA_HOME` when set), then `/bin/sh -c '<open the log>' <log> <limactl> shell --start --workdir / <instance> true` |
| `RunAtLoad` | true: at boot, and once at the install's bootstrap |
| `KeepAlive` | absent: the job runs once and exits with `limactl`'s status |
| `AbandonProcessGroup` | true: launchd does not kill the VM's hostagent when `limactl shell --start ... true` exits, whatever Lima does to detach it; the reboot test checks that the VM outlives the job |
| `StandardOutPath`, `StandardErrorPath` | `/dev/null`: the job opens `${XDG_STATE_HOME:-~/.local/state}/shift2eu/vm-boot-<instance>.log` itself, as the maintainer (ADR 0034's reason) |

The job carries no credential: the VM needs none to boot, and the refresh job delivers the token. An instance
that is missing or broken makes the job exit non-zero with `limactl`'s message in its log, and launchd does not
run it again until the next boot.

**One detection of Lima's autostart jobs, shared.** The Makefile macro `LIMA_AUTOSTART_JOBS` asks
`launchctl print gui/<uid>/io.lima-vm.autostart.<instance>` and
`launchctl print system/io.lima-vm.daemon.<instance>` (no `sudo`; skipped where there is no `launchctl`),
and looks for Lima's plist files on disk, `~/Library/LaunchAgents/io.lima-vm.autostart.<instance>.plist`
and `/Library/LaunchDaemons/io.lima-vm.daemon.<instance>.plist` (`LIMA_LAUNCHDAEMONS_DIR`, a test hook that
is only read, from make's command line or the Makefile), installed but not loaded, whether or not there is a
`launchctl`: an SSH-only session has no `gui/<uid>` domain to ask, and a job booted out by hand but left on
disk loads again at the next boot or login. It names each job it finds, its domain or file,
`limactl autostart disable <instance>`, and the removal by hand: `launchctl bootout` and `rm -f` of the
plist (`sudo` only for the LaunchDaemon). A plist path with anything but letters, digits, `/`, `.`, `_` and
`-` (as under a `HOME` with a space) is not repeated: the advice says the path is not shown and points at it
with an `ls` of `~/Library/LaunchAgents/<label>.plist` (or the LaunchDaemons one) instead. It does not detect a bare enable override (`=> enabled` in
`launchctl print-disabled`) with no job or plist behind it: that is inert alone, and
`docs/dev-environment.md` (Do not let Lima start the VM) turns it into a disable once per Mac.
`make vm-boot-install` refuses with it, before any `sudo`: Lima's job and ours would race for the same VM,
and Lima's can create `template:default`. `make vm-up` only warns with it (issue #315). It lives in the
Makefile, not in a `dev/` script, because `make vm-up` needs nothing else of the clone.

The open points of issue #398:

1. **Both modes, as the maintainer.** In the single-account mode and in the account mode (`_shift2euagents`,
   [ADR 0031](0031-agents-open-pull-requests-as-a-github-app-with-tokens-minted-on-the-mac.md)'s update of
   2026-11-19) alike, the job runs as the maintainer and is installed from the maintainer's host clone: Lima's
   instance belongs to the maintainer, under their `~/.lima`, and the account that holds the App's key has no
   business starting it. Only the refresh changes account in account mode.
2. **Whether it works with nobody logged in cannot be verified off the Mac.** Lima's `vz` driver uses
   Apple's Virtualization.framework, and whether `limactl shell --start` starts a VM from a LaunchDaemon with
   `UserName` and no GUI session is not something Linux CI can show. The check is the reboot test on the Mac
   Mini (`docs/dev-environment.md`, The VM at boot as a LaunchDaemon): after `sudo shutdown -r now`,
   `limactl list shift2eu` says `Running` within a few minutes with nobody logged in, and the refresh log shows
   a mint after the boot. If the VM stays `Stopped`, the fallback is today's: log in and run `make vm-up`, and
   remove the job with `make vm-boot-uninstall`; the job's log says why it failed. On a Mac with FileVault on,
   no LaunchDaemon runs until the disk is unlocked at the unlock screen: reboot with
   `sudo fdesetup authrestart`, or accept that a power cut needs a person at the Mac either way.
3. **One independent job per instance.** Each job starts its own instance and nothing else; a second VM on the
   host gets its own job (`make vm-boot-install LIMA_INSTANCE=<name>`). launchd starts them at boot in no
   particular order, which is fine: no instance depends on another.

**Ordering with the refresh job: none needed.** The refresh LaunchDaemon starts at boot beside the VM's and
may find the VM still down or booting. ADR 0034's Context already holds: `dev/agents-app-token.sh refresh`
treats an unreachable VM as a transient failure and retries, so it writes the first token once the VM is up.
The refresh is not changed.

## Alternatives considered

- **`limactl autostart enable --condition=boot`.** One command, Lima's own, and it lists the instance in
  `limactl list`. It runs `limactl start <name> --foreground`, the command that can create
  `template:default` (issue #237), and a job of Lima's that we neither render nor test.
- **Amend ADR 0034 and put the start into the refresh job.** One job instead of two. But the refresh runs as
  the account in account mode, which does not own the VM, and a `KeepAlive` job that starts the VM would start
  it again whenever the maintainer stops it with `make vm-stop`.
- **`KeepAlive` on the boot job.** It would restart a VM that crashed, but also one the maintainer stopped on
  purpose, and would retry a missing instance every few seconds. The job runs once; a VM that is down after
  boot is the maintainer's to look at.
- **A LaunchAgent in the maintainer's GUI domain.** It runs only after a login, which an unattended Mac never
  gets.

## Consequences

- After a reboot or a power cut the VM, and with it the deck's daemon service, comes back without anyone
  logging in, provided the reboot test passes; the documentation stops telling the maintainer to run
  `make vm-up` after every reboot, and keeps it as the fallback.
- `make vm-up` warns about a Lima autostart job of the instance, and `make vm-boot-install` refuses one, so
  the leftover of issue #237 can no longer sit there unnoticed.
- A second LaunchDaemon to install, and to install again when Homebrew moves `limactl` (the plist names its
  path) or when `HOME`, `XDG_STATE_HOME` or `LIMA_HOME` change.
- The open point 2 above stays open until the reboot test on the Mini has passed; the PR asks the maintainer to
  run it.
