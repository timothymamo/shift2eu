#!/usr/bin/env bash
# Does the once-only setup of a new Lima VM, after `make vm-env` and `make vm-claude-settings`, so the
# first `make deck` needs nothing typed in the VM (issue #128). Run it through `make vm-init`, which
# starts it in a login shell: only a login shell loads the credentials from
# ~/.config/dot-agent-deck/daemon.env (/etc/profile.d/shift2eu.sh).
#
# Steps, each named in its message, the first failing one stopping the run:
#   1. theme       Claude Code's theme in ~/.claude/settings.json.
#   2. claude      Claude Code's first-run flags in ~/.claude.json: onboarding done, and the workspace and
#                  the repository's main checkout trusted. Claude Code looks trust up by a session's git
#                  repository root, which for every worktree is the main checkout, so trusting the
#                  workspace alone does not cover sessions in the repository. The flags are Claude Code's
#                  saved state, not documented settings, so a later version may rename them; the fallback
#                  is a manual `claude` run in each of the two directories (docs/dev-environment.md).
#   3. git         The git identity the agents commit with (git config --global), where the VM has none:
#                  the agents App's bot from ~/.config/shift2eu/agents-app-identity when make agents-token
#                  wrote it (it replaces any other), otherwise the one passed in (step 3 below runs it).
#   4. claude auth Claude Code uses the subscription's OAuth token, not an API key.
#   5. pi          pi has the DeepSeek key.
#   6. gh          gh can read this repository, with the agents App's token or GITHUB_TOKEN.
#   7. deck        .dot-agent-deck.toml is valid.
#   8. deck command The deck's default_command, which new agents and dispatchers start with, where the VM
#                  has none (issue #134).
#   9. daemon      The deck's daemon is installed as a systemd user service, if it is not yet.
#
# Running it again changes nothing: a file is written only when a flag in it is not set yet, the theme only
# when it differs from the one asked for, and the git identity and the deck's default command only where
# the VM has none. Run it while no Claude Code session runs in the VM (before `make deck`): a running
# session can write its own copy of ~/.claude.json back over the trust flags. It never prints a credential: the tools whose output can
# include one are run quietly or filtered to the field checked.
set -euo pipefail

usage="usage: $0 <workspace directory> <main checkout> <git user.name> <git user.email> <claude theme>"
[ "$#" -eq 5 ] || {
  echo "${usage}" >&2
  exit 2
}
workspace="$1"
main_checkout="$2"
git_name="$3"
git_email="$4"
theme="$5"
repo="$(cd "$(dirname "$0")/.." && pwd)"

# Provisioned by dev/lima.yaml, so this never rewrites the Mac's own configuration by mistake.
if [ ! -f /etc/profile.d/shift2eu.sh ]; then
  echo "vm-init: run this inside the Lima VM (make vm-init)" >&2
  exit 1
fi

say() { printf 'vm-init: %s\n' "$*"; }
fail() {
  printf 'vm-init: %s\n' "$*" >&2
  exit 1
}

# set_json <file> <jq filter> [jq options...] applies the filter to a JSON file, keeping every other key,
# and rewrites the file only when the filter changes it. Returns 1 when it was already set. A new file and
# the rewritten one are readable only by the VM user, as ~/.claude.json is. Each command fails on its own,
# because set -e does not apply inside a function called as an if condition.
set_json() {
  local file="$1" filter="$2" tmp
  shift 2
  mkdir -p "$(dirname "${file}")" || fail "cannot create $(dirname "${file}")"
  [ -s "${file}" ] || (umask 077 && echo '{}' >"${file}") || fail "cannot create ${file}"
  jq empty "${file}" 2>/dev/null || fail "${file} is not valid JSON; fix or remove it, then run make vm-init again"
  if jq -e "$@" "(${filter}) == ." "${file}" >/dev/null; then
    return 1
  fi
  tmp="$(mktemp "${file}.XXXXXX")" || fail "cannot create a temporary file next to ${file}"
  if ! jq "$@" "${filter}" "${file}" >"${tmp}" || ! mv "${tmp}" "${file}"; then
    rm -f "${tmp}"
    fail "cannot update ${file}"
  fi
}

# 1. theme
if set_json "${HOME}/.claude/settings.json" ".theme = \$theme" --arg theme "${theme}"; then
  say "theme: set Claude Code's theme to ${theme}"
else
  say "theme: already ${theme}"
fi

