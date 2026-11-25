#!/usr/bin/env bash
# Creates the git worktree for a branch, or finds it when it already exists, and prints its absolute
# path. Every pull request is built on its own branch in its own worktree under .worktrees/, never on
# main (docs/adr/0004-one-worktree-per-pull-request.md).
#
# The new worktree goes under the .worktrees/ of the checkout this runs in: the main checkout, or a
# dispatch worktree that dot-agent-deck created beside it, whose agents can then reach it without leaving
# their working directory (docs/adr/0010-create-pull-request-worktrees-inside-the-dispatch-worktree.md).
# Run from inside a pull request's worktree, it uses the checkout that holds that worktree, so pull
# request worktrees are never nested in each other.
#
# It prints only a pull request's worktree, a linked worktree directly under a .worktrees/, and refuses
# anything else in the branch's way. Every command it suggests acts on one named worktree, never on the
# whole repository.
#
# A Renovate pull request's branch, renovate/<name>, is only ever resumed, never created: the script
# accepts it only when origin/renovate/<name> exists after its fetch of origin, or when the branch already
# has its worktree, and resumes a local branch only at origin's commit and tracking it (issue #573).
#
# Usage: dev/worktree.sh <type>/<issue>-<slug>   (or: make worktree BRANCH=<type>/<issue>-<slug>)
#        dev/worktree.sh renovate/<name>         (or: make worktree BRANCH=renovate/<name>)
set -euo pipefail

branch="${1:-}"
types='feat|fix|test|docs|refactor|perf|build|ci|chore|prd'
renovate=''
if [[ "${branch}" =~ ^renovate/[a-z0-9][a-z0-9._-]*$ && "${branch}" != *..* ]] &&
  git check-ref-format "refs/heads/${branch}"; then
  renovate=1
elif [[ ! "${branch}" =~ ^(${types})/[0-9]+-[a-z0-9]+(-[a-z0-9]+)*$ ]]; then
  echo "usage: make worktree BRANCH=<type>/<issue>-<slug>, for example feat/12-aws-ec2-discovery" >&2
  echo "<type> is one of: ${types//|/, }" >&2
  echo "or, to resume a Renovate pull request's existing branch: make worktree BRANCH=renovate/<name>," \
    "<name> of lower-case letters, digits, dots, underscores and hyphens, holding no .. and" \
    "a name git accepts for a branch" >&2
  exit 2
fi
slug="${branch//\//-}"

# Every worktree of the repository, read whole before it is searched: git gets SIGPIPE when a reader
# stops early, and with pipefail that would end this script without a message. One entry per worktree,
# the main worktree first: a "worktree <path>" line, then lines such as "HEAD <sha>", "branch <ref>" or
# "detached", "locked [<reason>]" and "prunable <reason>", then a blank line.
worktrees="$(git worktree list --porcelain)"

# entry <line>: prints the first entry that has exactly this line, or nothing. The main worktree's entry
# gets an extra first line, "main". The line reaches awk through the environment, because awk -v would
# expand the backslash escapes in it.
entry() {
  want="$1" awk '
    BEGIN { RS = ""; FS = "\n" }
    !found {
      for (i = 1; i <= NF; i++) if ($i == ENVIRON["want"]) found = 1
      if (found) { if (NR == 1) print "main"; print }
    }
  ' <<<"${worktrees}"
}

# detached_at: prints the first linked worktree on a detached HEAD whose directory is .worktrees/<slug>
# in any checkout, or nothing.
detached_at() {
  suffix="/.worktrees/${slug}" awk '
    BEGIN { RS = ""; FS = "\n"; suffix = ENVIRON["suffix"] }
    !found && NR > 1 {
      path = substr($1, length("worktree ") + 1)
      if (length(path) > length(suffix) && substr(path, length(path) - length(suffix) + 1) == suffix)
        for (i = 2; i <= NF; i++) if ($i == "detached") found = 1
      if (found) print
    }
  ' <<<"${worktrees}"
}

path_of() { sed -n 's/^worktree //p' <<<"$1"; }

