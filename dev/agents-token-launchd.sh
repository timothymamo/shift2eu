#!/bin/sh
# Prints the LaunchDaemon property list that runs the agents App token refresh at boot, with nobody logged
# in, as the maintainer (issue #229, docs/adr/0034-run-the-agents-app-token-refresh-as-a-launchdaemon.md).
# make agents-token-refresh-install renders it with this script and installs it in /Library/LaunchDaemons;
# this script itself installs nothing and needs no sudo.
#
# Usage: dev/agents-token-launchd.sh plist   print the plist of LIMA_INSTANCE's job on standard output
#
# The job, one per VM instance, labelled io.github.timothymamo.shift2eu.agents-token-refresh.<instance>:
#   - runs `/bin/sh <host clone>/dev/agents-app-token.sh refresh` from the host clone, as the account that
#     renders the plist (UserName, from id -un): never make and never git, so no direnv, no Makefile and no
#     pull is involved at boot. The plist names the script's path, not its contents: the job runs the host
#     clone's dev/agents-app-token.sh as it is on disk when the job STARTS (at boot, and at every restart),
#     not as it was at install time. After a git pull --ff-only in the host clone, restart the job so it runs
#     the new script now: sudo launchctl kickstart -k system/<label>;
#   - hands that refresh exactly LIMA_INSTANCE, SHIFT2EU_AGENTS_APP_ID, SHIFT2EU_AGENTS_APP_INSTALLATION_ID,
#     SHIFT2EU_AGENTS_APP_KEY_FILE (the key's path, never its contents), HOME, XDG_STATE_HOME when it is set
#     and not empty (so the job's lock and log are the ones make agents-token-refresh-start and a refresh
#     started by hand use), and a PATH that finds Homebrew's limactl: launchd gives a job no shell environment
#     of its own, so the plist holds a copy of the values the host clone's .envrc had at install time;
#   - starts at load (boot) and is restarted whenever it exits (RunAtLoad, KeepAlive), at most once every
#     ThrottleInterval seconds (300): a refresh that finds another one holding the instance's lock exits 0,
#     so a refresh started by hand beside the job costs one log line every five minutes, and a permanent
#     failure one mint attempt every five minutes;
#   - opens its own log, as the maintainer, through a shell line
#     (`umask 077 && mkdir -p "${1%/*}" && exec /bin/sh "$0" refresh >>"$1" 2>&1`, with the script and the
#     log as arguments of their own, spliced into no shell text; the umask keeps a recreated log 0600 and
#     mkdir -p recreates a missing state directory, so the job does not fail before its first line): the log make
#     agents-token-refresh-start writes, ${XDG_STATE_HOME:-$HOME/.local/state}/shift2eu/
#     agents-token-refresh-<instance>.log, never inside ~/.lima, where a log launchd creates would bring a
#     deleted instance's directory back (issue #229, the note from #237). StandardOutPath and
#     StandardErrorPath are /dev/null: launchd may open them as root, before UserName applies, and the log's
#     directory is the maintainer's to write, so a symlink planted there would make root append anywhere.
#
# It refuses to run as root (uid 0, as under sudo make) before it prints anything: the job would run as root.
#
# Inputs, from the host clone's .envrc (docs/dev-environment.md, The host clone): LIMA_INSTANCE (letters,
# digits, ., _ and -, beginning with a letter or digit), SHIFT2EU_AGENTS_APP_ID and
# SHIFT2EU_AGENTS_APP_INSTALLATION_ID (digits), SHIFT2EU_AGENTS_APP_KEY_FILE (an absolute path naming a
# regular file, not a symlink), HOME (absolute) and, optionally, XDG_STATE_HOME (absolute; empty counts as
# unset, as ${XDG_STATE_HOME:-...} does). A missing, empty or malformed value, or any value holding a
# character XML markup uses (< > & " ') or a control character (bytes 0x01-0x1F, line breaks and tabs
# included, and 0x7F), is refused before anything is printed, with the variable's name and never the key's
# contents, so make never installs half a plist. The script never reads the key file and runs no limactl,
# curl, gh or git.
set -eu

label_prefix=io.github.timothymamo.shift2eu.agents-token-refresh.
throttle_seconds=300

die() {
  printf 'agents-token-launchd: %s\n' "$*" >&2
  exit 1
}

# plain <name> <value> refuses a value the plist cannot hold as it is: one with XML markup (escaping it
# would make the job see what the maintainer did not write) or with a control character (0x01-0x1F, line
# breaks and tabs included, or 0x7F), which no one reads back from a plist or a terminal as it is. tr counts
# the control bytes, rather than a case pattern, because some shells treat 0x01 and 0x7F in a pattern as
# their own quoting markers.
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

digits() {
  case "$2" in
  '') die "$1 is not set: set it in the host clone's .envrc (docs/dev-environment.md, The host clone)" ;;
  *[!0123456789]*) die "$1 must be a number" ;;
  esac
}

check_inputs() {
  # The job runs as the account that renders the plist (UserName, from id -un): rendered under sudo, it would
  # run the host clone's script, which the maintainer can edit, as root at every boot.
  uid="$(id -u)" || die "cannot tell which account runs this (id -u failed)"
  [ "${uid}" != 0 ] ||
    die "refused: run it as the maintainer, not as root (no sudo make): the job runs as the account that renders the plist, and make agents-token-refresh-install runs sudo itself"
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
  # An empty XDG_STATE_HOME is no XDG_STATE_HOME, as ${XDG_STATE_HOME:-...} reads it in the Makefile and in
  # dev/agents-app-token.sh; a set one goes into the plist, so the job's lock and log are theirs too.
  xdg_state_home="${XDG_STATE_HOME:-}"
  if [ -n "${xdg_state_home}" ]; then
    absolute XDG_STATE_HOME "${xdg_state_home}"
    state="${xdg_state_home}/shift2eu"
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
  xdg_entry=''
  if [ -n "${xdg_state_home}" ]; then
    xdg_entry="$(printf '\t\t<key>XDG_STATE_HOME</key>\n\t\t<string>%s</string>' "${xdg_state_home}")"
  fi
  # The shell line the job runs, XML-escaped: it opens the log ($1) for appending as the job's UserName and
  # execs the script ($0). Both reach it as arguments of their own, never spliced into the shell text. It
  # sets umask 077 first, so a log the job recreates (after it was removed or rotated) is the maintainer's
  # alone (0600), and creates the log's directory, so a state directory removed since the install makes the
  # job recreate it rather than fail before the script writes a single line.
  # shellcheck disable=SC2016 # expanded by the job's /bin/sh -c, not here
  open_log='umask 077 &amp;&amp; mkdir -p "${1%/*}" &amp;&amp; exec /bin/sh "$0" refresh &gt;&gt;"$1" 2&gt;&amp;1'
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
    '		<string>/bin/sh</string>' \
    '		<string>-c</string>' \
    "		<string>${open_log}</string>" \
    "		<string>${clone}/dev/agents-app-token.sh</string>" \
    "		<string>${log}</string>" \
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
    '		<string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>'
    if [ -n "${xdg_entry}" ]; then
      printf '%s\n' "${xdg_entry}"
    fi
    printf '%s\n' \
      '	</dict>' \
      '	<key>RunAtLoad</key>' \
      '	<true/>' \
      '	<key>KeepAlive</key>' \
      '	<true/>' \
      '	<key>ThrottleInterval</key>' \
      "	<integer>${throttle_seconds}</integer>" \
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
