#!/bin/sh
# Starts pi for the tester role (issue #662, issue #692,
# docs/adr/0044-refuse-printing-the-environment-in-pi-with-a-project-extension.md): .dot-agent-deck.toml's
# tester command runs this script, never pi itself, and runs the copy dev/lima.yaml installs root-owned:
#   /usr/local/lib/shift2eu/pi-tester/pi-tester.sh                         this script
#   /usr/local/lib/shift2eu/pi-tester/extensions/deny-env-printing.ts      .pi/extensions/deny-env-printing.ts
#   /usr/local/lib/shift2eu/pi-tester/deny-env-printing.json               .pi/deny-env-printing.json
# The unit's folder, where the deck starts the pane, is writable by the agents, so a branch could weaken its
# copies; the installed ones change only on a VM rebuild or make vm-pi-tester, which the maintainer runs.
#
# 0. Reads the refusal's two files from the directory this script is installed in (the directory of $0,
#    which must be an absolute path), never from the working directory. When a copy in the working directory
#    (.pi/extensions/deny-env-printing.ts, .pi/deny-env-printing.json, dev/pi-tester.sh) is missing or not
#    byte for byte the installed one, it is not used for the checks or for --extension: one line on stderr
#    names the installed and the project paths and make vm-pi-tester, and pi starts with the installed files.
#    pi loads no extension and no trusted setting of the project (item 2). The line is expected on a branch
#    that changes one of the three files.
# 1. Fails closed. pi logs an extension that fails to load and runs without it, so the env-printing refusal
#    (ADR 0044) would fail open on its own. Before pi starts, the two installed files the refusal is made of
#    are checked:
#    - extensions/deny-env-printing.ts must be a non-empty regular file, and deny-env-printing.json a JSON
#      object whose "patterns" is a non-empty array (checked with jq, which dev/lima.yaml installs);
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
#    when the parent has it set, plus the marker SHIFT2EU_PI_TESTER=1, and the arguments
#    `--no-approve --no-extensions --extension <installed extensions/deny-env-printing.ts>
#    --extension <HOME>/.pi/agent/extensions/dot-agent-deck --skill <working directory>/.claude/skills`
#    followed by every other argument passed through unchanged (issue #702, ADR 0044 amendment). pi then
#    trusts no project: it applies no trust-gated setting of the project's .pi/settings.json, whose
#    shellCommandPrefix, packages and npmCommand could run commands the refusal never sees, and discovers
#    no extension of the project or of ~/.pi/agent; it loads exactly the installed refusal and the deck's
#    extension, and the skills by --skill. pi still reads the project's .pi/settings.json as data before
#    trust (sessionDir, the startup UI, first-time setup): see What this does not do.
#    An --approve or -a among the arguments is dropped with one line on stderr each, since the last of
#    --approve and --no-approve wins; units cut before the change still pass it. After a -- pi reads it as
#    message text, which could not re-trust the project: it is dropped there too only to keep the rule
#    simple. A -a given as another option's value (--name -a) is dropped as well; the deck never passes one.
#    Before the deck's extension, pi's agent directory ${HOME}/.pi/agent, whose settings, models and system
#    prompt pi always trusts, is checked: neither ~/.pi nor ~/.pi/agent may be a symbolic link, it must be a
#    directory, and its physical path (cd -P) must lie outside the working directory's physical path (pwd -P),
#    so a HOME spelled <parent>/./<unit>/h, <parent>//<unit>/h or with a .. before the unit folder's name,
#    or reaching the unit folder through a symbolic link, is refused too.
#    The deck's extension must lie under HOME (an absolute path), outside the working directory by its
#    spelling and by its physical path, be a directory reached through no symbolic link (neither it nor
#    ~/.pi/agent/extensions), and hold an index.ts, index.js or package.json that is a regular file and no
#    symbolic link. Otherwise the wrapper exits 1 with one line naming the directory and why, and for the
#    deck's extension the step that writes it (dot-agent-deck orchestrator setup). Everything else -
#    a GitHub token, another agent's key (ANTHROPIC_API_KEY, CLAUDE_CODE_OAUTH_TOKEN), the OpenTelemetry
#    headers that carry a backend's API key, any name this list does not know - is not in pi's environment,
#    so printing it shows only the DeepSeek key and the pane's variables.
#    The marker makes the VM's login profile (/etc/profile.d/shift2eu.sh, dev/lima.yaml) leave the agents'
#    keys file unsourced, so a `bash -l` from the tester's bash tool does not put them back.
#
# What this does not do: the tester still runs as the VM's one user, so the keys file, the agents App's
# token file and other processes' /proc/<pid>/environ stay readable to it, guarded only by the deny
# patterns of .pi/deny-env-printing.json, which a program the tester writes and runs can bypass. Keeping
# them from it needs a separate uid or the egress stage (ADR 0044, Alternatives). The skills under
# <working directory>/.claude/skills are still the branch's text, which pi reads as instructions (not code).
# gh and git push keep working for a command the tester runs: the VM's gh (dev/gh-app-token.sh, ADR 0031)
# reads the App's token from its file under HOME, not from the environment.
# pi still reads the project's .pi/settings.json as data before trust (sessionDir, the startup UI, first-time
# setup), and its migrations rename <working directory>/.pi/commands to .pi/prompts before trust; a branch's
# .pi/hooks or .pi/tools makes an interactive pi show a deprecation warning and wait for a key, which stalls
# an unattended pane. No code of the project runs. A follow-up can pin --session-dir.
# The user-level config under ~/.pi/agent - settings.json (shellCommandPrefix, shellPath, packages,
# npmCommand), models.json, SYSTEM.md, APPEND_SYSTEM.md, AGENTS.md - is always trusted and writable by any
# process of the same uid, branch code that make check runs included, so a write there runs at the next
# tester start, outside the refusal. A refusal of those keys was considered and not taken: it could break a
# legitimate deck setup and moves no trust boundary while the tester shares the uid.
# The later arguments of the deck's tester command (--extension, --session-dir, --system-prompt and the
# like) come from .dot-agent-deck.toml, which TestPiTesterCommandRunsTheLaunchWrapper pins word for word.
#
# PATH is checked first, before anything that looks a command up: an empty entry (a leading, middle or
# trailing `:`) or one that is not absolute (`.`, `bin`) is the working directory or under it, the unit's
# folder, so a command the wrapper, node or pi then finds could be one from there. Such a PATH exits 1 with a
# message that names PATH (never its value), and nothing is started. The VM's PATH (dev/lima.yaml's profile)
# has none, so it passes, and is then copied as it is.
#
# The refusals print paths built from $0, PWD and HOME; no variable's value on its own and no credential is
# ever printed.
#
# Usage: /usr/local/lib/shift2eu/pi-tester/pi-tester.sh <pi arguments...>
#        (by its absolute path, from the directory pi loads the project from)
set -eu

