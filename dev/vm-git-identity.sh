#!/usr/bin/env bash
# Sets the git identity the agents commit with in the Lima VM (git config --global): step 3 of
# dev/vm-init.sh, a script of its own so its switch-over cases are tested without a VM (PRD #161 milestone
# 1, ADR 0031).
#
# Usage: dev/vm-git-identity.sh <fallback user.name> <fallback user.email>
#
#   - $HOME/.config/shift2eu/agents-app-identity present (make agents-token wrote it): its two lines, the
#     agents App's bot name and noreply address, replace any identity the VM has. A file that is not exactly
#     that is refused, and nothing changes. The mint that writes the file (dev/agents-app-token.sh) sets the
#     same identity at once, since on a new VM this step runs before the first mint (issue #241); here it
#     is applied again, which changes nothing when the mint already did.
#   - absent: the fallback (the identity make vm-init was given, the Mac's own by default) is set where the
#     VM has none, and replaces a bot's identity left from an earlier run (the rollback). Any other identity
#     the VM has is kept.
#
# It changes nothing on a re-run, and fails before changing anything.
set -euo pipefail

[ "$#" -eq 2 ] || {
  echo "usage: $0 <fallback user.name> <fallback user.email>" >&2
  exit 2
}
git_name="$1"
git_email="$2"

say() { printf 'vm-init: git: %s\n' "$*"; }
fail() {
  printf 'vm-init: git: %s\n' "$*" >&2
  exit 1
}

identity_file="${HOME:?HOME is not set}/.config/shift2eu/agents-app-identity"
bot_identity=0
if [ -e "${identity_file}" ] || [ -L "${identity_file}" ]; then
  { IFS= read -r git_name && IFS= read -r git_email; } <"${identity_file}" ||
    fail "${identity_file} does not hold two lines; ask the maintainer to run make agents-token on the Mac"
  [[ "${git_name}" =~ ^[A-Za-z0-9-]+\[bot\]$ ]] ||
    fail "the first line of ${identity_file} is not an App's bot name (<slug>[bot]); ask the maintainer to run make agents-token on the Mac"
  [[ "${git_email}" =~ ^[0-9]+\+[A-Za-z0-9-]+\[bot\]@users\.noreply\.github\.com$ ]] ||
    fail "the second line of ${identity_file} is not a bot's noreply address; ask the maintainer to run make agents-token on the Mac"
  bot_identity=1
fi
[ -n "${git_name}" ] || fail "no user.name; set it on the Mac (git config --global user.name) or pass GIT_USER_NAME=..."
[ -n "${git_email}" ] || fail "no user.email; set it on the Mac (git config --global user.email) or pass GIT_USER_EMAIL=..."

# Set only where the VM has none, so a re-run never replaces an identity chosen in the VM. Two exceptions,
# for the switch to the App and back: the bot's identity replaces any other, and with no identity file a
# bot's identity left from an earlier run gives way to the fallback.
# A key ~/.gitconfig holds several times is read with --get-all (one value per line), so several values are
# never the wanted identity, and every write is --replace-all: git config <key> <value> refuses to write one
# value over several (status 5), as the mint (dev/agents-app-token.sh) knows too (issue #262).
for pair in "user.name=${git_name}" "user.email=${git_email}"; do
  key="${pair%%=*}"
  value="${pair#*=}"
  current="$(git config --global --get-all "${key}" || true)"
  # Only what is printed is stripped of the bytes a terminal acts on (escapes, carriage returns, bells), the
  # way the mint's printable does; the comparisons and the writes use the values as they are (issue #262).
  # Only C0 control bytes and DEL go, byte by byte in the C locale, so UTF-8 names (José Müller) survive
  # whatever the locale; [:print:] would drop their bytes in the C locale.
  shown="$(printf '%s' "${current}" | tr '\n' ',' | LC_ALL=C tr -d '\000-\037\177')"
  shown_value="$(printf '%s' "${value}" | LC_ALL=C tr -d '\000-\037\177')"
  if [ -n "${current}" ] && [ "${current}" != "${value}" ] &&
    { [ "${bot_identity}" -eq 1 ] || [[ "${current}" == *"[bot]"* ]]; }; then
    git config --global --replace-all "${key}" "${value}"
    say "replaced ${key} ${shown} with ${shown_value}"
  elif [ -z "${current}" ]; then
    git config --global --replace-all "${key}" "${value}"
    say "set ${key} to ${shown_value}"
  elif [ "${current}" = "${value}" ]; then
    say "${key} already ${shown_value}"
  else
    say "kept ${key} ${shown}, not ${shown_value}; change it with git config --global ${key} in the VM"
  fi
done