refuse() {
  echo "worktree: $*" >&2
  exit 1
}

# refuse_broken <entry>: refuses an entry whose directory no longer exists, that git marks prunable, or
# whose directory git does not see as that worktree (a locked entry is never marked prunable), and
# returns for a healthy one. For a missing directory it names the commands that clear that one entry. It
# never suggests 'git worktree prune', which clears every stale entry of the repository, including those
# of healthy worktrees on a disk that is not mounted
# (docs/adr/0008-mount-a-workspace-directory-for-dispatcher-mode.md).
refuse_broken() {
  local path quoted reason actual where=''
  path="$(path_of "$1")"
  printf -v quoted '%q' "${path}"
  if [[ -d "${path}" ]]; then
    actual="$(git -C "${path}" rev-parse --show-toplevel 2>/dev/null || true)"
    if [[ -z "${actual}" ]]; then
      where="git commands fail there"
    elif [[ "${actual}" != "${path}" ]]; then
      where="git commands run there act on ${actual}"
    fi
    if [[ $'\n'"$1" == *$'\n'prunable* ]]; then
      reason="$(sed -n 's/^prunable *//p' <<<"$1")"
      refuse "${path} is registered as a worktree, but git marks the entry prunable (${reason:-no reason" \
        "given})${where:+, and ${where}}. It may still hold work, so leave it where it is and report it"
    fi
    if [[ -n "${where}" ]]; then
      refuse "${path} is registered as a worktree, but ${where}. It may still hold work, so leave it" \
        "where it is and report it"
    fi
    return 0
  fi
  if [[ $'\n'"$1" == *$'\n'locked* ]]; then
    reason="$(sed -n 's/^locked *//p' <<<"$1")"
    refuse "${path} is registered as a worktree but no longer exists, and the entry is" \
      "locked${reason:+ (${reason})}. Unless ${path} is on a disk that is not mounted, run" \
      "'git worktree unlock ${quoted}', then 'git worktree remove ${quoted}', and try again"
  fi
  refuse "${path} is registered as a worktree but no longer exists; run 'git worktree remove ${quoted}'" \
    "and try again"
}

# refuse_detached <dir>: refuses the worktree at <dir>, which is on a detached HEAD.
refuse_detached() {
  local quoted
  printf -v quoted '%q' "$1"
  refuse "$1 is a worktree on a detached HEAD, not on '${branch}': a rebase or bisect may be in progress" \
    "there, or it was detached by hand, and 'git -C ${quoted} status' says which. It may hold the" \
    "branch's work, so leave it where it is and report it"
}

# Resume: a branch that already has a pull request's worktree gets that worktree's path, wherever it is.
found="$(entry "branch refs/heads/${branch}")"
if [[ -n "${found}" ]]; then
  existing="$(path_of "${found}")"
  if [[ "${found%%$'\n'*}" == main ]]; then
    refuse "'${branch}' is checked out in the main checkout, ${existing}, which stays on main and is" \
      "never a pull request's worktree (CLAUDE.md rule 4); leave the main checkout as it is and report it"
  fi
  if [[ "$(basename "$(dirname "${existing}")")" != .worktrees ]]; then
    refuse "'${branch}' is checked out in ${existing}, which is not a pull request worktree (one directly" \
      "under a .worktrees/); leave it where it is and report it"
  fi
  refuse_broken "${found}"
  echo "${existing}"
  exit 0
fi

# A worktree on a detached HEAD at .worktrees/<slug>, in any checkout, is this branch's worktree in the
# middle of a rebase or bisect, or detached by hand. Git refuses a second worktree for a branch that is
# being rebased or bisected, but not for one detached by hand, so this refuses all of them.
held="$(detached_at)"
if [[ -n "${held}" ]]; then
  refuse_broken "${held}"
  refuse_detached "$(path_of "${held}")"
fi

