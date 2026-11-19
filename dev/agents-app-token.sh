#!/bin/sh
# Mints installation tokens of the agents App on the Mac and writes them into the Lima VM (PRD #161
# milestone 1, docs/adr/0031-agents-open-pull-requests-as-a-github-app-with-tokens-minted-on-the-mac.md).
# This script never copies the App's private key into the VM: the VM only ever holds a token that expires
# within the hour. Code run from the workspace on the Mac can still read the key (ADR 0031, Consequences).
#
# Usage: dev/agents-app-token.sh mint      mint one token (make agents-token)
#        dev/agents-app-token.sh refresh   mint now, then again before each token expires, until stopped
#                                          (make agents-token-refresh; in the background,
#                                          make agents-token-refresh-start; at boot, the LaunchDaemon)
#        dev/agents-app-token.sh vm-writer print the VM's writer, the script the VM runs to write the token:
#                                          #!/bin/sh, one comment line, then vm_write below byte for byte.
#                                          make vm-agents-token-ssh installs it in the VM as
#                                          /usr/local/libexec/shift2eu/agents-token-write. It needs no input,
#                                          mints nothing and runs no tool.
#
# Two modes write the token into the VM (issue #227 part 2, ADR 0031's update of 2026-11-19, 3 to 6):
#   - limactl mode, the single-account mode: the refresh runs as the maintainer, from the maintainer's host
#     clone, and runs vm_write in the VM through `limactl shell`. It is on when none of the four
#     SHIFT2EU_AGENTS_VM_SSH_* variables below is set.
#   - SSH mode, the account mode: the refresh runs as the separate macOS account that holds the App's key
#     (_shift2euagents), which cannot reach the maintainer's ~/.lima, so it connects to the VM's pinned SSH
#     port on 127.0.0.1 with a key of its own, which the VM authorises only to run the writer above (its
#     forced command). It is on when all four are set and not empty:
#       SHIFT2EU_AGENTS_VM_SSH_PORT         the VM's pinned SSH port on 127.0.0.1, 1 to 65535
#       SHIFT2EU_AGENTS_VM_SSH_USER         the VM user (letters, digits, ., _ and -, beginning with a
#                                           letter, digit or _)
#       SHIFT2EU_AGENTS_VM_SSH_KEY          the account's SSH key to the VM (absolute, a regular file the
#                                           running uid owns, no group or other bit)
#       SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS  the VM's pinned host key, under the alias shift2eu-vm-<instance>
#                                           (absolute, a regular file the running uid owns)
#     A set between none and all four is refused before anything is minted (status 1). SSH mode runs no
#     limactl at all, and refuses, before it mints, any of the App's key, the SSH key, the known-hosts file and
#     this script itself that fails the trusted path rule: the file and every directory above it, up to /,
#     owned by the running uid or root, with no group or other write bit, except a root-owned directory with
#     the sticky bit (/tmp). The refusal names the path and why, never a file's content.
#
# Inputs, from the direnv .envrc of the maintainer's host clone (outside the workspace, where the VM cannot
# reach), never from the workspace or the .envrc above it (docs/dev-environment.md, The host clone):
#   SHIFT2EU_AGENTS_APP_ID               the App ID
#   SHIFT2EU_AGENTS_APP_INSTALLATION_ID  the ID of the App's installation on this repository
#   SHIFT2EU_AGENTS_APP_KEY_FILE         the path of the App's PEM private key: a regular file, not a symlink,
#                                        readable only by its owner
# and, for refresh:
#   SHIFT2EU_AGENTS_TOKEN_CHECK_SECONDS  how often the wall clock is compared with the expiry (default 60)
#   SHIFT2EU_AGENTS_TOKEN_MARGIN_SECONDS mint again when fewer seconds than this remain (default 1200)
# Hooks the tests use: SHIFT2EU_AGENTS_GITHUB_API_URL (default https://api.github.com; plain http only for
# 127.0.0.1 and localhost, and never with an @, a quote, a backslash or whitespace), LIMACTL (default
# limactl), SHIFT2EU_AGENTS_SSH (the ssh SSH mode runs, default ssh), LIMA_INSTANCE (default shift2eu; in SSH
# mode it names only the host key alias, the lock, the log and the messages), SHIFT2EU_VM_REAL_GH_PATH (where the VM keeps the real gh once
# it has the gh wrapper, default /usr/local/libexec/shift2eu/gh; mint warns when it is missing) and
# XDG_STATE_HOME (refresh keeps its lock in ${XDG_STATE_HOME:-$HOME/.local/state}/shift2eu).
#
# refresh tells failures apart. A transient one (GitHub unreachable or answering 5xx or 429, a 403 that is a
# rate limit, limactl failing or the VM not running) never stops it, even with no valid token left: it says
# so in one line and tries again, at every check at first and then less often, at most every 5 checks. A rate
# limit is waited out instead: never earlier than its retry-after, nor, when it says no request is left
# (x-ratelimit-remaining 0), than its x-ratelimit-reset, each honoured up to 3600 seconds. A permanent one
# (GitHub refusing the JWT, the installation, the App or the bot user with any other 4xx, a key that cannot
# sign, a VM that refuses to write through a symlink) stops it with "agents App token refresh stopped" and
# status 1. One refresher runs per VM instance: a second one finds the first one's lock, says so, and exits 0.
#
# What it writes into the VM user's home, each file mode 0600 in a 0700 directory, replaced atomically:
#   ~/.config/shift2eu/agents-app-token     the token, then its expiry as Unix seconds
#   ~/.config/shift2eu/agents-app-identity  the git user.name <slug>[bot], then the git user.email
#                                           <bot user id>+<slug>[bot]@users.noreply.github.com
#   ~/.gitconfig                            user.name and user.email set to that identity, whenever the
#                                           identity is written (git config; issue #241)
#
# The token is written into the VM as soon as it is minted. The bot user is looked up after that, with the
# installation token, never with the JWT or without one, and its identity file is written once the lookup
# works: a failed lookup never discards the token, and refresh retries it at its next check, with the token
# it holds, without minting another one. The identity is kept for the life of the process.
#
# Secrets: the JWT and the token live only in shell variables. They reach curl through its standard input
# (--config -) and the VM through limactl's or ssh's standard input, never a command line, a file on the Mac,
# or this script's output. Errors name what failed, never a value.
set -eu

api="${SHIFT2EU_AGENTS_GITHUB_API_URL:-https://api.github.com}"
limactl="${LIMACTL:-limactl}"
ssh="${SHIFT2EU_AGENTS_SSH:-ssh}"
mode=limactl
instance="${LIMA_INSTANCE:-shift2eu}"
vm_real_gh="${SHIFT2EU_VM_REAL_GH_PATH:-/usr/local/libexec/shift2eu/gh}"

# nl is a newline, for the parameter expansions that split curl's answer and the URL check.
nl="$(printf '\nx')"
nl="${nl%x}"

say() { printf 'agents-token: %s\n' "$*" >&2; }
die() {
  say "$*"
  exit 1
}

# now prints the wall clock as Unix seconds.
now() { date +%s; }

# show_time <unix seconds> prints the time in UTC, with GNU date (Linux) or BSD date (macOS).
show_time() {
  date -u -d "@$1" '+%Y-%m-%d %H:%M:%S UTC' 2>/dev/null ||
    date -u -r "$1" '+%Y-%m-%d %H:%M:%S UTC' 2>/dev/null ||
    printf '%s (Unix seconds)\n' "$1"
}

# parse_time <RFC 3339 UTC time> prints it as Unix seconds, or fails. BSD date (macOS) first: GNU date has
# no -j and fails at once, while an old BSD date read -d as another option.
parse_time() {
  date -u -j -f '%Y-%m-%dT%H:%M:%SZ' "$1" +%s 2>/dev/null ||
    date -u -d "$1" +%s 2>/dev/null
}

is_number() {
  case "$1" in
  '' | *[!0-9]*) return 1 ;;
  *) return 0 ;;
  esac
}

