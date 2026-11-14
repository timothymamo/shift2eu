#!/bin/sh
# Mints installation tokens of the agents App on the Mac and writes them into the Lima VM (PRD #161
# milestone 1, docs/adr/0029-agents-open-pull-requests-as-a-github-app-with-tokens-minted-on-the-mac.md).
# The App's private key never leaves the Mac: the VM only ever holds a token that expires within the hour.
#
# Usage: dev/agents-app-token.sh mint      mint one token (make agents-token)
#        dev/agents-app-token.sh refresh   mint now, then again before each token expires, until stopped
#                                          (make agents-token-refresh; make deck starts it in the background)
#
# Inputs, from the maintainer's direnv .envrc above the workspace, never from the workspace:
#   SHIFT2EU_AGENTS_APP_ID               the App ID
#   SHIFT2EU_AGENTS_APP_INSTALLATION_ID  the ID of the App's installation on this repository
#   SHIFT2EU_AGENTS_APP_KEY_FILE         the path of the App's PEM private key, readable only by its owner
# and, for refresh:
#   SHIFT2EU_AGENTS_TOKEN_CHECK_SECONDS  how often the wall clock is compared with the expiry (default 60)
#   SHIFT2EU_AGENTS_TOKEN_MARGIN_SECONDS mint again when fewer seconds than this remain (default 1200)
# Hooks the tests use: GITHUB_API_URL (default https://api.github.com; plain http only for 127.0.0.1 and
# localhost), LIMACTL (default limactl) and LIMA_INSTANCE (default shift2eu).
#
# What it writes into the VM user's home, each file mode 0600 in a 0700 directory, replaced atomically:
#   ~/.config/shift2eu/agents-app-token     the token, then its expiry as Unix seconds
#   ~/.config/shift2eu/agents-app-identity  the git user.name <slug>[bot], then the git user.email
#                                           <bot user id>+<slug>[bot]@users.noreply.github.com
#
# Secrets: the JWT and the token live only in shell variables. They reach curl through its standard input
# (--config -) and the VM through limactl's standard input, never a command line, a file on the Mac, or
# this script's output. Errors name what failed, never a value.
set -eu

api="${GITHUB_API_URL:-https://api.github.com}"
api="${api%/}"
limactl="${LIMACTL:-limactl}"
instance="${LIMA_INSTANCE:-shift2eu}"

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

# parse_time <RFC 3339 UTC time> prints it as Unix seconds, or fails.
parse_time() {
  date -u -d "$1" +%s 2>/dev/null ||
    date -u -j -f '%Y-%m-%dT%H:%M:%SZ' "$1" +%s 2>/dev/null
}

is_number() {
  case "$1" in
  '' | *[!0-9]*) return 1 ;;
  *) return 0 ;;
  esac
}

check_inputs() {
  is_number "${SHIFT2EU_AGENTS_APP_ID:-}" ||
    die "SHIFT2EU_AGENTS_APP_ID is not set, or not a number: export the agents App's ID from the .envrc above the workspace (docs/dev-environment.md, The agents App)"
  is_number "${SHIFT2EU_AGENTS_APP_INSTALLATION_ID:-}" ||
    die "SHIFT2EU_AGENTS_APP_INSTALLATION_ID is not set, or not a number: export the ID of the App's installation from the .envrc above the workspace"
  key="${SHIFT2EU_AGENTS_APP_KEY_FILE:-}"
  [ -n "${key}" ] ||
    die "SHIFT2EU_AGENTS_APP_KEY_FILE is not set: export the path of the App's private key, kept outside the workspace, from the .envrc above the workspace"
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

  case "${api}" in
  https://*) proto="=https" ;;
  http://127.0.0.1:* | http://127.0.0.1 | http://localhost:* | http://localhost) proto="=http" ;;
  *) die "GITHUB_API_URL must be an https URL" ;;
  esac
  is_number "${check_seconds}" && [ "${check_seconds}" -gt 0 ] ||
    die "SHIFT2EU_AGENTS_TOKEN_CHECK_SECONDS must be a positive number of seconds"
  is_number "${margin_seconds}" || die "SHIFT2EU_AGENTS_TOKEN_MARGIN_SECONDS must be a number of seconds"
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

# call <method> <path> [bearer|none] sets status and body. The headers, the URL and the method reach curl
# on its standard input, so the JWT never is on a command line.
call() {
  auth_header=""
  if [ "${3:-bearer}" = bearer ]; then
    auth_header="header = \"Authorization: Bearer ${jwt}\""
  fi
  response="$(
    printf '%s\n' \
      "url = \"${api}$2\"" \
      "request = \"$1\"" \
      "${auth_header}" \
      'header = "Accept: application/vnd.github+json"' \
      'header = "X-GitHub-Api-Version: 2022-11-28"' |
      curl --silent --show-error --max-time 30 --proto "${proto}" --write-out '\n%{http_code}' --config -
  )" || {
    status=000
    body=""
    return 0
  }
  status="${response##*
}"
  body="${response%
*}"
}

# json_string <name> prints the value of the first string field <name> of body.
json_string() {
  printf '%s' "${body}" | tr -d '\n' | sed -n "s/.*\"$1\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p"
}

# json_number <name> prints the value of the first number field <name> of body.
json_number() {
  printf '%s' "${body}" | tr -d '\n' | sed -n "s/.*\"$1\"[[:space:]]*:[[:space:]]*\\([0-9][0-9]*\\).*/\\1/p"
}

