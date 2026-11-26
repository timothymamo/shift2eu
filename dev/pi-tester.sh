#!/bin/sh
# Starts pi for the tester role (issue #662,
# docs/adr/0044-refuse-printing-the-environment-in-pi-with-a-project-extension.md): .dot-agent-deck.toml's
# tester command runs this script, never pi itself.
#
# 1. Fails closed. pi logs a project extension that fails to load and runs without it, so the env-printing
#    refusal (ADR 0044) would fail open on its own. Before pi starts, the two files the refusal is made of
#    are checked, relative to the working directory pi loads the project from (the deck starts the pane in
#    the unit's folder):
#    - .pi/extensions/deny-env-printing.ts must be a non-empty regular file, and .pi/deny-env-printing.json
#      a JSON object whose "patterns" is a non-empty array (checked with jq, which dev/lima.yaml installs);
#    - then node (from PATH, the one dev/lima.yaml pins, under the allow-listed environment below) imports
#      the extension and requires a function as its default export, which is what pi calls, and compiles
#      every pattern of the list with `new RegExp`, as the extension does. The VM's node (22.18 or later)
#      strips TypeScript types on import by default, so no flag is needed; the load is the one
#      test/architecture/pienvdenyload_test.go's piNodeLoadScript makes.
#    Anything else - node missing too - exits 1 with a message that names the file, and pi is not started.
#    Only node's exit status is read: its output is discarded, so nothing the load prints reaches the pane.
#    pi (1.6.1, the release dev/lima.yaml pins) has no option that makes a failed extension load fatal, so
#    these checks are the ones that do it.
# 2. Starts pi with an allow-listed environment: `env -i` and only the variables named below, each copied
#    when the parent has it set, plus the marker SHIFT2EU_PI_TESTER=1, every argument passed through
#    unchanged. Everything else - a GitHub token, another agent's key (ANTHROPIC_API_KEY,
#    CLAUDE_CODE_OAUTH_TOKEN), the OpenTelemetry headers that carry a backend's API key, any name this list
#    does not know - is not in pi's environment, so printing it shows only the DeepSeek key and the pane's
#    variables. The marker makes the VM's login profile (/etc/profile.d/shift2eu.sh, dev/lima.yaml) leave
#    the agents' keys file unsourced, so a `bash -l` from the tester's bash tool does not put them back.
#
# What this does not do: the tester still runs as the VM's one user, so the keys file, the agents App's
# token file and other processes' /proc/<pid>/environ stay readable to it, guarded only by the deny
# patterns of .pi/deny-env-printing.json, which a program the tester writes and runs can bypass. Keeping
# them from it needs a separate uid or the egress stage (ADR 0044, Alternatives). gh and git push keep
# working for a command the tester runs: the VM's gh (dev/gh-app-token.sh, ADR 0031) reads the App's token
# from its file under HOME, not from the environment.
#
# PATH is checked first, before anything that looks a command up: an empty entry (a leading, middle or
# trailing `:`) or one that is not absolute (`.`, `bin`) is the working directory or under it, the unit's
# folder, so a command the wrapper, node or pi then finds could be one from there. Such a PATH exits 1 with a
# message that names PATH (never its value), and nothing is started. The VM's PATH (dev/lima.yaml's profile)
# has none, so it passes, and is then copied as it is.
#
# No value is ever printed: the messages name files, never a variable's value.
#
# Usage: dev/pi-tester.sh <pi arguments...>   (from the directory pi loads the project from)
set -eu

# refuse_path <why> stops the wrapper on a PATH that could find a command in the working directory.
refuse_path() {
  printf 'pi-tester: PATH %s: the tester is not started, because a command found on it could be one from the working directory, the unit folder (issue #662)\n' \
    "$1" >&2
  exit 1
}

