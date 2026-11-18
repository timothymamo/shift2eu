#!/bin/sh
# Prints the LaunchDaemon property list that runs the agents App token refresh at boot, with nobody logged
# in, as the maintainer (issue #229, docs/adr/0033-run-the-agents-app-token-refresh-as-a-launchdaemon.md).
# make agents-token-refresh-install renders it with this script and installs it in /Library/LaunchDaemons;
# this script itself installs nothing and needs no sudo.
#
# Usage: dev/agents-token-launchd.sh plist   print the plist of LIMA_INSTANCE's job on standard output
#
# The job, one per VM instance, labelled io.github.timothymamo.shift2eu.agents-token-refresh.<instance>:
#   - runs `/bin/sh <host clone>/dev/agents-app-token.sh refresh` from the host clone, as the account that
#     renders the plist (UserName, from id -un): never make and never git, so no direnv, no Makefile and no
#     pull is involved at boot, and the job runs the host clone's code as it was when it was installed;
#   - hands that refresh exactly LIMA_INSTANCE, SHIFT2EU_AGENTS_APP_ID, SHIFT2EU_AGENTS_APP_INSTALLATION_ID,
#     SHIFT2EU_AGENTS_APP_KEY_FILE (the key's path, never its contents), HOME, and a PATH that finds
#     Homebrew's limactl: launchd gives a job no shell environment of its own, so the plist holds a copy of
#     the values the host clone's .envrc had at install time;
#   - starts at load (boot) and is restarted whenever it exits (RunAtLoad, KeepAlive), at most once every
#     ThrottleInterval seconds (300): a refresh that finds another one holding the instance's lock exits 0,
#     so a refresh started by hand beside the job costs one log line every five minutes, and a permanent
#     failure one mint attempt every five minutes;
#   - writes its output to the log make agents-token-refresh-start writes,
#     ${XDG_STATE_HOME:-$HOME/.local/state}/shift2eu/agents-token-refresh-<instance>.log, never inside
#     ~/.lima, where a log launchd creates would bring a deleted instance's directory back (issue #229, the
#     note from #237).
#
# Inputs, from the host clone's .envrc (docs/dev-environment.md, The host clone): LIMA_INSTANCE (letters,
# digits, ., _ and -, beginning with a letter or digit), SHIFT2EU_AGENTS_APP_ID and
# SHIFT2EU_AGENTS_APP_INSTALLATION_ID (digits), SHIFT2EU_AGENTS_APP_KEY_FILE (an absolute path naming a
# regular file, not a symlink), HOME (absolute) and, optionally, XDG_STATE_HOME (absolute). A missing, empty
# or malformed value, or any value holding a character XML markup uses (< > & " ') or a line break, is
# refused before anything is printed, with the variable's name and never the key's contents, so make never
# installs half a plist. The script never reads the key file and runs no limactl, curl, gh or git.
set -eu

label_prefix=io.github.timothymamo.shift2eu.agents-token-refresh.
throttle_seconds=300

die() {
  printf 'agents-token-launchd: %s\n' "$*" >&2
  exit 1
}

nl="$(printf '\nx')"
nl="${nl%x}"
cr="$(printf '\r')"
tab="$(printf '\t')"

# plain <name> <value> refuses a value the plist cannot hold as it is: one with XML markup (escaping it
# would make the job see what the maintainer did not write) or with a line break or tab.
plain() {
  case "$2" in
  *'<'* | *'>'* | *'&'* | *'"'* | *"'"* | *"${nl}"* | *"${cr}"* | *"${tab}"*)
    die "$1 holds a character a plist cannot carry as it is (< > & \" ', a line break or a tab): use a path without it"
    ;;
  esac
}