# lookup_identity sets bot_name and bot_email from GET /app (the slug) and GET /users/<slug>[bot] (the bot
# user's id). GitHub's noreply address for the bot is numbered with the bot user's id, not the App ID.
lookup_identity() {
  call GET /app
  [ "${status}" = 200 ] || {
    say "GET /app answered HTTP ${status}: cannot read the agents App's slug with SHIFT2EU_AGENTS_APP_ID and its key"
    return 1
  }
  slug="$(json_string slug)"
  case "${slug}" in
  '' | *[!A-Za-z0-9-]*)
    say "GET /app gave no usable slug"
    return 1
    ;;
  esac
  call GET "/users/${slug}%5Bbot%5D"
  if [ "${status}" != 200 ]; then
    # The bot's user is public: when GitHub refuses an App JWT on /users, ask without one.
    call GET "/users/${slug}%5Bbot%5D" none
  fi
  [ "${status}" = 200 ] || {
    say "GET /users/${slug}[bot] answered HTTP ${status}: cannot read the bot user's id"
    return 1
  }
  bot_id="$(json_number id)"
  is_number "${bot_id}" || {
    say "GET /users/${slug}[bot] gave no numeric id"
    return 1
  }
  bot_name="${slug}[bot]"
  bot_email="${bot_id}+${slug}[bot]@users.noreply.github.com"
}

# The script the VM runs: it reads four lines from its standard input and writes the two files atomically
# (a temporary file beside each, then mv), so a gh running at the same time never reads half a token.
# shellcheck disable=SC2016 # expanded by the VM's sh, not here
vm_write='set -eu
umask 077
dir="$HOME/.config/shift2eu"
mkdir -p "$dir"
chmod 700 "$dir"
IFS= read -r token
IFS= read -r expiry
IFS= read -r name
IFS= read -r email
tmp="$(mktemp "$dir/.agents-app-identity.XXXXXX")"
printf "%s\n%s\n" "$name" "$email" >"$tmp"
chmod 600 "$tmp"
mv -f "$tmp" "$dir/agents-app-identity"
tmp="$(mktemp "$dir/.agents-app-token.XXXXXX")"
printf "%s\n%s\n" "$token" "$expiry" >"$tmp"
chmod 600 "$tmp"
mv -f "$tmp" "$dir/agents-app-token"'

# mint_once mints one installation token and writes it, and the bot identity, into the VM. It sets expiry
# to the token's expiry, and prints why when it fails, never a secret.
mint_once() {
  started="$(now)"
  make_jwt || {
    say "cannot sign a JWT with SHIFT2EU_AGENTS_APP_KEY_FILE: is it the App's PEM private key?"
    return 1
  }
  if [ -z "${bot_email:-}" ]; then
    lookup_identity || return 1
  fi
  call POST "/app/installations/${SHIFT2EU_AGENTS_APP_INSTALLATION_ID}/access_tokens"
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
  case "${token}" in
  '' | *[!A-Za-z0-9_]*)
    say "no installation token minted: GitHub's answer holds no token"
    return 1
    ;;
  esac
  # A minute early, so a call started just before the expiry still has a valid token.
  expiry="$((started + 3600 - 60))"
  if stated="$(parse_time "$(json_string expires_at)")" && is_number "${stated}" && [ "$((stated - 60))" -lt "${expiry}" ]; then
    expiry="$((stated - 60))"
  fi
  printf '%s\n%s\n%s\n%s\n' "${token}" "${expiry}" "${bot_name}" "${bot_email}" |
    "${limactl}" shell --workdir / "${instance}" sh -c "${vm_write}" >/dev/null || {
    token=""
    say "minted a token but could not write it into the VM ${instance}: is it running (make vm-up)?"
    return 1
  }
  token=""
  say "wrote a token for ${bot_name} into the VM ${instance}, valid until $(show_time "${expiry}")"
}

# stopped <why> says the refresh stopped, when, until when the VM's token is valid, and how to start it
# again. make deck sends it to the refresh's log too.
stopped() {
  if [ -n "${expiry:-}" ]; then
    left="the VM's token is valid until $(show_time "${expiry}")"
  else
    left="the VM holds no token from this refresh"
  fi
  say "agents App token refresh stopped at $(show_time "$(now)") ($1); ${left}; start it again with make agents-token-refresh"
}

refresh() {
  sleeper=""
  trap 'stopped "signal"; [ -z "${sleeper}" ] || kill "${sleeper}" 2>/dev/null || true; exit 143' TERM
  trap 'stopped "interrupted"; [ -z "${sleeper}" ] || kill "${sleeper}" 2>/dev/null || true; exit 130' INT
  expiry=""
  say "refreshing the agents App token every ${check_seconds}s, ${margin_seconds}s before it expires"
  while :; do
    # The wall clock, not a sleep of the token's lifetime: after the Mac sleeps, the first check sees how
    # little is left and mints at once.
    current="$(now)"
    if [ -z "${expiry}" ] || [ "$((expiry - current))" -lt "${margin_seconds}" ]; then
      previous="${expiry}"
      if ! mint_once; then
        expiry="${previous}"
        # Retry at the next check while the VM's token still outlives it; stop once it would not.
        if [ -z "${expiry}" ] || [ "$((expiry - current))" -le "${check_seconds}" ]; then
          stopped "no token could be minted"
          exit 1
        fi
        say "will try again in ${check_seconds}s; the VM's token is valid until $(show_time "${expiry}")"
      fi
    fi
    sleep "${check_seconds}" &
    sleeper=$!
    wait "${sleeper}" || true
    sleeper=""
  done
}

check_seconds="${SHIFT2EU_AGENTS_TOKEN_CHECK_SECONDS:-60}"
margin_seconds="${SHIFT2EU_AGENTS_TOKEN_MARGIN_SECONDS:-1200}"
bot_name=""
bot_email=""

case "${1:-}" in
mint)
  check_inputs
  mint_once
  ;;
refresh)
  check_inputs
  refresh
  ;;
*)
  echo "usage: $0 mint|refresh" >&2
  exit 2
  ;;
esac
