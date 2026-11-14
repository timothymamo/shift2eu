#!/usr/bin/env bash
# Sets the git identity the agents commit with in the Lima VM (git config --global): step 3 of
# dev/vm-init.sh, a script of its own so its switch-over cases are tested without a VM (PRD #161 milestone
# 1, ADR 0029).
#
# Usage: dev/vm-git-identity.sh <fallback user.name> <fallback user.email>
#
#   - $HOME/.config/shift2eu/agents-app-identity present (make agents-token wrote it): its two lines, the
#     agents App's bot name and noreply address, replace any identity the VM has. A file that is not exactly
#     that is refused, and nothing changes.
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
for pair in "user.name=${git_name}" "user.email=${git_email}"; do
  key="${pair%%=*}"
  value="${pair#*=}"
  current="$(git config --global --get "${key}" || true)"
  if [ -n "${current}" ] && [ "${current}" != "${value}" ] &&
    { [ "${bot_identity}" -eq 1 ] || [[ "${current}" == *"[bot]"* ]]; }; then
    git config --global "${key}" "${value}"
    say "replaced ${key} ${current} with ${value}"
  elif [ -z "${current}" ]; then
    git config --global "${key}" "${value}"
    say "set ${key} to ${value}"
  elif [ "${current}" = "${value}" ]; then
    say "${key} already ${value}"
  else
    say "kept ${key} ${current}, not ${value}; change it with git config --global ${key} in the VM"
  fi
done