# 2. claude: the flags manual first runs in the two directories leave behind.
if set_json "${HOME}/.claude.json" \
  ".hasCompletedOnboarding = true
   | .projects[\$workspace].hasTrustDialogAccepted = true
   | .projects[\$main].hasTrustDialogAccepted = true" \
  --arg workspace "${workspace}" --arg main "${main_checkout}"; then
  say "claude: marked onboarding done and trusted ${workspace} and ${main_checkout}"
else
  say "claude: onboarding already done and ${workspace} and ${main_checkout} already trusted"
fi

# 3. git: the agents App's bot when make agents-token has written its identity into the VM (PRD #161,
# ADR 0031), otherwise the identity passed in; a script of its own, so its cases are tested without a VM.
bash "${repo}/dev/vm-git-identity.sh" "${git_name}" "${git_email}" || exit 1

# 4. claude auth: filtered to the one field checked.
method="$({ claude auth status --json 2>/dev/null || true; } | jq -r '.authMethod // "none"' 2>/dev/null || true)"
[ "${method}" = "oauth_token" ] ||
  fail "claude auth: Claude Code authenticates with '${method:-none}', not the subscription's OAuth token; set CLAUDE_CODE_OAUTH_TOKEN for make vm-env, never ANTHROPIC_API_KEY, then run make vm-env and make vm-init again"
say "claude auth: Claude Code uses the subscription's OAuth token"

# 5. pi prints only ready or not_ready.
pi auth check --provider deepseek >/dev/null 2>&1 ||
  fail "pi: no DeepSeek key; set DEEPSEEK_API_KEY for make vm-env, then run make vm-env and make vm-init again"
say "pi: DeepSeek key ready"

# 6. gh: a read of this repository, which works with both credentials the gh wrapper hands gh (ADR 0031):
# the agents App's installation token, which cannot call GET /user and so may fail gh auth status, and
# GITHUB_TOKEN. Only the repository's name is printed.
origin="$(git -C "${main_checkout}" remote get-url origin 2>/dev/null || true)"
nwo="$(printf '%s\n' "${origin}" | sed -n -E 's#^(https://github\.com/|git@github\.com:|ssh://git@github\.com/)([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)$#\2#p')"
nwo="${nwo%.git}"
[ -n "${nwo}" ] || fail "gh: ${main_checkout}'s origin is not a github.com repository"
if [ -e "${HOME}/.config/shift2eu/agents-app-token" ]; then
  credential="the agents App's token (make agents-token)"
else
  credential="GITHUB_TOKEN (make vm-env)"
fi
read_back="$(gh api "repos/${nwo}" --jq .full_name 2>/dev/null || true)"
[ "${read_back,,}" = "${nwo,,}" ] ||
  fail "gh: cannot read ${nwo} with ${credential}; with the App, run make agents-token on the Mac; without it, set GITHUB_TOKEN for make vm-env, then run make vm-env and make vm-init again"
say "gh: reads ${nwo} with ${credential}"

# 7. deck
dot-agent-deck validate --path "${repo}" >/dev/null || fail "deck: ${repo}/.dot-agent-deck.toml is not valid; see dot-agent-deck validate"
say "deck: .dot-agent-deck.toml is valid"

# 8. deck command: what the New Agent form pre-fills, and what a dispatcher started with a blank command,
# and the single units it dispatches, run. The orchestrator role's command (.dot-agent-deck.toml). Set
# only where the VM has none, so a re-run never replaces a command chosen in the VM.
deck_command="claude --model opus --permission-mode auto"
current="$(dot-agent-deck config get default_command)" || fail "deck command: dot-agent-deck config get default_command failed"
if [ -z "${current}" ]; then
  dot-agent-deck config set default_command "${deck_command}" >/dev/null ||
    fail "deck command: dot-agent-deck config set default_command failed"
  say "deck command: set default_command to ${deck_command}"
elif [ "${current}" = "${deck_command}" ]; then
  say "deck command: default_command already ${deck_command}"
else
  say "deck command: kept default_command ${current}, not ${deck_command}; change it with dot-agent-deck config set default_command in the VM"
fi

# 9. daemon: last, as installing it starts it, and only when it is not installed yet.
if [ "$(systemctl --user is-enabled dot-agent-deck.service 2>/dev/null || true)" = "enabled" ]; then
  say "daemon: dot-agent-deck.service already installed"
else
  dot-agent-deck daemon install || fail "daemon: dot-agent-deck daemon install failed"
  say "daemon: installed dot-agent-deck.service"
fi

say "done; open the deck with make deck"