# absolute <name> <value> refuses an empty or relative path: launchd starts a job from no directory of its own.
absolute() {
  case "$2" in
  /*) plain "$1" "$2" ;;
  '') die "$1 is not set: set it in the host clone's .envrc (docs/dev-environment.md, The host clone)" ;;
  *) die "$1 must be an absolute path: launchd starts the job from no directory of its own" ;;
  esac
}

digits() {
  case "$2" in
  '') die "$1 is not set: set it in the host clone's .envrc (docs/dev-environment.md, The host clone)" ;;
  *[!0123456789]*) die "$1 must be a number" ;;
  esac
}

check_inputs() {
  instance="${LIMA_INSTANCE:-}"
  # Spelled out, not A-Z: some shells match a range by the locale's collation (dev/agents-app-token.sh).
  case "${instance}" in
  '') die "LIMA_INSTANCE is not set: name the VM instance whose token the job refreshes" ;;
  [!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789]* | *[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-]*)
    die "LIMA_INSTANCE must be letters, digits, ., _ and -, beginning with a letter or digit: it becomes part of the job's label"
    ;;
  esac
  app_id="${SHIFT2EU_AGENTS_APP_ID:-}"
  digits SHIFT2EU_AGENTS_APP_ID "${app_id}"
  installation_id="${SHIFT2EU_AGENTS_APP_INSTALLATION_ID:-}"
  digits SHIFT2EU_AGENTS_APP_INSTALLATION_ID "${installation_id}"
  key="${SHIFT2EU_AGENTS_APP_KEY_FILE:-}"
  absolute SHIFT2EU_AGENTS_APP_KEY_FILE "${key}"
  # Its type only, never its contents: the refresh checks its mode and owner at every start.
  [ ! -L "${key}" ] && [ -f "${key}" ] ||
    die "SHIFT2EU_AGENTS_APP_KEY_FILE does not name a regular file (a symlink is refused): name the key file itself"
  home="${HOME:-}"
  absolute HOME "${home}"
  if [ -n "${XDG_STATE_HOME+set}" ]; then
    absolute XDG_STATE_HOME "${XDG_STATE_HOME}"
    state="${XDG_STATE_HOME}/shift2eu"
  else
    state="${home}/.local/state/shift2eu"
  fi
  user="$(id -un)" || die "cannot read the account name (id -un)"
  plain "the account name" "${user}"
  # The host clone is the repository holding this script, symlinks resolved.
  clone="$(cd "$(dirname "$0")/.." && pwd -P)" || die "cannot resolve the repository this script is in"
  plain "the host clone's path" "${clone}"
}

plist() {
  check_inputs
  log="${state}/agents-token-refresh-${instance}.log"
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
    '		<string>/bin/sh</string>' \
    "		<string>${clone}/dev/agents-app-token.sh</string>" \
    '		<string>refresh</string>' \
    '	</array>' \
    '	<key>WorkingDirectory</key>' \
    "	<string>${clone}</string>" \
    '	<key>EnvironmentVariables</key>' \
    '	<dict>' \
    '		<key>LIMA_INSTANCE</key>' \
    "		<string>${instance}</string>" \
    '		<key>SHIFT2EU_AGENTS_APP_ID</key>' \
    "		<string>${app_id}</string>" \
    '		<key>SHIFT2EU_AGENTS_APP_INSTALLATION_ID</key>' \
    "		<string>${installation_id}</string>" \
    '		<key>SHIFT2EU_AGENTS_APP_KEY_FILE</key>' \
    "		<string>${key}</string>" \
    '		<key>HOME</key>' \
    "		<string>${home}</string>" \
    '		<key>PATH</key>' \
    '		<string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>' \
    '	</dict>' \
    '	<key>RunAtLoad</key>' \
    '	<true/>' \
    '	<key>KeepAlive</key>' \
    '	<true/>' \
    '	<key>ThrottleInterval</key>' \
    "	<integer>${throttle_seconds}</integer>" \
    '	<key>StandardOutPath</key>' \
    "	<string>${log}</string>" \
    '	<key>StandardErrorPath</key>' \
    "	<string>${log}</string>" \
    '</dict>' \
    '</plist>'
}

case "${1:-}" in
plist) plist ;;
*)
  echo "usage: $0 plist" >&2
  exit 2
  ;;
esac