# The checkout this runs in. A linked worktree directly under a .worktrees/ is a pull request's worktree,
# and stands for the checkout that holds it.
top="$(git rev-parse --show-toplevel)"
if [[ "$(git rev-parse --path-format=absolute --git-dir)" != "$(git rev-parse --path-format=absolute --git-common-dir)" &&
  "$(basename "$(dirname "${top}")")" == ".worktrees" ]]; then
  top="$(dirname "$(dirname "${top}")")"
fi
dir="${top}/.worktrees/${slug}"

if [[ -e "${dir}" ]]; then
  held="$(entry "worktree ${dir}")"
  if [[ -z "${held}" ]]; then
    refuse "${dir} exists but is not a worktree; look at what it holds, move it out of the way, and try" \
      "again"
  fi
  refuse_broken "${held}"
  on="$(sed -n 's#^branch refs/heads/##p' <<<"${held}")"
  if [[ -n "${on}" ]]; then
    refuse "${dir} is the worktree of '${on}', not of '${branch}'; it may hold that branch's work, so" \
      "leave it where it is and report it"
  fi
  refuse_detached "${dir}"
fi

# main's full ref, never its short name: a tag named main or origin/main makes the short name ambiguous.
base=refs/heads/main
fetched=''
if git remote get-url origin >/dev/null 2>&1 && git fetch --quiet --prune origin; then
  base=refs/remotes/origin/main
  fetched=1
fi

# A Renovate branch without a worktree is resumed only from origin's branch, as fetched just now: a local
# one alone, or a remote-tracking ref left by an earlier fetch, may be a branch Renovate has since rebased
# or deleted. A local branch is resumed only when it is that branch itself, at its commit and tracking it;
# any other is refused and left as it is, since only the maintainer removes local branches (CLAUDE.md
# rule 8).
if [[ -n "${renovate}" ]]; then
  if [[ -z "${fetched}" ]]; then
    refuse "'${branch}' is resumed only from origin's branch, but origin could not be fetched (no remote" \
      "origin, or it is unreachable), so whether origin/${branch} exists is unknown; make worktree never" \
      "creates a Renovate branch. Fix the remote and try again"
  fi
  if ! git show-ref --verify --quiet "refs/remotes/origin/${branch}"; then
    refuse "'${branch}' has no such remote branch origin/${branch}; make worktree resumes an existing" \
      "Renovate branch and never creates one"
  fi
  if git show-ref --verify --quiet "refs/heads/${branch}"; then
    at="$(git rev-parse "refs/heads/${branch}")"
    origin_at="$(git rev-parse "refs/remotes/origin/${branch}")"
    # The upstream as a full ref, read by the branch's full ref: a short name on either side is
    # ambiguous beside a same-named tag. git rejects refs/heads/<branch>@{upstream}, hence for-each-ref.
    # %(upstream:short) widens to remotes/origin/<branch> beside a tag origin/<branch>, hence the full
    # ref. Empty when the branch tracks nothing.
    upstream="$(git for-each-ref --format='%(upstream)' "refs/heads/${branch}")"
    if [[ "${at}" != "${origin_at}" || "${upstream}" != "refs/remotes/origin/${branch}" ]]; then
      refuse "the local branch '${branch}' differs from origin/${branch}: it is at another commit or does" \
        "not track it, so it is no base to work on; leave it where it is and report it (only the" \
        "maintainer removes local branches)"
    fi
  fi
fi

if git show-ref --verify --quiet "refs/heads/${branch}"; then
  # Resume a local branch. The short name, since refs/heads/<branch> would detach HEAD; it is unambiguous:
  # git worktree add checks out the local branch of that name even beside a same-named tag.
  git worktree add --quiet "${dir}" "${branch}"
elif git show-ref --verify --quiet "refs/remotes/origin/${branch}"; then
  # Resume a branch that was pushed from elsewhere.
  git worktree add --quiet --track -b "${branch}" "${dir}" "refs/remotes/origin/${branch}"
else
  # New work, cut from the latest main (never a Renovate branch, refused above without its remote one).
  # --no-track: the branch must never push to main.
  git worktree add --quiet --no-track -b "${branch}" "${dir}" "${base}"
fi
echo "${dir}"