# decimal <digits> prints the number without its leading zeros, 0 for zeros only. Every number from outside
# the script (a header, the VM's clock, a setting) goes through it before it reaches $(( )), where a leading
# 0 reads as octal and a digit 8 or 9 then ends the shell ("Illegal number" in dash).
decimal() {
  set -- "${1#"${1%%[!0]*}"}"
  printf '%s' "${1:-0}"
}

check_inputs() {
  is_number "${SHIFT2EU_AGENTS_APP_ID:-}" ||
    die "SHIFT2EU_AGENTS_APP_ID is not set, or not a number: export the agents App's ID from the .envrc of your host clone, outside the workspace (docs/dev-environment.md, The agents App)"
  is_number "${SHIFT2EU_AGENTS_APP_INSTALLATION_ID:-}" ||
    die "SHIFT2EU_AGENTS_APP_INSTALLATION_ID is not set, or not a number: export the ID of the App's installation from the .envrc of your host clone, outside the workspace"
  key="${SHIFT2EU_AGENTS_APP_KEY_FILE:-}"
  [ -n "${key}" ] ||
    die "SHIFT2EU_AGENTS_APP_KEY_FILE is not set: export the path of the App's private key, kept outside the workspace, from the .envrc of your host clone, outside the workspace"
  [ ! -L "${key}" ] ||
    die "SHIFT2EU_AGENTS_APP_KEY_FILE names a symlink: name the key file itself, a regular file of mode 0600"
  [ -f "${key}" ] && [ -r "${key}" ] ||
    die "SHIFT2EU_AGENTS_APP_KEY_FILE does not name a readable file"
  # Only the owner may read or write the key: chmod 600 it. POSIX find tests, the same on macOS and Linux.
  case "${key}" in
  /*) key_path="${key}" ;;
  *) key_path="./${key}" ;;
  esac
  [ -z "$(find "${key_path}" -prune \( -perm -0040 -o -perm -0020 -o -perm -0010 -o -perm -0004 -o -perm -0002 -o -perm -0001 \) -print)" ] ||
    die "SHIFT2EU_AGENTS_APP_KEY_FILE names a key other accounts can read, write or run: chmod 600 it"
  [ -n "$(find "${key_path}" -prune -user "$(id -u)" -print)" ] ||
    die "SHIFT2EU_AGENTS_APP_KEY_FILE names a key another account owns"

  check_ssh
  check_api_url
  is_number "${check_seconds}" && [ "${check_seconds}" -gt 0 ] ||
    die "SHIFT2EU_AGENTS_TOKEN_CHECK_SECONDS must be a positive number of seconds"
  is_number "${margin_seconds}" || die "SHIFT2EU_AGENTS_TOKEN_MARGIN_SECONDS must be a number of seconds"
  check_seconds="$(decimal "${check_seconds}")"
  margin_seconds="$(decimal "${margin_seconds}")"
}

# refuse <why> refuses the configuration before anything is minted, as a permanent failure: status 1, and under
# refresh the stop line too, so the log says the refresh stopped and where to start it again.
refuse() {
  say "$*"
  if [ "${command}" = refresh ]; then
    stopped "the configuration is refused"
  fi
  exit 1
}

# trusted_entry <what> <path> refuses one entry that fails the trusted path rule (ADR 0031, update of
# 2026-11-19, 2): owned by the running uid or root, and no group or other write bit, unless it is a root-owned
# directory with the sticky bit, where no one else can rename an entry (/tmp). A symlink's own mode means
# nothing, so only its owner is checked; trusted_path checks where it leads. ls -ldn, not stat: a stock Mac has
# no GNU stat, and the two print the same first and third fields.
trusted_entry() {
  listing="$(ls -ldn "$2" 2>/dev/null)" || refuse "$1: cannot read the owner and mode of $2 (the trusted path rule)"
  perms="$(printf '%s\n' "${listing}" | awk '{ print $1; exit }')"
  owner="$(printf '%s\n' "${listing}" | awk '{ print $3; exit }')"
  if [ "${owner}" != "${uid}" ] && [ "${owner}" != 0 ]; then
    refuse "$1: $2 is owned by uid ${owner}, neither this account (uid ${uid}) nor root, so another account controls what runs with the App's key (the trusted path rule, docs/dev-environment.md)"
  fi
  case "${perms}" in
  l*) return 0 ;;
  ?????????[tT]*) [ "${owner}" != 0 ] || return 0 ;;
  esac
  case "${perms}" in
  ?????w*) refuse "$1: $2 is group-writable, so another account can replace what runs with the App's key: chmod g-w it (the trusted path rule, docs/dev-environment.md)" ;;
  esac
  case "${perms}" in
  ????????w*) refuse "$1: $2 is other-writable, so any account can replace what runs with the App's key: chmod o-w it (the trusted path rule, docs/dev-environment.md)" ;;
  esac
}

# trusted_path <what> <absolute path> applies the trusted path rule to the path and every directory above it, up
# to /, as written, then to the directory it is in with its symlinks resolved (pwd -P), so a symlink on the way
# (macOS's /tmp, /var) is followed to where it leads, and that is checked too.
trusted_path() {
  set -- "$1" "$2" "$2"
  while :; do
    trusted_entry "$1" "$3"
    [ "$3" != / ] || break
    set -- "$1" "$2" "${3%/*}"
    [ -n "$3" ] || set -- "$1" "$2" /
  done
  real="$(cd "${2%/*}/" && pwd -P)" || refuse "$1: cannot resolve the directory of $2 (the trusted path rule)"
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

# own_file <variable> <path> refuses a path SSH mode reads as anything but an absolute path to a regular file,
# not a symlink, that the running uid owns.
own_file() {
  case "$2" in
  /*) ;;
  *) refuse "$1 must be an absolute path" ;;
  esac
  [ ! -L "$2" ] || refuse "$1 names a symlink ($2): name the file itself"
  [ -f "$2" ] || refuse "$1 does not name a regular file ($2)"
  [ -n "$(find "$2" -prune -user "${uid}" -print)" ] || refuse "$1 names a file another account owns ($2)"
}

# no_access <variable> <path> refuses a key any other account can read, write or run: chmod 600 it.
no_access() {
  [ -z "$(find "$2" -prune \( -perm -0040 -o -perm -0020 -o -perm -0010 -o -perm -0004 -o -perm -0002 -o -perm -0001 \) -print)" ] ||
    refuse "$1 names a key with a group or other permission bit ($2), which other accounts can read: chmod 600 it"
}

# check_ssh chooses the mode (ADR 0031, update of 2026-11-19, 5): SSH mode when all four SHIFT2EU_AGENTS_VM_SSH_*
# are set and not empty, limactl mode when none is, and a refusal for anything between, naming those not set.
# In SSH mode it checks each value, naming the variable and never its value, and applies the trusted path rule
# to the App's key, the SSH key, the known-hosts file and this script.
check_ssh() {
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
  if [ "${unset_count}" -eq 4 ]; then
    mode=limactl
    return 0
  fi
  [ -z "${missing}" ] ||
    refuse "SSH mode needs all of SHIFT2EU_AGENTS_VM_SSH_PORT, SHIFT2EU_AGENTS_VM_SSH_USER, SHIFT2EU_AGENTS_VM_SSH_KEY and SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS; not set: ${missing}"
  mode=ssh
  ssh_port="${SHIFT2EU_AGENTS_VM_SSH_PORT}"
  is_number "${ssh_port}" && [ "${#ssh_port}" -le 5 ] && ssh_port="$(decimal "${ssh_port}")" &&
    [ "${ssh_port}" -ge 1 ] && [ "${ssh_port}" -le 65535 ] ||
    refuse "SHIFT2EU_AGENTS_VM_SSH_PORT must be a port, 1 to 65535 in digits"
  ssh_user="${SHIFT2EU_AGENTS_VM_SSH_USER}"
  case "${ssh_user}" in
  [!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_]* | *[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-]*)
    refuse "SHIFT2EU_AGENTS_VM_SSH_USER must be letters, digits, ., _ and -, beginning with a letter, digit or _"
    ;;
  esac
  ssh_key="${SHIFT2EU_AGENTS_VM_SSH_KEY}"
  ssh_known_hosts="${SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS}"
  uid="$(id -u)" || refuse "cannot tell which account runs this (id -u failed)"
  own_file SHIFT2EU_AGENTS_VM_SSH_KEY "${ssh_key}"
  own_file SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS "${ssh_known_hosts}"
  no_access SHIFT2EU_AGENTS_VM_SSH_KEY "${ssh_key}"
  case "${key}" in
  /*) ;;
  *) refuse "SHIFT2EU_AGENTS_APP_KEY_FILE must be an absolute path in SSH mode" ;;
  esac
  case "$0" in
  /*) self="$0" ;;
  *) self="$(pwd -P)/$0" ;;
  esac
  [ ! -L "${self}" ] || refuse "this script is a symlink (${self}): run the account's clone's dev/agents-app-token.sh itself"
  trusted_path "this script" "${self}"
  trusted_path SHIFT2EU_AGENTS_APP_KEY_FILE "${key}"
  trusted_path SHIFT2EU_AGENTS_VM_SSH_KEY "${ssh_key}"
  trusted_path SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS "${ssh_known_hosts}"
}

# check_api_url refuses an API URL that could send the JWT anywhere but GitHub: an @ (curl reads what comes
# before it as a user name, so http://localhost:x@evil.example/ goes to evil.example), a quote, a backslash or
# whitespace (each could end the url line of curl's --config and add a directive), plain http to anything but
# this machine, and no https at all. It sets proto, the only scheme curl may use.
check_api_url() {
  tab="$(printf '\t')"
  case "${api}" in
  *@* | *\"* | *\\* | *" "* | *"${tab}"* | *"${nl}"*)
    die "SHIFT2EU_AGENTS_GITHUB_API_URL must not contain an @, a quote, a backslash or whitespace"
    ;;
  esac
  if printf '%s\n' "${api}" | grep -Eqx 'http://(127\.0\.0\.1|localhost)(:[0-9]+)?(/.*)?'; then
    proto="=http"
  else
    case "${api}" in
    https://?*) proto="=https" ;;
    *) die "SHIFT2EU_AGENTS_GITHUB_API_URL must be an https URL (plain http only for 127.0.0.1 and localhost)" ;;
    esac
  fi
  api="${api%/}"
}

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

# make_jwt sets jwt: an RS256 JWT for the App, valid from a minute ago (clock drift) for ten minutes, the
# longest GitHub accepts.
make_jwt() {
  issued="$(($(now) - 60))"
  head="$(printf '%s' '{"alg":"RS256","typ":"JWT"}' | b64url)"
  claims="$(printf '{"iat":%s,"exp":%s,"iss":"%s"}' "${issued}" "$((issued + 600))" "${SHIFT2EU_AGENTS_APP_ID}" | b64url)"
  signature="$(printf '%s.%s' "${head}" "${claims}" | openssl dgst -sha256 -sign "${key}" -binary | b64url)" ||
    return 1
  [ -n "${signature}" ] || return 1
  jwt="${head}.${claims}.${signature}"
}

# call <method> <path> <bearer> sets status, body, and the rate limit headers remaining (x-ratelimit-remaining),
# reset (x-ratelimit-reset, Unix seconds) and retry_after (retry-after), each empty unless a number. The bearer
# is the JWT or the installation token.
# The headers, the URL and the method reach curl on its standard input, so neither secret is ever on a
# command line. -q comes first, so curl reads no ~/.curlrc that could log the Authorization header or send
# the request elsewhere. %header{} needs curl 7.84 or later; an older curl prints no number there, which
# reads as no rate limit header.
call() {
  remaining=""
  reset=""
  retry_after=""
  response="$(
    printf '%s\n' \
      "url = \"${api}$2\"" \
      "request = \"$1\"" \
      "header = \"Authorization: Bearer $3\"" \
      'header = "Accept: application/vnd.github+json"' \
      'header = "X-GitHub-Api-Version: 2022-11-28"' |
      curl -q --silent --show-error --max-time 30 --proto "${proto}" \
        --write-out '\n%header{x-ratelimit-remaining}\n%header{x-ratelimit-reset}\n%header{retry-after}\n%{http_code}' --config -
  )" || {
    status=000
    body=""
    return 0
  }
  status="${response##*"${nl}"}"
  response="${response%"${nl}"*}"
  header="${response##*"${nl}"}"
  ! is_number "${header}" || retry_after="$(decimal "${header}")"
  response="${response%"${nl}"*}"
  header="${response##*"${nl}"}"
  ! is_number "${header}" || reset="$(decimal "${header}")"
  response="${response%"${nl}"*}"
  header="${response##*"${nl}"}"
  ! is_number "${header}" || remaining="$(decimal "${header}")"
  body="${response%"${nl}"*}"
}

# json_string <name> prints the value of the string field <name> of body. The greedy .* makes it the last
# such field, which for GitHub's flat answers here is the only one; the callers check its characters.
json_string() {
  printf '%s' "${body}" | tr -d '\n' | sed -n "s/.*\"$1\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p"
}

# json_number <name> prints the value of the number field <name> of body: the last one, as for json_string.
json_number() {
  printf '%s' "${body}" | tr -d '\n' | sed -n "s/.*\"$1\"[[:space:]]*:[[:space:]]*\\([0-9][0-9]*\\).*/\\1/p"
}