# refuse_path <why> stops the wrapper on a PATH that could find a command in the working directory.
refuse_path() {
  printf 'pi-tester: PATH %s: the tester is not started, because a command found on it could be one from the working directory, the unit folder (issue #662)\n' \
    "$1" >&2
  exit 1
}

case "${PATH:-}" in
'' | :* | *: | *:[:]*) refuse_path "has an empty entry (a leading, doubled or trailing colon)" ;;
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

# The installed layout, read beside this script's own path, never from the working directory (issue #692).
case "$0" in
/*/*) ;;
*)
  echo "pi-tester: not started by its absolute path, so it cannot find its installed files: run /usr/local/lib/shift2eu/pi-tester/pi-tester.sh (issue #692)" >&2
  exit 1
  ;;
esac
installed=${0%/*}
script=$0
extension=${installed}/extensions/deny-env-printing.ts
list=${installed}/deny-env-printing.json
skills=${PWD}/.claude/skills

# --approve and -a are dropped wherever they stand, one line each in their order: pi starts with --no-approve,
# and an --approve after it would win. After a -- pi reads one as message text, which could not re-trust the
# project; it is dropped there too only to keep the rule simple. A -a given as another option's value
# (--name -a) is dropped as well; the deck never passes one. Every other argument is kept, unchanged and in order.
for arg; do
  shift
  case "${arg}" in
  --approve | -a)
    printf "pi-tester: argument %s dropped: the tester's pi never trusts the project, because a trusted .pi/settings.json can run commands the env-printing refusal does not see (shellCommandPrefix, packages, npmCommand) (issue #702)\n" \
      "${arg}" >&2
    ;;
  *) set -- "$@" "${arg}" ;;
  esac
done

# The deck's pi extension, where the deck writes it: always under HOME, never derived from the working directory.
case "${HOME:-}" in
/*) ;;
*)
  echo "pi-tester: HOME is not an absolute path, so the deck's pi extension cannot be found: the tester is not started (issue #702)" >&2
  exit 1
  ;;
esac
deck=${HOME}/.pi/agent/extensions/dot-agent-deck

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
# Left out on purpose: GH_TOKEN and every other GitHub token variable; ANTHROPIC_API_KEY,
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

[ -f "${extension}" ] || refuse "${extension}" "is missing or not a regular file"
[ -s "${extension}" ] || refuse "${extension}" "is empty"
[ -f "${list}" ] || refuse "${list}" "is missing or not a regular file"
command -v jq >/dev/null 2>&1 || refuse "${list}" "cannot be checked: jq is not on PATH"
jq -e 'type == "object" and (.patterns | type == "array" and length > 0)' "${list}" >/dev/null 2>&1 ||
  refuse "${list}" "is not a JSON object with a non-empty patterns array"

node="$(command -v node)" ||
  refuse "${extension}" "cannot be loaded: node is not on PATH (re-provision the VM: dev/lima.yaml installs it)"
allowlisted run "${node}" --input-type=module -e "${load_extension}" "${extension}" >/dev/null 2>&1 ||
  refuse "${extension}" "does not load in node with a function as its default export (make test runs TestPiEnvDenyExtensionLoads, which says why)"
allowlisted run "${node}" --input-type=module -e "${compile_patterns}" "${list}" >/dev/null 2>&1 ||
  refuse "${list}" "has a pattern node cannot compile as a regular expression"

pi="$(command -v pi)" || {
  echo "pi-tester: pi is not on PATH: re-provision the VM (dev/lima.yaml installs it)" >&2
  exit 1
}

# The working directory's physical path, every link resolved; empty when it cannot be resolved, which
# in_unit then reads as the unit folder (fail closed).
pwd_real=$(pwd -P) || pwd_real=""

# in_unit <directory> succeeds when the directory's physical path is the working directory's or lies below it,
# and when either path cannot be resolved (fail closed). A working directory of / holds every path.
in_unit() {
  [ -n "${pwd_real}" ] || return 0
  dir_real=$(cd -P -- "$1" 2>/dev/null && pwd -P) || return 0
  [ -n "${dir_real}" ] || return 0
  case "${dir_real%/}/" in
  "${pwd_real%/}/"*) return 0 ;;
  esac
  return 1
}

# pi's agent directory, where pi keeps the settings, the models and the system prompt it always trusts, whatever
# --no-approve and --no-extensions say. The deck's extension lies in it.
agent=${HOME}/.pi/agent

# refuse_agent <why> stops the wrapper when pi's agent directory could be the unit folder's.
refuse_agent() {
  printf "pi-tester: pi's agent directory %s %s: the tester is not started, because pi trusts the settings and extensions it finds there (issue #702)\n" \
    "${agent}" "$1" >&2
  exit 1
}

# The agent directory must be a real directory reached through no link, and physically outside the working
# directory: a HOME spelled <parent>/./<unit>/h, <parent>//<unit>/h or with a .. before the unit folder's
# name, or reaching the unit folder through a symbolic link, passes a string comparison and is still the unit
# folder.
if [ -L "${HOME}/.pi" ] || [ -L "${agent}" ]; then
  refuse_agent "is reached through a symbolic link"
fi
[ -d "${agent}" ] || refuse_agent "is missing or not a directory"
! in_unit "${agent}" || refuse_agent "lies in the working directory, the unit folder"

# refuse_deck <why> stops the wrapper when the deck's pi extension is not where the deck writes it.
refuse_deck() {
  printf "pi-tester: the deck's pi extension %s %s: the tester is not started, because without it the pane gets no task and its work-done is not delivered; run dot-agent-deck orchestrator setup, then restart the pane (issue #702)\n" \
    "${deck}" "$1" >&2
  exit 1
}

# entry_point <file> succeeds for a regular file that is no symbolic link: the deck writes its entry point there.
entry_point() {
  [ -f "$1" ] && [ ! -L "$1" ]
}

# The deck's extension must lie outside the working directory, which a branch writes, by its spelling and by its
# physical path, be a real directory reached through no link, and hold an entry point pi can load. Its content
# is not read: it is the deck's, written by its setup.
unit_prefix=${PWD%/}/
case "${deck}" in
"${PWD}" | "${unit_prefix}"*) refuse_deck "lies in the working directory, the unit folder" ;;
esac
[ ! -L "${agent}/extensions" ] || refuse_deck "is reached through a symbolic link, which the deck never writes"
[ ! -L "${deck}" ] || refuse_deck "is a symbolic link, which the deck never writes"
[ -d "${deck}" ] || refuse_deck "is missing or not a directory"
! in_unit "${deck}" || refuse_deck "lies in the working directory, the unit folder"
entry_point "${deck}/index.ts" || entry_point "${deck}/index.js" || entry_point "${deck}/package.json" ||
  refuse_deck "has no index.ts, index.js or package.json"

# The project's copies: each one missing or not byte for byte the installed file is named in one line, with the
# installed file and the update step; the checks and --extension use the installed files, and pi loads no
# extension and no trusted setting of the project. Only paths are printed, never a file's content.
differs=""
# compare_copy <installed file> <project file> notes the project file when it is not the installed one.
compare_copy() {
  if ! cmp -s "$1" "$2"; then
    differs="${differs}${differs:+; }$2 (installed: $1)"
  fi
}
compare_copy "${script}" "${PWD}/dev/pi-tester.sh"
compare_copy "${extension}" "${PWD}/.pi/extensions/deny-env-printing.ts"
compare_copy "${list}" "${PWD}/.pi/deny-env-printing.json"
if [ -n "${differs}" ]; then
  printf 'pi-tester: project copies missing or different from the installed ones: %s (expected on a branch that changes them); the checks and --extension use the installed copies, and pi loads no extension and no trusted setting of the project (--no-extensions, --no-approve: ADR 0044, issue #702); to install the project copies after review, the maintainer runs make vm-pi-tester (issue #692)\n' \
    "${differs}" >&2
fi

allowlisted exec "${pi}" --no-approve --no-extensions --extension "${extension}" --extension "${deck}" --skill "${skills}" "$@"
