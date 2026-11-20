#!/bin/sh
# Prints the LaunchDaemon property list that starts the Lima VM of LIMA_INSTANCE at boot, with nobody logged in, as
# the maintainer (issue #398, docs/adr/0038-start-the-lima-vm-at-boot-with-a-launchdaemon-of-our-own.md). make
# vm-boot-install renders it with this script and installs it in /Library/LaunchDaemons; this script itself
# installs nothing, needs no sudo and runs no limactl, launchctl or sudo.
#
# Usage: dev/vm-boot-launchd.sh plist   print the plist of LIMA_INSTANCE's boot job on standard output
#
# The job, one per VM instance, labelled io.github.timothymamo.shift2eu.vm-boot.<instance>:
#   - runs `limactl shell --start --workdir / <instance> true`, the command line make vm-up starts an existing
#     instance with (issue #254): it starts a Stopped instance, only runs true in a Running one, and fails with
#     "does not exist" for an instance that has gone, so it never creates a VM. It never runs `limactl start`,
#     which Lima's own `limactl autostart` job runs and which can fall back to template:default, a VM that mounts
#     the home directory (issue #237);
#   - runs as the account that renders the plist (UserName, from id -un), never as root: the instance belongs to
#     that account (Lima keeps it under its ~/.lima or LIMA_HOME);
#   - starts through /usr/bin/env -i with exactly HOME, a PATH that finds limactl (the directory this script found
#     it in, then /usr/bin:/bin:/usr/sbin:/sbin), LIMA_INSTANCE and, when it is set, LIMA_HOME: no credential of
#     any kind. The VM needs none to boot; the agents App's refresh job delivers the token (ADR 0034);
#   - runs once at load, that is at boot and at the install's bootstrap (RunAtLoad, no KeepAlive), and exits with
#     limactl's status, so an instance that is missing or broken leaves a non-zero exit and its message in the log;
#   - opens its own log, as the maintainer, through a shell line (`/bin/sh -c '<line>' <log> <limactl command>`,
#     the log and every word of the command arguments of their own, spliced into no shell text): umask 077,
#     mkdir -p of the log's directory, one dated line, then exec of the command with both streams appended to
#     ${XDG_STATE_HOME:-$HOME/.local/state}/shift2eu/vm-boot-<instance>.log, beside the refresh's log and never
#     inside ~/.lima, where a log launchd creates would bring a deleted instance's directory back (issue #237).
#     StandardOutPath and StandardErrorPath are /dev/null: launchd may open them as root, before UserName
#     applies, in a directory the maintainer writes (ADR 0034).
#
# It refuses to run as root (uid 0, as under sudo make) before it prints anything: the job would run as root.
#
# Inputs, from the host clone's .envrc (docs/dev-environment.md, The host clone): LIMA_INSTANCE (letters, digits,
# ., _ and -, beginning with a letter or digit; it becomes part of the label), HOME (absolute), and optionally
# XDG_STATE_HOME and LIMA_HOME (absolute; empty counts as unset). limactl must be on PATH, at an absolute path.
# A missing, empty or malformed value, or one holding a character XML markup uses (< > & " ') or a control
# character, is refused before anything is printed, with the variable's name, so make never installs half a plist.
set -eu

# Also in the Makefile (VM_BOOT_LAUNCHD_LABEL_PREFIX): keep the two alike.
label_prefix=io.github.timothymamo.shift2eu.vm-boot.

die() {
  printf 'vm-boot-launchd: %s\n' "$*" >&2
  exit 1
}

# plain <name> <value> refuses a value the plist cannot hold as it is, as dev/agents-token-launchd.sh does (keep
# the two alike): XML markup (escaping it would make the job see what the maintainer did not write) or a control
# character (0x01-0x1F, line breaks and tabs included, or 0x7F).
plain() {
  case "$2" in
  *'<'* | *'>'* | *'&'* | *'"'* | *"'"*)
    die "$1 holds a character a plist cannot carry as it is (< > & \" or '): use a path without it"
    ;;
  esac
  controls="$(printf '%s' "$2" | LC_ALL=C tr -cd '\001-\037\177' | wc -c)" ||
    die "cannot check $1 for control characters"
  [ "${controls}" -eq 0 ] ||
    die "$1 holds a control character (a line break, a tab, an escape or another byte 0x01-0x1F or 0x7F): use a path without it"
}

