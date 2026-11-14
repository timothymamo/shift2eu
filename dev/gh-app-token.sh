#!/bin/sh
# The gh of the Lima VM (PRD #161 milestone 1,
# docs/adr/0029-agents-open-pull-requests-as-a-github-app-with-tokens-minted-on-the-mac.md). dev/lima.yaml
# installs a copy of this file, root-owned, as /usr/local/bin/gh, and the real gh as
# /usr/local/libexec/shift2eu/gh, off the PATH: every agent, the deck, the git credential helper and
# make vm-init run this wrapper.
#
# On every call it reads ~/.config/shift2eu/agents-app-token (the agents App's installation token, then
# its expiry as Unix seconds, written by make agents-token on the Mac) and:
#   - file absent: runs the real gh unchanged, so it falls back to GITHUB_TOKEN (milestone 3 removes this);
#   - file present, well formed, mode 0600, the VM user's own and not expired: runs the real gh with the
#     token as GH_TOKEN and GITHUB_TOKEN unset, every argument and standard input passed through;
#   - anything else: fails without running gh, naming make agents-token-refresh, never the file's content.
# SHIFT2EU_REAL_GH names another real gh, for the tests.

real="${SHIFT2EU_REAL_GH:-/usr/local/libexec/shift2eu/gh}"
file="${HOME:?HOME is not set}/.config/shift2eu/agents-app-token"

if [ ! -e "${file}" ] && [ ! -L "${file}" ]; then
  exec "${real}" "$@"
fi

refuse() {
  printf 'gh: %s is %s; mint a new token on the Mac with make agents-token-refresh (or make agents-token), or delete the file to fall back to GITHUB_TOKEN\n' \
    "${file}" "$1" >&2
  exit 1
}

[ -f "${file}" ] && [ ! -L "${file}" ] || refuse "not a regular file"
[ -z "$(find "${file}" -prune \( -perm -0040 -o -perm -0020 -o -perm -0010 -o -perm -0004 -o -perm -0002 -o -perm -0001 \) -print)" ] ||
  refuse "readable or writable by other accounts (want mode 0600)"
[ -n "$(find "${file}" -prune -user "$(id -u)" -print)" ] || refuse "owned by another account"
[ -r "${file}" ] || refuse "not readable"

token=""
expiry=""
extra=""
{
  IFS= read -r token || :
  IFS= read -r expiry || :
  IFS= read -r extra || :
} <"${file}"

case "${token}" in
'' | *[!A-Za-z0-9_]*) refuse "malformed (want two lines: the token, then its expiry as Unix seconds)" ;;
esac
case "${expiry}" in
'' | *[!0-9]*) refuse "malformed (want two lines: the token, then its expiry as Unix seconds)" ;;
esac
[ -z "${extra}" ] || refuse "malformed (want exactly two lines)"

if [ "$(date +%s)" -ge "${expiry}" ]; then
  when="$(date -u -d "@${expiry}" '+%Y-%m-%d %H:%M:%S UTC' 2>/dev/null || date -u -r "${expiry}" '+%Y-%m-%d %H:%M:%S UTC' 2>/dev/null || echo "${expiry}")"
  refuse "expired since ${when} (is the refresh make deck starts still running? see its log)"
fi

GH_TOKEN="${token}"
export GH_TOKEN
unset GITHUB_TOKEN token
exec "${real}" "$@"