# rate_limited tells whether the last answer was GitHub's rate limit: a 403 (or 429) whose body mentions a
# rate limit, or whose headers say no request is left or when to try again.
rate_limited() {
  [ "${remaining}" = 0 ] || [ -n "${retry_after}" ] ||
    printf '%s' "${body}" | grep -qi 'rate limit'
}

# classify <HTTP status> sets failure: transient for what goes away by itself (000: no answer at all, 408,
# 429, 5xx, a 403 that is a rate limit), permanent for any other refusal, which only the maintainer can fix
# (401: the JWT, 403, 404: the App or its installation, any other 4xx). For a transient one it sets wait_hint
# to the seconds GitHub asked to wait, the later of the retry-after and, when no request is left, the
# x-ratelimit-reset (a second after it, as reset is rounded down), at most 3600; empty when it asked for none.
classify() {
  wait_hint=""
  case "$1" in
  000 | 408 | 429 | 5??) failure=transient ;;
  403)
    if rate_limited; then
      failure=transient
    else
      failure=permanent
    fi
    ;;
  *) failure=permanent ;;
  esac
  [ "${failure}" = transient ] || return 0
  # At most 5 digits for a retry-after and 12 for a reset (the clock's own length): a longer one is waited
  # out for the full 3600 seconds, and never reaches the shell's arithmetic, where it could overflow.
  if is_number "${retry_after}"; then
    if [ "${#retry_after}" -gt 5 ]; then
      wait_hint=3600
    elif [ "${retry_after}" -gt 0 ]; then
      wait_hint="${retry_after}"
    fi
  fi
  if [ "${remaining}" = 0 ] && is_number "${reset}"; then
    if [ "${#reset}" -gt 12 ]; then
      wait_hint=3600
    else
      until_reset="$((reset - $(now) + 1))"
      if [ "${until_reset}" -gt "${wait_hint:-0}" ]; then
        wait_hint="${until_reset}"
      fi
    fi
  fi
  if [ -n "${wait_hint}" ] && [ "${wait_hint}" -gt 3600 ]; then
    wait_hint=3600
  fi
}

# lookup_slug sets slug from GET /app, with the App's JWT. A slug is letters, digits and -, and begins with a
# letter or digit, as the VM's name check wants it: anything else is refused before a token is minted, so the
# bot name <slug>[bot] never reaches git config shaped like an option (issue #241). A malformed slug is a
# permanent failure (GitHub's answer will not fix itself, and the refresh stops on it); an empty one is
# transient, an answer that said nothing yet.
lookup_slug() {
  call GET /app "${jwt}"
  [ "${status}" = 200 ] || {
    classify "${status}"
    say "GET /app answered HTTP ${status}: cannot read the agents App's slug with SHIFT2EU_AGENTS_APP_ID and its key"
    return 1
  }
  slug="$(json_string slug)"
  case "${slug}" in
  '')
    failure=transient
    say "GET /app gave no slug: cannot name the bot yet"
    return 1
    ;;
  [!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789]* | *[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-]*)
    failure=permanent
    slug=""
    say "GET /app gave a slug no App has: the bot name would not be an App bot name (<slug>[bot], letters, digits and -, beginning with a letter or digit)"
    return 1
    ;;
  esac
}

