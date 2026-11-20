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
# Account mode (issue #227 part 2, ADR 0034's update of 2026-11-19, c): make agents-token-refresh-install runs
# this script as the separate account that holds the App's key (sudo -u <account> -H /usr/bin/env -i ...), from
# that account's own clone, so UserName, HOME and the clone are the account's. With all four
# SHIFT2EU_AGENTS_VM_SSH_* variables set it renders the SSH mode's job (dev/agents-app-token.sh): it puts the four
# in EnvironmentVariables, sets PATH to exactly /usr/bin:/bin:/usr/sbin:/sbin (no /opt/homebrew/bin, which the
# maintainer's uid owns: a planted openssl, curl or ssh there would run as the account with the key), refuses
# the clone's dev/ or dev/agents-app-token.sh being a symlink, and applies the trusted path rule (ADR 0031,
# update of 2026-11-19, 2) to this script, dev/agents-app-token.sh, the App's key, the SSH key and the
# known-hosts file, naming the path and why. A partial set of the four is refused, naming the missing ones.
# With none of them it renders exactly what the single-account mode renders.
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

# Also in the Makefile (AGENTS_TOKEN_LAUNCHD_LABEL_PREFIX) and dev/agents-app-token.sh (stopped): keep the three alike.
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
  check_ssh
}

# trusted_entry <what> <path> and trusted_path <what> <absolute path>: the trusted path rule, as
# dev/agents-app-token.sh applies it at every start of the refresh (keep the two alike): the path and every
# directory above it, up to /, owned by the running uid or root, with no group or other write bit, except a
# root-owned directory with the sticky bit, and no ACL with an allow entry (a deny-only ACL is accepted); a
# symlink's own mode is not checked, and the path's directory is checked again with its symlinks resolved.
trusted_entry() {
  listing="$(ls -ldn "$2" 2>/dev/null)" || die "$1: cannot read the owner and mode of $2 (the trusted path rule)"
  perms="$(printf '%s\n' "${listing}" | awk '{ print $1; exit }')"
  owner="$(printf '%s\n' "${listing}" | awk '{ print $3; exit }')"
  if [ "${owner}" != "${uid}" ] && [ "${owner}" != 0 ]; then
    die "$1: $2 is owned by uid ${owner}, neither this account (uid ${uid}) nor root, so another account controls what runs with the App's key (the trusted path rule, docs/dev-environment.md)"
  fi
  case "${perms}" in
  l*) return 0 ;;
  esac
  # A Mac's ls ends the mode with + when the entry carries an ACL, and with @ when it carries extended attributes,
  # ACL or not (issue #227 part 2, the security audit's second pass), so the ACL is read under either; ls -lde
  # lists its entries, a line each after the long line. An allow entry can grant another account what the mode
  # bits deny, so it is refused; a deny-only ACL (every macOS home carries group:everyone deny delete) grants
  # nothing and is accepted, as is no ACL at all.
  case "${perms}" in
  *+ | *@)
    acl="$(ls -lde "$2" 2>/dev/null)" || die "$1: cannot read the ACL of $2 (the trusted path rule)"
    if printf '%s\n' "${acl}" | sed 1d | grep -Eq '[[:space:]]allow([[:space:]]|$)'; then
      die "$1: $2 carries an ACL with an allow entry, which can let another account replace what runs with the App's key: remove it (chmod -N, or chmod -a for the entry; ls -lde shows it) (the trusted path rule, docs/dev-environment.md)"
    fi
    ;;
  esac
  case "${perms}" in
  ?????????[tT]*) [ "${owner}" != 0 ] || return 0 ;;
  esac
  case "${perms}" in
  ?????w*) die "$1: $2 is group-writable, so another account can replace what runs with the App's key: chmod g-w it (the trusted path rule, docs/dev-environment.md)" ;;
  esac
  case "${perms}" in
  ????????w*) die "$1: $2 is other-writable, so any account can replace what runs with the App's key: chmod o-w it (the trusted path rule, docs/dev-environment.md)" ;;
  esac
}

