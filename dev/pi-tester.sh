#!/bin/sh
# Starts pi for the tester role (issue #662,
# docs/adr/0044-refuse-printing-the-environment-in-pi-with-a-project-extension.md): .dot-agent-deck.toml's
# tester command runs this script, never pi itself.
#
# 1. Fails closed. pi logs a project extension that fails to load and runs without it, so the env-printing
#    refusal (ADR 0044) would fail open on its own. Before pi starts, the two files the refusal is made of
#    are checked, relative to the working directory pi loads the project from (the deck starts the pane in
#    the unit's folder): .pi/extensions/deny-env-printing.ts must be a non-empty regular file, and
#    .pi/deny-env-printing.json a JSON object whose "patterns" is a non-empty array (checked with jq, which
#    dev/lima.yaml installs). Anything else exits 1 with a message that names the file, and pi is not
#    started. pi (1.6.1, the release dev/lima.yaml pins) has no option that makes a failed extension load fatal, so this check is the one
#    that does it.
# 2. Starts pi with an allow-listed environment: `env -i` and only the variables named below, each copied
#    when the parent has it set, every argument passed through unchanged. Everything else - a GitHub
#    token, another agent's key (ANTHROPIC_API_KEY, CLAUDE_CODE_OAUTH_TOKEN), the OpenTelemetry headers
#    that carry a backend's API key, any name this list does not know - never reaches pi or a command pi
#    runs, so no command the tester runs can print it (CLAUDE.md rule 3).
#
# The tester gets no GitHub token in its environment. The VM's gh (dev/gh-app-token.sh, ADR 0031) reads the
# agents App's token from a file under HOME, not from the environment, so gh and git push still work for a
# command the tester runs; the orchestrator still hands the tester the issue text in its task, as before.
#
# No value is ever printed: the messages name files, never a variable's value.
#
# Usage: dev/pi-tester.sh <pi arguments...>   (from the directory pi loads the project from)
set -eu

extension=.pi/extensions/deny-env-printing.ts
list=.pi/deny-env-printing.json

# refuse <file> <why> stops the wrapper before pi starts.
refuse() {
  printf 'pi-tester: %s %s: the tester is not started, because pi would run without the env-printing refusal (issue #662, docs/adr/0044-refuse-printing-the-environment-in-pi-with-a-project-extension.md)\n' \
    "$1" "$2" >&2
  exit 1
}

[ -f "${extension}" ] || refuse "${PWD}/${extension}" "is missing or not a regular file"
[ -s "${extension}" ] || refuse "${PWD}/${extension}" "is empty"
[ -f "${list}" ] || refuse "${PWD}/${list}" "is missing or not a regular file"
command -v jq >/dev/null 2>&1 || refuse "${PWD}/${list}" "cannot be checked: jq is not on PATH"
jq -e 'type == "object" and (.patterns | type == "array" and length > 0)' "${list}" >/dev/null 2>&1 ||
  refuse "${PWD}/${list}" "is not a JSON object with a non-empty patterns array"

pi="$(command -v pi)" || {
  echo "pi-tester: pi is not on PATH: re-provision the VM (dev/lima.yaml installs it)" >&2
  exit 1
}