# lookup_identity sets bot_name and bot_email from GET /users/<slug>[bot] (the bot user's id), asked with the
# installation token minted last (held): GitHub refuses the App's JWT there, and the token has a rate limit of
# its own, where an unauthenticated call shares one with everything behind the same address. GitHub's noreply
# address for the bot is numbered with the bot user's id, not the App ID. On failure it sets failure, and
# wait_hint for a rate limit, as classify does.
lookup_identity() {
  call GET "/users/${slug}%5Bbot%5D" "${held}"
  [ "${status}" = 200 ] || {
    classify "${status}"
    if [ "${failure}" = transient ] && [ "${status}" = 403 ]; then
      say "GET /users/${slug}[bot] answered HTTP 403, a rate limit: cannot read the bot user's id yet"
    else
      say "GET /users/${slug}[bot] answered HTTP ${status}: cannot read the bot user's id"
    fi
    return 1
  }
  bot_id="$(json_number id)"
  is_number "${bot_id}" || {
    failure=transient
    wait_hint=""
    say "GET /users/${slug}[bot] gave no numeric id"
    return 1
  }
  bot_name="${slug}[bot]"
  bot_email="${bot_id}+${slug}[bot]@users.noreply.github.com"
}

# The script the VM runs: it reads four lines from its standard input (the token, its expiry, the git
# user.name, the git user.email) and writes the identity file when the name is not empty and the token file
# when the token is not empty, each atomically (a temporary file beside it, then mv), so a gh running at the
# same time never reads half a token. With the identity it also sets the VM user's global git identity
# (user.name and user.email in ~/.gitconfig, replacing every value there, one value per key), so the agents
# commit as the bot even when vm-init ran before the first mint (issue #241). Once the bot identity is known
# every mint sends it, so every mint of a refresh applies it again; only a mint before the bot user's lookup
# has worked (a process's first one, or one while the lookup keeps failing) writes the token alone, and such
# a write never touches the git configuration. First, without any lock, it reads ~/.gitconfig itself (with
# its [include] files followed): an [includeIf] is refused, and a file that already holds exactly one
# user.name and one user.email, the bot's, is left untouched (same inode, same mode) and no lock is looked at,
# so a stale ~/.gitconfig.lock never blocks the mints after the first. Only when the identity needs writing
# does it take git's own lock: an existing ~/.gitconfig.lock that is a symlink or not a regular file is no lock
# git wrote and is refused with status 3 (never opened, followed or removed); a regular one belongs to another
# git writer, which the script leaves alone, exiting with status 4 before it writes anything, which the host
# reads as transient (such a lock is held for a moment; the next mint tries again, and when no git runs in the
# VM it is a crashed git's, for the maintainer to remove). Otherwise the script takes the lock with a primitive
# that never opens or follows what is there: it creates its own file (mktemp, mode 0600) beside ~/.gitconfig
# and hard-links it to ~/.gitconfig.lock (ln -T: link(2) fails on any existing entry, a symlink, FIFO or
# directory planted after the check included, which is then classified as above), then drops the temporary
# name. It copies ~/.gitconfig into that lock; the copy is checked again (the file may have changed in
# between): an [includeIf] is refused, a copy already holding the bot's identity is dropped, and
# else the copy gets the two keys (--replace-all), git reads the identity back from it, and only when that is
# the bot's does the copy replace ~/.gitconfig (mv); otherwise ~/.gitconfig is left as it was. The lock is
# removed on every way out but the mv, and only ever the one this script created: the traps are set before the
# lock is taken, and the path they remove is set only while the script holds the lock (or, before the link,
# its own temporary file, which is removed on every way out too). A git that cannot read
# ~/.gitconfig's includes (any status but 1, no match) is refused with status 3, never taken for none. It
# refuses, with status 3, before it writes anything: a ~/.config/shift2eu that is a symlink or that resolves
# (pwd -P) outside the VM user's real home, a token or identity file or a ~/.gitconfig that is a symlink or
# not a regular file, a name or email that is not a bot's (the patterns of dev/vm-git-identity.sh, with a
# slug that begins with a letter or digit), and, with an identity, a VM without git (its image always has
# git, so a missing one is a broken VM that would leave the agents committing as someone else: refused
# loudly, not skipped), a ~/.gitconfig it cannot read, git failing to set the keys, an [include] in
# ~/.gitconfig that would override the bot's name or address (the effective identity a commit would use is not
# the bot's), and any [includeIf] (the check runs outside every repository, where git never evaluates a
# conditional include, while the agents commit inside the workspace's, where it applies). Values read out of
# the VM's git configuration are printed with their non-printable characters stripped. That keeps the token
# from being written through a symlink by mistake; it is no boundary against the VM user, who can read the
# token anyway and could swap a path between the check and the mv.
# It then prints, never a secret, git-identity=set when it replaced ~/.gitconfig with the bot's identity, or
# git-identity=unchanged when ~/.gitconfig already held it and was left untouched (issue #263), the VM's clock as
# clock=<Unix seconds> and, when the real gh is not where the gh wrapper keeps it ($1), gh-wrapper=missing:
# a VM created before the wrapper.
# shellcheck disable=SC2016 # expanded by the VM's sh, not here
vm_write='set -eu
umask 077
dir="$HOME/.config/shift2eu"
if [ -L "$dir" ]; then
  echo "agents-token: $dir is a symlink; the VM writes no token through it: remove it in the VM" >&2
  exit 3