case ":${PATH:-}:" in
*::*) refuse_path "has an empty entry (a leading, doubled or trailing colon)" ;;
esac
path_entries=${PATH}
while [ -n "${path_entries}" ]; do
  path_entry=${path_entries%%:*}
  case "${path_entries}" in
  *:*) path_entries=${path_entries#*:} ;;
  *) path_entries="" ;;
  esac
  case "${path_entry}" in
  /*) ;;
  *) refuse_path "has an entry that is not an absolute path" ;;
  esac
done

extension=.pi/extensions/deny-env-printing.ts
list=.pi/deny-env-printing.json

# refuse <file> <why> stops the wrapper before pi starts.
refuse() {
  printf 'pi-tester: %s %s: the tester is not started, because pi would run without the env-printing refusal (issue #662, docs/adr/0044-refuse-printing-the-environment-in-pi-with-a-project-extension.md)\n' \
    "$1" "$2" >&2
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
#   LANG, LC_ALL, LC_CTYPE  the locale, so pi and the tools read and write UTF-8. LANG sets every other
#                         LC_* category that is not set itself, so the others are left out.
#   TMPDIR                the temporary directory, which the deck's socket falls back to without
#                         XDG_RUNTIME_DIR.
#   XDG_RUNTIME_DIR       where the deck's socket lives by default, and the rootless containerd's.
#   XDG_CONFIG_HOME, XDG_STATE_HOME, XDG_CACHE_HOME, XDG_DATA_HOME
#                         where tools keep their files when the system sets them (the deck's state dir).
#   DOT_AGENT_DECK_PANE_ID, _PANE_CAPABILITY, _AGENT_ID, _VIA_DAEMON
#                         the pane's identity and the token that lets its work-done and status updates be
#                         accepted (dot-agent-deck docs configuration, the deck's pi extension); the
#                         capability is scoped to this pane and the deck, no other service.
#   DOT_AGENT_DECK_WORK_ID, _LAUNCH_NONCE, _RESUME_AGENT, _RESUME_SESSION, _RESUME_TOKEN, _FRESH_AGENT,
#   _FRESH_SESSION, _FRESH_TOKEN
#                         kept for the deck binary's launch and resume path: the names were found in the
#                         binary's strings beside the pane id, and what reads them is unverified, so a later
#                         trim can test dropping them one by one. Scoped to the deck.
#   DOT_AGENT_DECK_SOCKET the daemon's socket, which the deck passes to the agents it starts.
#   DOT_AGENT_DECK_EXE, DOT_AGENT_DECK_BIN
#                         the dot-agent-deck executable the deck's pi extension runs.
#   DOT_AGENT_DECK_STATE_DIR  the deck's state directory, when it is not the default.
#   DOT_AGENT_DECK_USAGE_FILE, DOT_AGENT_DECK_INBOX_WATCHER_BACKOFF_MS
#                         read by the deck's pi extension: where it writes pi's usage, and its inbox
#                         watcher's backoff.
# Set by the wrapper itself, whatever the parent has:
#   SHIFT2EU_PI_TESTER=1  the marker the VM's login profile reads to leave the agents' keys file unsourced.
# Left out on purpose: GH_TOKEN, GITHUB_TOKEN and every other GitHub token; ANTHROPIC_API_KEY,
# CLAUDE_CODE_OAUTH_TOKEN and every other agent's key; OTEL_* and DOT_AGENT_DECK_PI_TELEMETRY, because
# OTEL_EXPORTER_OTLP_HEADERS carries the telemetry backend's API key (the tester's pane exports no telemetry
# of pi's own); and no Go variable, since dev/lima.yaml sets none (Go's defaults under HOME apply).

# allowlisted <run|exec> <command> [<argument>...] runs the command under `env -i` and the allow list alone.
# Inside the function the positional parameters become: the NAME=value assignments, the marker, then the
# command and its arguments, each unchanged.
allowlisted() {
  mode=$1
  shift
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
  set -- "$@" "SHIFT2EU_PI_TESTER=1"
  while [ "${count}" -gt 0 ]; do
    set -- "$@" "$1"
    shift
    count=$((count - 1))
  done
  if [ "${mode}" = exec ]; then
    exec env -i "$@"
  fi
  env -i "$@"
}

# The extension's load, as pi makes it: an import whose default export must be a function.
load_extension='
const { pathToFileURL } = await import("node:url");
const module = await import(pathToFileURL(process.argv[1]).href);
if (typeof module.default !== "function") {
  process.exit(1);
}
'

# The list's patterns, compiled as the extension compiles them: each a string and a regular expression.
compile_patterns='
const { readFileSync } = await import("node:fs");
const doc = JSON.parse(readFileSync(process.argv[1], "utf8"));
if (!Array.isArray(doc.patterns) || doc.patterns.length === 0) {
  process.exit(1);
}
for (const pattern of doc.patterns) {
  if (typeof pattern !== "string") {
    process.exit(1);
  }
  new RegExp(pattern);
}
'

[ -f "${extension}" ] || refuse "${PWD}/${extension}" "is missing or not a regular file"
[ -s "${extension}" ] || refuse "${PWD}/${extension}" "is empty"
[ -f "${list}" ] || refuse "${PWD}/${list}" "is missing or not a regular file"
command -v jq >/dev/null 2>&1 || refuse "${PWD}/${list}" "cannot be checked: jq is not on PATH"
jq -e 'type == "object" and (.patterns | type == "array" and length > 0)' "${list}" >/dev/null 2>&1 ||
  refuse "${PWD}/${list}" "is not a JSON object with a non-empty patterns array"

node="$(command -v node)" ||
  refuse "${PWD}/${extension}" "cannot be loaded: node is not on PATH (re-provision the VM: dev/lima.yaml installs it)"
allowlisted run "${node}" --input-type=module -e "${load_extension}" "${PWD}/${extension}" >/dev/null 2>&1 ||
  refuse "${PWD}/${extension}" "does not load in node with a function as its default export (make test runs TestPiEnvDenyExtensionLoads, which says why)"
allowlisted run "${node}" --input-type=module -e "${compile_patterns}" "${PWD}/${list}" >/dev/null 2>&1 ||
  refuse "${PWD}/${list}" "has a pattern node cannot compile as a regular expression"

pi="$(command -v pi)" || {
  echo "pi-tester: pi is not on PATH: re-provision the VM (dev/lima.yaml installs it)" >&2
  exit 1
}

allowlisted exec "${pi}" "$@"
