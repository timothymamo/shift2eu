#!/usr/bin/env bash
# The required path check of PRD #175 milestone 2 (ADR 0040), run by .github/workflows/audit-paths.yml as
# the job "Audit report paths", which the audit-paths ruleset on main requires (the PRD's runbook step 5).
#
# The audit App, shift2eu-audit[bot], opens docs-only pull requests that add
# docs/security/audits/YYYY-MM-DD-<run_id>.md. Its token holds contents write, which cannot be narrowed to
# a path, so this check fences what such a pull request may land.
#
# The fence applies to an audit pull request: one whose author is shift2eu-audit[bot], one whose sender is
# (the App's contents write lets it push a commit onto another author's pull request), and one whose head
# branch starts with security-audit/ (the branches the App pushes its reports from). The event carries the
# canonical login, so the comparison is an exact match. An audit pull request passes only when its base
# branch is exactly main (one into another branch could take the green check there and be retargeted to
# main), every changed path is under docs/security/ with no path component starting with a dot (a
# .gitattributes there could set export-ignore or export-subst on the reports the release archives ship),
# and every added or modified entry there is a regular, non-executable file, git mode 100644 (no symlink
# 120000, executable 100755 or submodule 160000). A deletion under docs/security/ passes. Every other pull
# request passes trivially: the check exists for that one App and costs nothing elsewhere.
#
# Inputs, from the environment alone:
#   AUDIT_PATHS_AUTHOR    the pull request's author login (the event's pull_request.user.login)
#   AUDIT_PATHS_SENDER    the login of the account whose action raised the event (the event's sender.login)
#   AUDIT_PATHS_HEAD_REF  the pull request's head branch (the event's pull_request.head.ref)
#   AUDIT_PATHS_BASE_REF  the pull request's base branch (the event's pull_request.base.ref)
#   AUDIT_PATHS_BASE      the base commit (the event's pull_request.base.sha)
#   AUDIT_PATHS_HEAD      the pull request's head commit (the event's pull_request.head.sha)
#
# The workflow runs main's copy of this script under pull_request_target, from a checkout of main, and
# fetches the pull request's commits as objects only. So the script reads nothing from the working tree:
# both commits are git objects it diffs, and the tree it runs in is main's, never the pull request's.
#
# It judges the change from git merge-base BASE HEAD to HEAD, so a branch behind main is judged only on
# what it lands. It diffs with git diff-tree, plumbing that reads no diff driver, textconv or external diff
# from any configuration, renames not detected (a rename is a deletion and an addition, both judged),
# NUL-separated (-z), so no character of a name, a newline included, can split one path into two or join
# two. The system and global git configuration are switched off; the repository's own is the checkout's.
#
# It fails closed: an unset or empty author, sender or head branch (for any pull request, since whether
# the fence applies cannot then be decided), for an audit pull request an unset or empty base branch, a
# BASE or HEAD that is unset, malformed or not a commit of the repository, no merge base, a failed diff or
# an entry it cannot read all fail the run. Offending paths are printed with printf %q, so a control
# character is escaped and every output line starts with this script's own text: no name can start a
# workflow command.
#
# Usage: AUDIT_PATHS_AUTHOR=... AUDIT_PATHS_SENDER=... AUDIT_PATHS_HEAD_REF=... AUDIT_PATHS_BASE_REF=...
#        AUDIT_PATHS_BASE=... AUDIT_PATHS_HEAD=... .github/scripts/audit-paths.sh
#        (from inside the repository that holds both commits)
set -euo pipefail

readonly audit_author='shift2eu-audit[bot]'
readonly audit_branch_prefix='security-audit/'
readonly allowed_base_ref='main'
readonly allowed_prefix='docs/security/'
readonly allowed_mode='100644'

export GIT_CONFIG_NOSYSTEM=1
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_PAGER=cat
export LC_ALL=C
unset GIT_EXTERNAL_DIFF GIT_DIFF_OPTS GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
  GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_CONFIG GIT_CONFIG_PARAMETERS GIT_CONFIG_COUNT

fail() {
  echo >&2 "audit-paths: $*"
  exit 1
}

# required prints the value of the variable named $1, or fails the run when it is unset or empty; $2 says
# what the run cannot then tell.
required() {
  local name="$1" value
  value="${!name:-}"
  if [ -z "${value}" ]; then
    fail "${name} is unset or empty: cannot tell $2, so the check fails closed"
  fi
  printf '%s' "${value}"
}

author="$(required AUDIT_PATHS_AUTHOR 'who opened the pull request')"
sender="$(required AUDIT_PATHS_SENDER 'who pushed to the pull request')"
head_ref="$(required AUDIT_PATHS_HEAD_REF 'which branch the pull request comes from')"