fi
home="$(cd "$HOME" && pwd -P)"
inside() {
  [ ! -e "$1" ] && [ ! -L "$1" ] && return 0
  real="$(cd "$1" && pwd -P)" || return 1
  case "$real/" in
  "$home"/*) return 0 ;;
  *) return 1 ;;
  esac
}
for d in "$HOME/.config" "$dir"; do
  if ! inside "$d"; then
    echo "agents-token: $d resolves outside $HOME; the VM writes no token there: make it a directory in the VM user home" >&2
    exit 3
  fi
done
gitconfig="$HOME/.gitconfig"
if [ -L "$gitconfig" ] || { [ -e "$gitconfig" ] && [ ! -f "$gitconfig" ]; }; then
  echo "agents-token: $gitconfig is a symlink or not a regular file; the VM writes no token and no git identity: make it a regular file (or remove it) in the VM" >&2
  exit 3
fi
mkdir -p "$dir"
if ! inside "$dir"; then
  echo "agents-token: $dir resolves outside $HOME; the VM writes no token there: make it a directory in the VM user home" >&2
  exit 3
fi
chmod 700 "$dir"
for name in agents-app-identity agents-app-token; do
  if [ -L "$dir/$name" ] || { [ -e "$dir/$name" ] && [ ! -f "$dir/$name" ]; }; then
    echo "agents-token: $dir/$name is a symlink or not a regular file; the VM writes no token there: remove it in the VM" >&2
    exit 3
  fi
done
IFS= read -r token
IFS= read -r expiry
IFS= read -r name
IFS= read -r email
slug_only() {
  case "$1" in
  [!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789]*) return 1 ;;
  *[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-]*) return 1 ;;
  "") return 1 ;;
  esac
}
if [ -n "$name" ]; then
  name_slug="${name%"[bot]"}"
  if [ "$name_slug" = "$name" ] || ! slug_only "$name_slug"; then
    echo "agents-token: the bot name is not an App bot name (<slug>[bot]); the VM writes no git identity" >&2
    exit 3
  fi
  email_rest="${email%"[bot]@users.noreply.github.com"}"
  email_id="${email_rest%%+*}"
  email_slug="${email_rest#*+}"
  if [ "$email_rest" = "$email" ] || [ "$email_id" = "$email_rest" ] || ! slug_only "$email_slug"; then
    echo "agents-token: the bot email is not a bot noreply address; the VM writes no git identity" >&2
    exit 3
  fi
  case "$email_id" in
  "" | *[!0123456789]*)
    echo "agents-token: the bot email is not a bot noreply address; the VM writes no git identity" >&2
    exit 3
    ;;
  esac
  if ! command -v git >/dev/null 2>&1; then
    echo "agents-token: git is not installed in the VM, so the agents cannot commit as the bot: install it (make vm-init)" >&2
    exit 3
  fi
  printable() {
    printf "%s" "$1" | tr -cd "[:print:]"
  }
  no_conditional() {
    found=0
    conditional="$(git config --includes --file "$1" --name-only --get-regexp "^includeif\..*\.path$")" || found=$?
    if [ "$found" -ne 0 ] && [ "$found" -ne 1 ]; then
      echo "agents-token: cannot read the includes of $gitconfig (git config exited $found), so the VM writes no token and no git identity: fix $gitconfig in the VM" >&2
      exit 3
    fi
    if [ -n "$conditional" ]; then
      conditional="$(printf "%s\n" "$conditional" | tr -cd "[:print:]\n" | tr "\n" " ")"
      echo "agents-token: $gitconfig has a conditional include ($conditional); git applies an includeIf only inside the repositories it matches, so the identity a commit in the workspace would use cannot be checked, and $gitconfig is unchanged: remove the includeIf in the VM" >&2
      exit 3
    fi
  }
  is_bots() {
    got_name="$(git config --includes --file "$1" --get-all user.name)" || got_name=""
    got_email="$(git config --includes --file "$1" --get-all user.email)" || got_email=""
    [ "$got_name" = "$name" ] && [ "$got_email" = "$email" ]
  }
  lock="$gitconfig.lock"
  gitnew=""
  trap '\''rm -f "$gitnew"'\'' EXIT
  trap '\''exit 1'\'' HUP INT TERM
  if [ -e "$gitconfig" ]; then
    no_conditional "$gitconfig"
  fi
  identity=unchanged
  if [ -e "$gitconfig" ] && is_bots "$gitconfig"; then
    :
  else
    if [ -e "$lock" ] || [ -L "$lock" ]; then
      if [ -L "$lock" ] || [ ! -f "$lock" ]; then
        echo "agents-token: $lock is a symlink or not a regular file, so no lock git wrote: the VM writes no token and no git identity, and leaves it alone: remove $lock in the VM" >&2
        exit 3
      fi
      echo "agents-token: $lock exists: another git writer holds the lock on $gitconfig, so the VM writes no token and no git identity now; the next mint tries again. If no git is running in the VM, remove $lock there (a crashed git leaves it behind)" >&2
      exit 4
    fi
    if ! locktmp="$(mktemp "$HOME/.gitconfig.agents-token.XXXXXX")"; then
      echo "agents-token: cannot create the lock $lock, so the VM writes no git identity: fix the VM user home" >&2
      exit 3
    fi
    gitnew="$locktmp"
    chmod 600 "$locktmp"
    # An entry planted after the pre-check above: TestAgentsAppTokenMintRefusesAnEntryPlantedAfterTheLockPreCheck.
    if ! ln -T "$locktmp" "$lock" 2>/dev/null; then
      rm -f "$locktmp"
      gitnew=""
      if [ -L "$lock" ] || { [ -e "$lock" ] && [ ! -f "$lock" ]; }; then
        echo "agents-token: $lock is a symlink or not a regular file, so no lock git wrote: the VM writes no token and no git identity, and leaves it alone: remove $lock in the VM" >&2
        exit 3
      fi
      if [ -e "$lock" ]; then
        echo "agents-token: $lock exists: another git writer holds the lock on $gitconfig, so the VM writes no token and no git identity now; the next mint tries again. If no git is running in the VM, remove $lock there (a crashed git leaves it behind)" >&2
        exit 4
      fi
      echo "agents-token: cannot create the lock $lock, so the VM writes no git identity: fix the VM user home" >&2
      exit 3
    fi
    gitnew="$lock"
    rm -f "$locktmp"
    if [ -e "$gitconfig" ] && ! cat "$gitconfig" >"$gitnew"; then
      echo "agents-token: cannot read $gitconfig, so the VM writes no git identity: make it readable by the VM user" >&2
      exit 3
    fi
    no_conditional "$gitnew"
    if is_bots "$gitnew"; then
      rm -f "$gitnew"
      gitnew=""
    else
      if ! git config --file "$gitnew" --replace-all user.name "$name" || ! git config --file "$gitnew" --replace-all user.email "$email"; then
        echo "agents-token: git could not set the bot identity in a copy of $gitconfig, which is unchanged: fix that file in the VM" >&2
        exit 3
      fi
      got_name="$(git config --includes --file "$gitnew" --get user.name)" || got_name=""
      got_email="$(git config --includes --file "$gitnew" --get user.email)" || got_email=""
      if [ "$got_name" != "$name" ] || [ "$got_email" != "$email" ]; then
        echo "agents-token: with the bot identity set, the effective git identity in the VM would still be $(printable "$got_name") <$(printable "$got_email")>, from an [include] in $gitconfig that sets user.name or user.email after it; $gitconfig is unchanged: remove that include in the VM" >&2
        exit 3
      fi
      mv -f "$gitnew" "$gitconfig"
      gitnew=""
      identity=set
    fi
  fi
  trap - EXIT
  if [ "$identity" = set ]; then
    printf "git-identity=set\n"
  else
    printf "git-identity=unchanged\n"
  fi
  tmp="$(mktemp "$dir/.agents-app-identity.XXXXXX")"
  printf "%s\n%s\n" "$name" "$email" >"$tmp"
  chmod 600 "$tmp"
  mv -f "$tmp" "$dir/agents-app-identity"
fi
if [ -n "$token" ]; then
  tmp="$(mktemp "$dir/.agents-app-token.XXXXXX")"
  printf "%s\n%s\n" "$token" "$expiry" >"$tmp"
  chmod 600 "$tmp"
  mv -f "$tmp" "$dir/agents-app-token"
fi
printf "clock=%s\n" "$(date +%s)"
[ -e "$1" ] || printf "gh-wrapper=missing\n"'

# check_vm <what vm_write printed> warns about a VM created before the gh wrapper, whose gh ignores the
# token, and about a VM clock more than a minute off the Mac's, which shortens or stretches the token's life
# in the VM.
check_vm() {
  case "$1" in
  *gh-wrapper=missing*)
    say "warning: the VM ${instance} has no ${vm_real_gh}: this VM predates the gh wrapper, so its gh and git push ignore the token and have no GitHub credential of the agents. Upgrade it with make vm-gh-wrapper (or make vm-delete vm-create)"
    ;;
  esac
  # 1 to 12 digits only: a longer reading is no clock, and could overflow the shell's arithmetic.
  vm_clock="$(printf '%s\n' "$1" | sed -n 's/^clock=\([0-9]\{1,12\}\)$/\1/p')"
  if is_number "${vm_clock}"; then
    vm_clock="$(decimal "${vm_clock}")"
    offset="$((vm_clock - $(now)))"
    if [ "${offset}" -gt 60 ] || [ "${offset}" -lt -60 ]; then
      say "warning: the VM ${instance}'s clock is ${offset}s off the Mac's; the token's expiry is the Mac's, so the VM may refuse it early or keep it past its life. Restart the VM's time sync (or make vm-stop vm-up)"
    fi
  fi
}

# said_git_identity says the VM user's global git identity is now the bot's, when vm_write printed that it
# set it (git-identity=set): the agents commit as the bot from here on (issue #241). When ~/.gitconfig already
# held it (git-identity=unchanged) it says so instead, so the refresher's log claims no change it did not make
# (issue #263).
said_git_identity() {
  case "${written}" in
  *git-identity=set*)
    say "set the VM ${instance}'s git identity (user.name and user.email in ~/.gitconfig) to ${bot_name} <${bot_email}>"
    ;;
  *git-identity=unchanged*)
    say "the VM ${instance}'s git identity is already ${bot_name} <${bot_email}> (~/.gitconfig unchanged)"
    ;;
  esac
}

# vm_ssh runs the VM's writer over SSH (ADR 0031, update of 2026-11-19, 3): the ssh SHIFT2EU_AGENTS_SSH names,
# with exactly these options and nothing else on its command line. -F /dev/null reads no ssh_config; the host
# key is checked strictly against the account's pinned file under the instance's alias, so a port change needs
# no new pin; no agent, no forwarding, no multiplexing. agents-token-write is a placeholder: the VM's forced
# command runs the writer instead, whatever the client asks for. The four lines arrive on standard input.
vm_ssh() {
  "${ssh}" -F /dev/null -T -p "${ssh_port}" -l "${ssh_user}" -i "${ssh_key}" \
    -o BatchMode=yes -o IdentitiesOnly=yes -o IdentityAgent=none \
    -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=${ssh_known_hosts}" -o GlobalKnownHostsFile=/dev/null \
    -o "HostKeyAlias=shift2eu-vm-${instance}" -o CheckHostIP=no -o UpdateHostKeys=no \
    -o ControlMaster=no -o ControlPath=none -o ForwardAgent=no -o ForwardX11=no \
    -o ClearAllForwardings=yes -o PermitLocalCommand=no \
    -o ConnectTimeout=15 -o ServerAliveInterval=15 -o ServerAliveCountMax=2 -o LogLevel=ERROR \
    127.0.0.1 agents-token-write
}

# vm_send <token> <expiry> <name> <email> runs vm_write in the VM, which writes each file whose first field is
# not empty, and sets written to what it printed: through limactl in limactl mode, through ssh in SSH mode. On
# failure it says why, never a secret, and sets failure: permanent when the VM refused the write (status 3),
# transient otherwise (status 4 is another git writer's lock on ~/.gitconfig, 255 is ssh's own failure).
vm_send() {
  sent=0
  if [ "${mode}" = ssh ]; then
    written="$(printf '%s\n%s\n%s\n%s\n' "$1" "$2" "$3" "$4" | vm_ssh)" || sent=$?
  else
    written="$(printf '%s\n%s\n%s\n%s\n' "$1" "$2" "$3" "$4" |
      "${limactl}" shell --workdir / "${instance}" sh -c "${vm_write}" vm-write "${vm_real_gh}")" || sent=$?
  fi
  [ "${sent}" -eq 0 ] || {
    written="${sent}"
    if [ "${written}" -eq 3 ]; then
      failure=permanent
      say "the VM ${instance} refused to write the ${5} (see above)"
    elif [ "${written}" -eq 4 ]; then
      failure=transient
      say "the VM ${instance} did not write the ${5} now: another git writer holds the lock on ~/.gitconfig (see above); if no git is running in the VM, remove ~/.gitconfig.lock there (a crashed git leaves it behind)"
    elif [ "${mode}" = ssh ] && [ "${written}" -eq 255 ]; then
      failure=transient
      say "could not write the ${5} into the VM ${instance} over SSH (127.0.0.1:${ssh_port}, ssh exit 255): is it running (make vm-up)? If ssh said \`Host key verification failed\` above, the VM's host key changed (a recreated VM): pin it again with make vm-agents-token-ssh; if it said \`Permission denied\`, the VM does not authorise the account's key: run make vm-agents-token-ssh"
    elif [ "${mode}" = ssh ]; then
      failure=transient
      say "could not write the ${5} into the VM ${instance} over SSH (127.0.0.1:${ssh_port}, ssh exit ${written}): is it running (make vm-up), and is ssh installed?"
    else
      failure=transient
      say "could not write the ${5} into the VM ${instance}: is it running (make vm-up)?"
    fi
    return 1
  }
}

# mint_once mints one installation token and writes it into the VM at once, with the bot identity when that
# is known already. It sets expiry to the token's expiry, and prints why when it fails, never a secret. On
# failure it sets failure to transient or permanent, which refresh tells apart. While the bot identity is not
# known it keeps the token in held, for identify.
mint_once() {
  failure=permanent
  wait_hint=""
  started="$(now)"
  make_jwt || {
    say "cannot sign a JWT with SHIFT2EU_AGENTS_APP_KEY_FILE: is it the App's PEM private key?"
    return 1
  }
  if [ -z "${slug}" ]; then
    lookup_slug || return 1
  fi
  call POST "/app/installations/${SHIFT2EU_AGENTS_APP_INSTALLATION_ID}/access_tokens" "${jwt}"
  jwt=""
  classify "${status}"
  case "${status}" in
  201) ;;
  000)
    say "no installation token minted: GitHub (${api}) could not be reached"
    return 1
    ;;
  401)
    say "no installation token minted: GitHub refused the App's JWT (HTTP 401); check SHIFT2EU_AGENTS_APP_ID, SHIFT2EU_AGENTS_APP_KEY_FILE and the Mac's clock"
    return 1
    ;;
  403)
    if [ "${failure}" = transient ]; then
      say "no installation token minted: GitHub's rate limit (HTTP 403); trying again later"
    else
      say "no installation token minted: GitHub forbids this App's installation SHIFT2EU_AGENTS_APP_INSTALLATION_ID a token (HTTP 403); is it suspended?"
    fi
    return 1
    ;;
  404)
    say "no installation token minted: no installation SHIFT2EU_AGENTS_APP_INSTALLATION_ID of this App (HTTP 404)"
    return 1
    ;;
  *)
    say "no installation token minted: GitHub answered HTTP ${status}"
    return 1
    ;;
  esac
  token="$(json_string token)"
  expires_at="$(json_string expires_at)"
  # The token is opaque, as GitHub asks: an allow list, not a format parser. It takes both of GitHub's
  # formats, ghs_ and 36 alphanumerics, and ghs_<App ID>_<JWT> (about 520 characters, variable length,
  # whose base64url JWT adds - and the . between its parts), and still refuses anything that could break
  # the two-line token file or the shell: whitespace, quotes, \, $, ; and control characters.
  # The set is spelled out, not written as ranges like A-Z: some shells, bash 3.2 among them as the
  # Mac's /bin/sh, match a range by the locale's collation, where a UTF-8 locale can let a non-ASCII
  # letter through. LC_ALL=C instead would also change the locale of every command this script runs.
  case "${token}" in
  '' | *[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.-]*)
    failure=transient
    say "no installation token minted: GitHub's answer holds no usable token"
    return 1
    ;;
  esac
  # Five minutes early, of the Mac's hour and of GitHub's own expires_at, whichever ends first: a call
  # started just before the expiry still has a valid token, and so does a VM whose clock lags a little.
  expiry="$((started + 3600 - 300))"
  if stated="$(parse_time "${expires_at}")" && is_number "${stated}" && stated="$(decimal "${stated}")" && [ "$((stated - 300))" -lt "${expiry}" ]; then
    expiry="$((stated - 300))"
  fi
  # A token the VM did not take is dropped: the next try mints another one.
  vm_send "${token}" "${expiry}" "${bot_name}" "${bot_email}" "token minted just now" || {
    token=""
    return 1
  }
  if [ -n "${bot_email}" ]; then
    identity_written=1
    held=""
    say "wrote a token for ${bot_name} into the VM ${instance}, valid until $(show_time "${expiry}")"
    said_git_identity
  else
    held="${token}"
    say "wrote a token into the VM ${instance}, valid until $(show_time "${expiry}"); looking up the bot user next"
  fi
  token=""
  check_vm "${written}"
}

# identify looks the bot user up with the held token, once per process, and writes the identity file into
# the VM; with both done already it does nothing. It fails like mint_once, never touching the VM's token.
identify() {
  wait_hint=""
  if [ -z "${bot_email}" ]; then
    lookup_identity || return 1
    held=""
  fi
  [ "${identity_written}" -eq 0 ] || return 0
  vm_send "" "" "${bot_name}" "${bot_email}" "bot identity" || return 1
  identity_written=1
  say "wrote the bot identity ${bot_name} into the VM ${instance}"
  said_git_identity
}

# stopped <why> says the refresh stopped, when, until when the VM's token is valid, and how to start it
# again. make agents-token-refresh-start sends it to the refresh's log too. It names the token's expiry only
# while that is still ahead: a token GitHub already expired, or one from before a long sleep, is not valid.
stopped() {
  if [ -n "${expiry:-}" ] && [ "${expiry}" -gt "$(now)" ]; then
    left="the VM's token is valid until $(show_time "${expiry}")"
  elif [ -n "${expiry:-}" ]; then
    left="the VM's token from this refresh has expired"
  else
    left="the VM holds no token from this refresh"
  fi
  if [ "${mode}" = ssh ]; then
    # SSH mode is the account's LaunchDaemon: launchd starts it again within its ThrottleInterval.
    restart="as the account's LaunchDaemon it starts again within 5 minutes, or now with sudo launchctl kickstart -k system/io.github.timothymamo.shift2eu.agents-token-refresh.${instance}; in single-account mode start it again from the host clone on the Mac with make agents-token-refresh-start"
  else
    restart="start it again from the host clone on the Mac with make agents-token-refresh-start"
  fi
  say "agents App token refresh stopped at $(show_time "$(now)") ($1); ${left}; ${restart}"
}

# is_refresh <pid> tells whether pid is a running refresh. is_number and kill -0 must stay ahead of ps -p:
# ps -p accepts a list of pids (123,456 or "123 456"), and the lock's pid file is untrusted text
# (TestAgentsAppTokenRefreshTakesOverALockWhosePidIsNoNumber pins this).
is_refresh() {
  is_number "$1" && kill -0 "$1" 2>/dev/null &&
    ps -p "$1" -o command= 2>/dev/null | grep -q 'agents-app-token.sh refresh'
}

# show_pid <text> prints what was read from a lock's pid file for a message: the pid when it is a number,
# unknown when it is empty, and invalid otherwise, so no text from that file (a terminal escape, say) reaches
# the output.
show_pid() {
  if is_number "$1"; then
    printf '%s' "$1"
  elif [ -z "$1" ]; then
    printf 'unknown'
  else
    printf 'invalid'
  fi
}

# take_lock makes this the only refresher of the VM instance: the lock is a directory under the state
# directory on the Mac holding the owner's pid. It is created whole: a temporary directory with this shell's
# pid already inside is renamed to the lock, so no reader ever sees a lock without its pid. When the lock
# exists, mv moves the temporary directory into it instead, and the pid file shows this one lost: it removes
# its own directory again. A lock whose pid is no longer a running refresh (one killed without its trap) is
# taken over atomically: it is renamed aside, which only one contender can do; the one that did checks that
# what it moved still holds the stale owner (and puts it back if not), removes it, and tries again. A lock
# holding this shell's own pid is this one's, even when its own read missed it because a contender had moved
# it aside for a moment (issue #240). While another refresher holds the lock, this one says so and exits 0:
# that VM's token is already being refreshed. A lock moved aside that held no stale owner is put back only
# while no lock exists; when a third contender has created one meanwhile, the one moved aside is dropped and
# this one loses, so a lock never ends up nested inside another (issue #288). The refresher whose lock was
# dropped is not told here: still_locked, before each mint, finds the lock no longer its own. A put-back that
# fails is tried once more; what was moved aside is removed only when a lock exists afterwards, and when none
# does it is kept and this one stops, or, when it is gone too, this one says so and tries again (issue #319).
take_lock() {
  state="${XDG_STATE_HOME:-${HOME:?set HOME}/.local/state}/shift2eu"
  lock="${state}/agents-token-refresh-${instance}.lock"
  new="${state}/.agents-token-refresh-${instance}.lock.new.$$"
  aside="${state}/.agents-token-refresh-${instance}.lock.stale.$$"
  (umask 077 && mkdir -p "${state}") || die "cannot create ${state} for the refresh's lock"
  attempts=0
  while :; do
    rm -rf "${new}" "${aside}"
    (umask 077 && mkdir "${new}" && printf '%s\n' "$$" >"${new}/pid") ||
      die "cannot create ${new} for the refresh's lock"
    if mv "${new}" "${lock}" 2>/dev/null && [ "$(cat "${lock}/pid" 2>/dev/null || true)" = "$$" ]; then
      break
    fi
    # Lost: the temporary directory is still there, or inside the other refresher's lock.
    rm -rf "${new}" "${lock:?}/${new##*/}"
    owner="$(cat "${lock}/pid" 2>/dev/null || true)"
    # A lock holding this shell's pid is this one's: only this shell writes its pid, so the lock it had won was
    # moved aside by a contender and put back between the mv and the read above (issue #240).
    [ "${owner}" != "$$" ] || break
    attempts=$((attempts + 1))
    [ "${attempts}" -le 5 ] || die "cannot take the lock ${lock}; remove it if no refresh for the VM ${instance} runs"
    if is_refresh "${owner}"; then
      say "another agents App token refresh for the VM ${instance} is running (pid $(show_pid "${owner}"), lock ${lock}); this one exits and leaves it to that one"
      exit 0
    fi
    [ -d "${lock}" ] || continue # released or moved aside meanwhile: try again
    say "taking over the lock ${lock} of a refresh that is no longer running (pid $(show_pid "${owner}"))"
    if mv "${lock}" "${aside}" 2>/dev/null; then
      if [ "$(cat "${aside}/pid" 2>/dev/null || true)" = "${owner}" ]; then
        rm -rf "${aside}"
      else
        # Another contender took the stale lock over first and this moved its new one: put it back, but never
        # onto a lock a third contender has created meanwhile, where mv would nest it inside that lock as a copy
        # nobody removes (issue #288). Then the lock is that third one's: drop what was moved aside, which
        # leaves its own owner without the lock (that owner finds out before its next mint), and lose.
        # A put-back that fails (a full disk, a permission) is tried once more, so a transient failure leaves
        # the winner its lock (issue #319).
        putback=0
        while [ "${putback}" -lt 2 ] && [ ! -e "${lock}" ] && [ ! -L "${lock}" ]; do
          if mv "${aside}" "${lock}" 2>/dev/null; then
            break
          fi
          putback=$((putback + 1))
        done
        if [ -e "${lock}" ] || [ -L "${lock}" ]; then
          # Only with a lock in place is what was moved aside a duplicate, and dropped. The lock can still
          # appear between the test and the mv: whatever then landed inside it goes too.
          rm -rf "${aside}" "${lock:?}/${aside##*/}"
        elif [ -e "${aside}" ] || [ -L "${aside}" ]; then
          # No lock, and the put-back failed: the aside is another refresher's lock, so it is kept, never
          # removed (removing it cost the only refresher its lock, issue #319), and this one stops. The next
          # loop round would remove ${aside}, so it must not go on. Its owner's still_locked finds no lock within
          # seconds and exits, so the aside is then a stale entry that nothing removes: the message says so.
          die "cannot put the lock moved aside to ${aside} back to ${lock}; it is kept so no lock is lost while none exists at ${lock}, and this refresh stops. Once no refresh runs, remove it (rm -r ${aside}) and start a refresh again from the host clone on the Mac with make agents-token-refresh-start"
        else
          # Neither is left: nothing of the other refresher's lock remains to keep, so this one tries again.
          say "the lock ${lock} and the one moved aside to ${aside} are both gone: the moved-aside lock could not be put back; this refresh tries to take the lock again"
          continue
        fi
        if [ -e "${lock}" ] && [ "$(cat "${lock}/pid" 2>/dev/null || true)" != "$$" ]; then
          say "another agents App token refresh for the VM ${instance} took the lock ${lock} over first (pid $(show_pid "$(cat "${lock}/pid" 2>/dev/null || true)")); this one exits and leaves it to that one"
          exit 0
        fi
      fi
    fi
  done
  locked=1
}