# absolute <name> <value> refuses an empty or relative path: launchd starts a job from no directory of its own.
absolute() {
  case "$2" in
  /*) plain "$1" "$2" ;;
  '') die "$1 is not set: set it in the host clone's .envrc (docs/dev-environment.md, The host clone)" ;;
  *) die "$1 must be an absolute path: launchd starts the job from no directory of its own" ;;
  esac
}

check_inputs() {
  uid="$(id -u)" || die "cannot tell which account runs this (id -u failed)"
  [ "${uid}" != 0 ] ||
    die "refused: run it as the maintainer, not as root (no sudo make): the job runs as the account that renders the plist, which owns the VM, and make vm-boot-install runs sudo itself"
  instance="${LIMA_INSTANCE:-}"
  # Spelled out, not A-Z: some shells match a range by the locale's collation (dev/agents-app-token.sh).
  case "${instance}" in
  '') die "LIMA_INSTANCE is not set: name the VM instance the job starts at boot" ;;
  [!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789]* | *[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-]*)
    die "LIMA_INSTANCE must be letters, digits, ., _ and -, beginning with a letter or digit: it becomes part of the job's label and an argument of limactl"
    ;;
  esac
  home="${HOME:-}"
  absolute HOME "${home}"
  xdg_state_home="${XDG_STATE_HOME:-}"
  if [ -n "${xdg_state_home}" ]; then
    absolute XDG_STATE_HOME "${xdg_state_home}"
    state="${xdg_state_home}/shift2eu"
  else
    state="${home}/.local/state/shift2eu"
  fi
  lima_home="${LIMA_HOME:-}"
  if [ -n "${lima_home}" ]; then
    absolute LIMA_HOME "${lima_home}"
  fi
  limactl="$(command -v limactl)" || die "no limactl on PATH: install Lima (brew install lima), or put limactl on PATH"
  absolute "limactl's path" "${limactl}"
  user="$(id -un)" || die "cannot read the account name (id -un)"
  plain "the account name" "${user}"
}

plist() {
  check_inputs
  log="${state}/vm-boot-${instance}.log"
  job_path="${limactl%/*}:/usr/bin:/bin:/usr/sbin:/sbin"
  lima_home_entry=''
  if [ -n "${lima_home}" ]; then
    lima_home_entry="		<string>LIMA_HOME=${lima_home}</string>"
  fi
  # The shell line the job runs, XML-escaped: $0 is the log, "$@" the limactl command line. It opens the log for
  # appending as the job's UserName, recreating its directory and keeping a recreated log the maintainer's alone,
  # writes one dated line so a reboot test can tell this boot's run from an earlier one, and execs the command, so
  # the job's exit status is limactl's.
  # shellcheck disable=SC2016 # expanded by the job's /bin/sh -c, not here
  open_log='umask 077 &amp;&amp; mkdir -p "${0%/*}" &amp;&amp; printf "%s vm-boot: %s\n" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" &gt;&gt;"$0" &amp;&amp; exec "$@" &gt;&gt;"$0" 2&gt;&amp;1'
  {
    printf '%s\n' \
      '<?xml version="1.0" encoding="UTF-8"?>' \
      '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' \
      '<plist version="1.0">' \
      '<dict>' \
      '	<key>Label</key>' \
      "	<string>${label_prefix}${instance}</string>" \
      '	<key>UserName</key>' \
      "	<string>${user}</string>" \
      '	<key>ProgramArguments</key>' \
      '	<array>' \
      '		<string>/usr/bin/env</string>' \
      '		<string>-i</string>' \
      "		<string>HOME=${home}</string>" \
      "		<string>PATH=${job_path}</string>" \
      "		<string>LIMA_INSTANCE=${instance}</string>"
    if [ -n "${lima_home_entry}" ]; then
      printf '%s\n' "${lima_home_entry}"
    fi
    printf '%s\n' \
      '		<string>/bin/sh</string>' \
      '		<string>-c</string>' \
      "		<string>${open_log}</string>" \
      "		<string>${log}</string>" \
      "		<string>${limactl}</string>" \
      '		<string>shell</string>' \
      '		<string>--start</string>' \
      '		<string>--workdir</string>' \
      '		<string>/</string>' \
      "		<string>${instance}</string>" \
      '		<string>true</string>' \
      '	</array>' \
      '	<key>WorkingDirectory</key>' \
      '	<string>/</string>' \
      '	<key>RunAtLoad</key>' \
      '	<true/>' \
      '	<key>StandardOutPath</key>' \
      '	<string>/dev/null</string>' \
      '	<key>StandardErrorPath</key>' \
      '	<string>/dev/null</string>' \
      '</dict>' \
      '</plist>'
  }
}

case "${1:-}" in
plist) plist ;;
*)
  echo "usage: $0 plist" >&2
  exit 2
  ;;
esac