if [ "${author}" != "${audit_author}" ] && [ "${sender}" != "${audit_author}" ] &&
  [[ "${head_ref}" != "${audit_branch_prefix}"* ]]; then
  printf 'audit-paths: neither the author %q nor the sender %q is %s, and the head branch %q is not under %s, so the check does not apply: pass\n' \
    "${author}" "${sender}" "${audit_author}" "${head_ref}" "${audit_branch_prefix}"
  exit 0
fi

base_ref="$(required AUDIT_PATHS_BASE_REF 'which branch the audit pull request lands on')"
if [ "${base_ref}" != "${allowed_base_ref}" ]; then
  fail "$(printf 'the audit pull request is based on %q, not %s: an audit pull request lands on %s alone, so the check fails closed' \
    "${base_ref}" "${allowed_base_ref}" "${allowed_base_ref}")"
fi

# commit_of prints the full SHA of the commit the variable named $1 holds, or fails the run.
commit_of() {
  local name="$1" value
  value="${!name:-}"
  if [ -z "${value}" ]; then
    fail "${name} is unset or empty: cannot tell what the pull request changes, so the check fails closed"
  fi
  if ! [[ "${value}" =~ ^[0-9a-f]{40}([0-9a-f]{24})?$ ]]; then
    fail "$(printf '%s is %q, not a full commit SHA: the check fails closed' "${name}" "${value}")"
  fi
  if ! git rev-parse --quiet --verify "${value}^{commit}"; then
    fail "${name} is ${value}, not a commit of this repository: the check fails closed"
  fi
}

base="$(commit_of AUDIT_PATHS_BASE)"
head="$(commit_of AUDIT_PATHS_HEAD)"

if ! merge_base="$(git merge-base "${base}" "${head}")" || [ -z "${merge_base}" ]; then
  fail "${base} and ${head} have no merge base: the check fails closed"
fi

work="$(mktemp -d)"
trap 'rm -rf -- "${work}"' EXIT
diff_file="${work}/diff"

if ! git diff-tree -r -z --raw --no-renames --no-ext-diff --no-textconv \
  "${merge_base}" "${head}" >"${diff_file}"; then
  fail "git diff-tree ${merge_base} ${head} failed: the check fails closed"
fi

# Each entry of the raw -z output is ":<old mode> <new mode> <old sha> <new sha> <status>" NUL "<path>" NUL;
# with renames and copies off, every entry has exactly one path.
offending=0
entries=0
while IFS= read -r -d '' meta; do
  if ! IFS= read -r -d '' path; then
    fail "git diff-tree's output ends inside an entry: the check fails closed"
  fi
  entries=$((entries + 1))

  if ! [[ "${meta}" =~ ^:([0-7]{6})\ ([0-7]{6})\ [0-9a-f]+\ [0-9a-f]+\ ([A-Z])$ ]]; then
    fail "$(printf 'cannot read the diff entry %q for %q: the check fails closed' "${meta}" "${path}")"
  fi
  new_mode="${BASH_REMATCH[2]}"
  status="${BASH_REMATCH[3]}"

  if [[ "${path}" != "${allowed_prefix}"?* ]]; then
    printf 'audit-paths: outside %s (%s): %q\n' "${allowed_prefix}" "${status}" "${path}"
    offending=$((offending + 1))
    continue
  fi

  if [[ "/${path}" == */.* ]]; then
    printf 'audit-paths: a path component under %s starts with a dot (%s): %q\n' "${allowed_prefix}" "${status}" "${path}"
    offending=$((offending + 1))
    continue
  fi

  if [ "${status}" != "D" ] && [ "${new_mode}" != "${allowed_mode}" ]; then
    printf 'audit-paths: mode %s, not a regular non-executable file %s (%s): %q\n' \
      "${new_mode}" "${allowed_mode}" "${status}" "${path}"
    offending=$((offending + 1))
  fi
done <"${diff_file}"

# entries_text prints "1 changed entry" or "<n> changed entries".
entries_text() {
  if [ "$1" -eq 1 ]; then
    echo "1 changed entry"
  else
    echo "$1 changed entries"
  fi
}

if [ "${offending}" -ne 0 ]; then
  fail "the fence is broken by ${offending} of the pull request's $(entries_text "${entries}"): an audit pull request may add or change only regular files under ${allowed_prefix}, none of them a dot file"
fi

echo "audit-paths: $(entries_text "${entries}"), all regular files under ${allowed_prefix} (or deletions there): pass"