# still_locked returns when the lock holds this refresher's pid, and otherwise exits 0 without minting: a
# contender that read a stale owner can move this refresher's fresh lock aside after take_lock returned and,
# finding a third contender's lock in place, drop it (issue #288). A lock whose pid reads empty (missing, or
# without its pid) may only be in transit, moved aside by such a contender to be put back (issue #240): it
# is read again once a second, at most five times. When it waited, it reads the clock into current again,
# so the margin check of the same round uses the time of after the wait. A lock that holds another pid has
# been taken over: this one says so and leaves it. A lock still empty after the five tries is gone: no
# refresher holds it, so this one says that a refresh must be started again. Either way release_lock, which
# removes only a lock holding this one's pid, leaves the lock alone.
still_locked() {
  holder="$(cat "${lock}/pid" 2>/dev/null || true)"
  tries=0
  while [ -z "${holder}" ] && [ "${tries}" -lt 5 ]; do
    # The loop's background sleep, so the TERM and INT traps kill it and run at once, rather than up to a
    # second late after a foreground sleep (issue #319); see refresh for sleeper.
    sleeper=""
    sleep 1 &
    sleeper=$!
    wait "${sleeper}" || true
    sleeper=none
    holder="$(cat "${lock}/pid" 2>/dev/null || true)"
    tries=$((tries + 1))
  done
  [ "${tries}" -eq 0 ] || current="$(now)"
  [ "${holder}" != "$$" ] || return 0
  if [ -z "${holder}" ]; then
    say "the lock ${lock} is gone or holds no pid: no agents App token refresh holds it, so this one exits without minting and nothing refreshes the VM ${instance}'s token now; start a refresh again from the host clone on the Mac with make agents-token-refresh-start"
  else
    say "the lock ${lock} is no longer this refresh's (it holds pid $(show_pid "${holder}")): another agents App token refresh for the VM ${instance} took it over, so this one exits without minting and leaves it to that one"
  fi
  release_lock
  exit 0
}