trusted_path() {
  set -- "$1" "$2" "$2"
  while :; do
    trusted_entry "$1" "$3"
    [ "$3" != / ] || break
    set -- "$1" "$2" "${3%/*}"
    [ -n "$3" ] || set -- "$1" "$2" /
  done
  real="$(cd "${2%/*}/" && pwd -P)" || die "$1: cannot resolve the directory of $2 (the trusted path rule)"
  if [ "${real}" != "${2%/*}" ] && [ -n "${real}" ]; then
    set -- "$1" "$2" "${real}"
    while :; do
      trusted_entry "$1" "$3"
      [ "$3" != / ] || break
      set -- "$1" "$2" "${3%/*}"
      [ -n "$3" ] || set -- "$1" "$2" /
    done
  fi
}

# check_ssh chooses the job's mode as dev/agents-app-token.sh does: SSH mode with all four
# SHIFT2EU_AGENTS_VM_SSH_* set and not empty, the single-account mode with none, a refusal naming the missing
# ones for anything between. In SSH mode it checks each value and applies the trusted path rule.
check_ssh() {
  ssh_mode=0
  missing=""
  unset_count=0
  # The names are this script's own, so the eval reads a variable and never runs a value.
  for name in SHIFT2EU_AGENTS_VM_SSH_PORT SHIFT2EU_AGENTS_VM_SSH_USER SHIFT2EU_AGENTS_VM_SSH_KEY SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS; do
    eval "value=\${${name}:-}"
    if [ -z "${value}" ]; then
      missing="${missing:+${missing}, }${name}"
      unset_count=$((unset_count + 1))
    fi
  done
  value=""
  [ "${unset_count}" -ne 4 ] || return 0
  [ -z "${missing}" ] ||
    die "SSH mode needs all of SHIFT2EU_AGENTS_VM_SSH_PORT, SHIFT2EU_AGENTS_VM_SSH_USER, SHIFT2EU_AGENTS_VM_SSH_KEY and SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS; not set: ${missing}"
  ssh_mode=1
  ssh_port="${SHIFT2EU_AGENTS_VM_SSH_PORT}"
  digits SHIFT2EU_AGENTS_VM_SSH_PORT "${ssh_port}"
  [ "${#ssh_port}" -le 5 ] && [ "${ssh_port}" -ge 1 ] && [ "${ssh_port}" -le 65535 ] ||
    die "SHIFT2EU_AGENTS_VM_SSH_PORT must be a port, 1 to 65535"
  ssh_user="${SHIFT2EU_AGENTS_VM_SSH_USER}"
  case "${ssh_user}" in
  [!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_]* | *[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-]*)
    die "SHIFT2EU_AGENTS_VM_SSH_USER must be letters, digits, ., _ and -, beginning with a letter, digit or _"
    ;;
  esac
  ssh_key="${SHIFT2EU_AGENTS_VM_SSH_KEY}"
  ssh_known_hosts="${SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS}"
  for pair in "SHIFT2EU_AGENTS_VM_SSH_KEY=${ssh_key}" "SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS=${ssh_known_hosts}"; do
    absolute "${pair%%=*}" "${pair#*=}"
    [ ! -L "${pair#*=}" ] && [ -f "${pair#*=}" ] ||
      die "${pair%%=*} does not name a regular file (a symlink is refused): name the file itself"
  done
  # The key and the SSH key: no group or other bit at all (POSIX find tests, the same on macOS and Linux).
  for pair in "SHIFT2EU_AGENTS_APP_KEY_FILE=${key}" "SHIFT2EU_AGENTS_VM_SSH_KEY=${ssh_key}"; do
    [ -z "$(find "${pair#*=}" -prune \( -perm -0040 -o -perm -0020 -o -perm -0010 -o -perm -0004 -o -perm -0002 -o -perm -0001 \) -print)" ] ||
      die "${pair%%=*} names a key with a group or other permission bit (${pair#*=}): chmod 600 it"
  done
  # The install's old symlink check, made as the account: the maintainer's uid cannot look into its home.
  for link in "${clone}/dev" "${clone}/dev/agents-app-token.sh"; do
    [ ! -L "${link}" ] ||
      die "refused: ${link} is a symlink, so the job would run a file outside the clone ${clone}: clone it again (docs/dev-environment.md, The agents App)"
  done
  case "$0" in
  /*) self="$0" ;;
  *) self="$(pwd -P)/$0" ;;
  esac
  trusted_path "this script" "${self}"
  if [ -e "${clone}/dev/agents-app-token.sh" ]; then
    trusted_path "the job's script" "${clone}/dev/agents-app-token.sh"
  fi
  trusted_path SHIFT2EU_AGENTS_APP_KEY_FILE "${key}"
  trusted_path SHIFT2EU_AGENTS_VM_SSH_KEY "${ssh_key}"
  trusted_path SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS "${ssh_known_hosts}"
}

plist() {
  check_inputs
  log="${state}/agents-token-refresh-${instance}.log"
  xdg_entry=''
  if [ -n "${xdg_state_home}" ]; then
    xdg_entry="$(printf '\t\t<key>XDG_STATE_HOME</key>\n\t\t<string>%s</string>' "${xdg_state_home}")"
  fi
  # SSH mode needs no limactl, so no Homebrew on the job's PATH (ADR 0034, update of 2026-11-19, c).
  job_path=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin
  ssh_entries=''
  if [ "${ssh_mode}" -eq 1 ]; then
    job_path=/usr/bin:/bin:/usr/sbin:/sbin
    ssh_entries="$(printf '\t\t<key>%s</key>\n\t\t<string>%s</string>\n' \
      SHIFT2EU_AGENTS_VM_SSH_PORT "${ssh_port}" \
      SHIFT2EU_AGENTS_VM_SSH_USER "${ssh_user}" \
      SHIFT2EU_AGENTS_VM_SSH_KEY "${ssh_key}" \
      SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS "${ssh_known_hosts}")"
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
    "		<string>${job_path}</string>"
    if [ -n "${xdg_entry}" ]; then
      printf '%s\n' "${xdg_entry}"
    fi
    if [ -n "${ssh_entries}" ]; then
      printf '%s\n' "${ssh_entries}"
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