# The allow list, one name a line with its reason. A name is copied only when the parent has it set.
allowed="
DEEPSEEK_API_KEY
HOME
PATH
USER
LOGNAME
SHELL
TERM
COLORTERM
LANG
LC_ALL
LC_CTYPE
TMPDIR
XDG_RUNTIME_DIR
XDG_CONFIG_HOME
XDG_STATE_HOME
XDG_CACHE_HOME
XDG_DATA_HOME
DOT_AGENT_DECK_PANE_ID
DOT_AGENT_DECK_PANE_CAPABILITY
DOT_AGENT_DECK_AGENT_ID
DOT_AGENT_DECK_WORK_ID
DOT_AGENT_DECK_VIA_DAEMON
DOT_AGENT_DECK_LAUNCH_NONCE
DOT_AGENT_DECK_RESUME_AGENT
DOT_AGENT_DECK_RESUME_SESSION
DOT_AGENT_DECK_RESUME_TOKEN
DOT_AGENT_DECK_FRESH_AGENT
DOT_AGENT_DECK_FRESH_SESSION
DOT_AGENT_DECK_FRESH_TOKEN
DOT_AGENT_DECK_SOCKET
DOT_AGENT_DECK_EXE
DOT_AGENT_DECK_BIN
DOT_AGENT_DECK_STATE_DIR
DOT_AGENT_DECK_USAGE_FILE
DOT_AGENT_DECK_INBOX_WATCHER_BACKOFF_MS
"
# Why each one:
#   DEEPSEEK_API_KEY      pi's DeepSeek provider credential, the tester's one key (ADR 0003).
#   HOME                  pi's ~/.pi/agent (settings, the deck's extension), git's config, Go's caches, and
#                         the agents App token file the VM's gh reads.
#   PATH                  finds node, go, make, git, gh, jq and dot-agent-deck (dev/lima.yaml's profile).
#   USER, LOGNAME         the account name tools look up without asking the system (git, os/user).
#   SHELL                 the shell pi's bash tool and make fall back to.
#   TERM, COLORTERM       the deck's terminal (the daemon's terminal.conf drop-in), so pi draws its colours.
#   LANG, LC_ALL, LC_CTYPE  the locale, so pi and the tools read and write UTF-8.
#   TMPDIR                the temporary directory, which the deck's socket falls back to without
#                         XDG_RUNTIME_DIR.
#   XDG_RUNTIME_DIR       where the deck's socket lives by default, and the rootless containerd's.
#   XDG_CONFIG_HOME, XDG_STATE_HOME, XDG_CACHE_HOME, XDG_DATA_HOME
#                         where tools keep their files when the system sets them (the deck's state dir).
#   DOT_AGENT_DECK_PANE_ID, _PANE_CAPABILITY, _AGENT_ID, _WORK_ID, _VIA_DAEMON, _LAUNCH_NONCE
#                         the pane's identity and the token that lets its work-done and status updates be
#                         accepted (dot-agent-deck docs configuration, the deck's pi extension); the
#                         capability is scoped to this pane and the deck, no other service.
#   DOT_AGENT_DECK_RESUME_AGENT, _RESUME_SESSION, _RESUME_TOKEN, _FRESH_AGENT, _FRESH_SESSION, _FRESH_TOKEN
#                         the deck's handshake for a pane it resumes or restarts fresh (named beside the pane
#                         id in the deck's binary); scoped to the deck.
#   DOT_AGENT_DECK_SOCKET the daemon's socket, which the deck passes to the agents it starts.
#   DOT_AGENT_DECK_EXE, DOT_AGENT_DECK_BIN
#                         the dot-agent-deck executable the deck's pi extension runs.
#   DOT_AGENT_DECK_STATE_DIR  the deck's state directory, when it is not the default.
#   DOT_AGENT_DECK_USAGE_FILE, DOT_AGENT_DECK_INBOX_WATCHER_BACKOFF_MS
#                         read by the deck's pi extension: where it writes pi's usage, and its inbox
#                         watcher's backoff.
# Left out on purpose: GH_TOKEN, GITHUB_TOKEN and every other GitHub token; ANTHROPIC_API_KEY,
# CLAUDE_CODE_OAUTH_TOKEN and every other agent's key; OTEL_* and DOT_AGENT_DECK_PI_TELEMETRY, because
# OTEL_EXPORTER_OTLP_HEADERS carries the telemetry backend's API key (the tester's pane exports no telemetry
# of pi's own); and no Go variable, since dev/lima.yaml sets none (Go's defaults under HOME apply).

# The positional parameters become: the NAME=value assignments, then pi, then the original arguments.
count=$#
isset=""
value=""
for name in ${allowed}; do
  eval "isset=\${${name}+x}"
  if [ -n "${isset}" ]; then
    eval "value=\${${name}}"
    set -- "$@" "${name}=${value}"
  fi
done
set -- "$@" "${pi}"
while [ "${count}" -gt 0 ]; do
  set -- "$@" "$1"
  shift
  count=$((count - 1))
done

exec env -i "$@"