# release_lock removes the lock, if this refresher holds it.
release_lock() {
  if [ "${locked:-0}" -eq 1 ] && [ "$(cat "${lock}/pid" 2>/dev/null || true)" = "$$" ]; then
    rm -rf "${lock}"
  fi
  locked=0
}

refresh() {
  # sleeper is the pid of the loop's running sleep, empty from just before the sleep starts until its pid is
  # stored, and none while no sleep runs. The traps kill ${sleeper}, or, while it is still empty, the last
  # background job $!: a signal between `sleep ... &` and `sleeper=$!` would otherwise leave that sleep running
  # after the script exits (issue #289). While it is none they kill nothing. Otherwise they kill that pid only
  # when ps names this shell as its parent: a child this shell has not reaped keeps its pid, so that pid is
  # still the sleep. One already reaped (the last round's sleep, which $! still names between `sleeper=""` and
  # `sleep ... &`, or the sleep wait has just reaped before `sleeper=none`) is no child of this shell any more,
  # even when the system has given its pid to another process, and is not killed, short of its pid being
  # reused in the instant between the ppid check and the kill (the two are not atomic: the shell may reap a
  # sleep that exits while it waits for ps). They read $! as ${!:-}: before the loop's first `sleep ... &` no
  # background job has run and $! is unset, so under set -u a bare $! would make the shell report "parameter
  # not set" into the log; ${!:-} is empty then, ps -p "" names no parent and nothing is killed (issue #289).
  # Each trap stays one line.
  sleeper=none
  locked=0
  trap 'release_lock' EXIT
  trap 'stopped "signal"; [ "${sleeper}" = none ] || [ "$(ps -o ppid= -p "${sleeper:-${!:-}}" 2>/dev/null | tr -d " ")" != "$$" ] || kill "${sleeper:-${!:-}}" 2>/dev/null || true; release_lock; exit 143' TERM
  trap 'stopped "interrupted"; [ "${sleeper}" = none ] || [ "$(ps -o ppid= -p "${sleeper:-${!:-}}" 2>/dev/null | tr -d " ")" != "$$" ] || kill "${sleeper:-${!:-}}" 2>/dev/null || true; release_lock; exit 130' INT
  take_lock
  expiry=""
  failures=0
  say "refreshing the agents App token every ${check_seconds}s, ${margin_seconds}s before it expires"
  while :; do
    wait_seconds="${check_seconds}"
    # The wall clock, not a sleep of the token's lifetime: after the Mac sleeps, the first check sees how
    # little is left and mints at once.
    current="$(now)"
    # After the clock read and right before any mint: the lock can have turned over since take_lock or the
    # last check (issue #288), and a refresher that no longer holds it must not mint beside the one that does.
    still_locked
    ok=1
    why="no token could be minted, and trying again cannot fix it"
    if [ -z "${expiry}" ] || [ "$((expiry - current))" -lt "${margin_seconds}" ]; then
      previous="${expiry}"
      if ! mint_once; then
        expiry="${previous}"
        ok=0
      fi
    fi
    # The bot identity, until it is in the VM: looked up with the token held from the mint, which is valid
    # here, as the check above mints a new one first when it is not.
    if [ "${ok}" -eq 1 ] && [ -n "${expiry}" ] && [ "${identity_written}" -eq 0 ]; then
      if ! identify; then
        ok=0
        why="the bot user's identity cannot be read or written into the VM, and trying again cannot fix it"
      fi
    fi
    if [ "${ok}" -eq 1 ]; then
      failures=0
    else
      if [ "${failure}" = permanent ]; then
        stopped "${why}"
        release_lock
        exit 1
      fi
      # Transient: never stop, even with no valid token left (a Mac waking before its network). Back off
      # from one check to at most five.
      failures=$((failures + 1))
      wait_seconds="${check_seconds}"
      n=1
      while [ "${n}" -lt "${failures}" ] && [ "${wait_seconds}" -lt "$((check_seconds * 5))" ]; do
        wait_seconds=$((wait_seconds * 2))
        n=$((n + 1))
      done
      [ "${wait_seconds}" -le "$((check_seconds * 5))" ] || wait_seconds=$((check_seconds * 5))
      # A rate limit is waited out: never retried before its retry-after or its reset (classify caps
      # either at 3600 seconds), however many checks that is.
      if is_number "${wait_hint:-}" && [ "${wait_hint}" -gt "${wait_seconds}" ]; then
        wait_seconds="${wait_hint}"
      fi
      if [ -n "${expiry}" ] && [ "${expiry}" -gt "${current}" ]; then
        left="the VM's token is valid until $(show_time "${expiry}")"
      else
        left="the VM has no valid token until then"
      fi
      say "will try again in ${wait_seconds}s (failure ${failures} in a row); ${left}"
    fi
    # A TERM landing between `sleep ... &` and `sleeper=$!` finds sleeper empty, and the trap kills $!, the
    # sleep just started (issue #289); one landing before `sleep ... &` finds $! naming the last round's sleep,
    # reaped and no child any more, and kills nothing.
    sleeper=""
    sleep "${wait_seconds}" &
    sleeper=$!
    wait "${sleeper}" || true
    sleeper=none
  done
}

check_seconds="${SHIFT2EU_AGENTS_TOKEN_CHECK_SECONDS:-60}"
margin_seconds="${SHIFT2EU_AGENTS_TOKEN_MARGIN_SECONDS:-1200}"
slug=""
bot_name=""
bot_email=""
jwt=""
token=""
held=""
identity_written=0

command="${1:-}"
case "${command}" in
mint)
  check_inputs
  mint_once
  identify
  ;;
refresh)
  check_inputs
  refresh
  ;;
vm-writer)
  # The writer make vm-agents-token-ssh installs in the VM: vm_write as a script of its own, whose $1 is the
  # real gh's path, as the forced command passes it (ADR 0031, update of 2026-11-19, 4).
  # shellcheck disable=SC2016 # a comment line of the printed script, not expanded here
  printf '#!/bin/sh\n%s\n%s\n' \
    '# agents-token-write: generated by dev/agents-app-token.sh vm-writer (ADR 0031); $1 is the real gh' \
    "${vm_write}"
  ;;
*)
  echo "usage: $0 mint|refresh|vm-writer" >&2
  exit 2
  ;;
esac
