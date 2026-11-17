#!/bin/sh
# The Protect main gate check (PRD #161 milestone 2,
# docs/adr/0031-agents-open-pull-requests-as-a-github-app-with-tokens-minted-on-the-mac.md): checks that no
# change lands on main without the maintainer's approval. It reads the repository timothymamo/shift2eu, its
# `Protect main` ruleset, .github/CODEOWNERS on main and GitHub's CODEOWNERS errors for that file, and prints
# one line per check, of three kinds: `ok: ...`, `gap: ...`, and `unverified: ...` for what this token may
# not read. In order:
#
#   - Protect main is enforced (enforcement active), targets branches, includes main and does not exclude it;
#   - it holds a pull_request rule, whose required_approving_review_count is at least 1 and whose
#     require_code_owner_review, dismiss_stale_reviews_on_push and require_last_push_approval are true
#     (without the rule those four cannot be read, and only the missing rule is reported);
#   - its bypass_actors list is empty. GitHub returns the list only to a token that may change the ruleset,
#     so an empty or missing list proves nothing on a token that is not a repository admin (the repository's
#     permissions.admin): that is `unverified:`, which the maintainer settles by running the check with their
#     own token. A missing list on an admin token is a gap, and a non-empty list is a gap on any token;
#   - this token cannot bypass Protect main (current_user_can_bypass is never);
#   - CODEOWNERS on main has rules, a * rule, an owner for every rule, and no owner but the maintainer, and
#     GitHub reports no errors for it.
#
# Usage: dev/protect-main-check.sh [--transition]   (make gate-check, from the repository root)
#
# Exit status:
#   0  every check holds, or --transition is given before the switch-over (see below)
#   1  at least one gap
#   2  it could not check: gh failed on a read, there is not exactly one ruleset named Protect main with a
#      numeric id, an answer is not what GitHub documents, or an unknown argument (in both modes)
#   3  no gap, but at least one check is unverified: run make gate-check as the maintainer, with their token
#
# The transition flag, --transition, is for the time before the switch-over, and it closes itself: while the
# gate is off (no pull_request rule, or one that requires neither an approval nor code owner review) it
# reports the gaps and the unverified checks, adds the line
# "transition: gaps=<n>, unverified=<m>, not failing the release" when there are any, and exits 0, so a
# release goes on. Once the gate is on it adds "transition: ignored, the gate is on" and exits as without the
# flag. The Makefile passes it while GATE_TRANSITION is 1, its default until PRD #161 milestone 3 removes it.
#
# It reads GitHub through gh only, with five GETs on the repository spelled out (never gh's {owner}/{repo}
# placeholders, which a fork's remote or GH_REPO would fill), the rulesets list with --paginate. It parses the
# answers with jq, never with gh --jq, writes nothing anywhere and reads no environment variable: gh's own
# credentials are gh's business. Every value of an answer it prints has its control characters (C0, DEL and
# C1) replaced, so an answer cannot rewrite the terminal or forge a line of the report.
set -eu

maintainer='@timothymamo'
repository='repos/timothymamo/shift2eu'

transition=0
for arg in "$@"; do
  case "${arg}" in
  --transition) transition=1 ;;
  *)
    printf 'protect-main-check: unknown argument %s; usage: dev/protect-main-check.sh [--transition]\n' \
      "$(printf '%s' "${arg}" | tr -d '\000-\037\177')" >&2
    exit 2
    ;;
  esac
done

cannot() {
  printf 'protect-main-check: cannot check: %s\n' "$*" >&2
  exit 2
}

# read_api [--paginate] <endpoint>: prints the API's answer for endpoint, or stops with status 2.
read_api() {
  if [ "$1" = '--paginate' ]; then
    gh api --paginate "$2" || cannot "gh api --paginate $2 failed"
  else
    gh api "$1" || cannot "gh api $1 failed"
  fi
}

# Every jq run below drops jq's own messages (2>/dev/null): they may quote an answer, which is data.
jq_lib='def clean: tostring | gsub("[\u0000-\u001f\u007f-\u009f]"; "?");'

list_endpoint="${repository}/rulesets"
pages="$(read_api --paginate "${list_endpoint}")" || exit 2
# gh prints one JSON array per page: together they are the list.
ids="$(printf '%s\n' "${pages}" | jq 2>/dev/null -r -s "${jq_lib}"'
  add
  | if type != "array" or any(.[]; type != "object") then error("not a list") else . end
  | [.[] | select(.name == "Protect main")
     | if (.id | type) == "number" then .id | tostring else "?" + (.id | clean) end]
  | join(" ")
')" || cannot "the answer of ${list_endpoint} is not a list of rulesets"
case "${ids}" in
'') cannot "no ruleset is named Protect main in ${list_endpoint}" ;;
*[!0-9]*) cannot "not exactly one ruleset is named Protect main in ${list_endpoint}, or its id is not a number: ${ids}" ;;
esac

ruleset_endpoint="${list_endpoint}/${ids}"
ruleset="$(read_api "${ruleset_endpoint}")" || exit 2

repo="$(read_api "${repository}")" || exit 2
repo_facts="$(printf '%s\n' "${repo}" | jq 2>/dev/null -c '
  if type != "object" then error("not a repository") else . end
  | {admin: (.permissions.admin? == true), default_branch: (.default_branch // "")}
')" || cannot "the answer of ${repository} is not a repository"

codeowners_endpoint="${repository}/contents/.github/CODEOWNERS?ref=main"
codeowners="$(read_api "${codeowners_endpoint}")" || exit 2

errors_endpoint="${repository}/codeowners/errors?ref=main"
errors="$(read_api "${errors_endpoint}")" || exit 2

ruleset_report="$(printf '%s\n' "${ruleset}" | jq 2>/dev/null -r --argjson repo "${repo_facts}" "${jq_lib}"'
  # covers(pattern): whether a ref_name condition pattern matches refs/heads/main. A glob is matched with *
  # inside one path segment and ** across them.
  def covers($pattern):
    if ($pattern | type) != "string" then false
    elif $pattern == "~ALL" then true
    elif $pattern == "~DEFAULT_BRANCH" then $repo.default_branch == "main"
    else "refs/heads/main" | test("^" + ($pattern
      | gsub("(?<c>[.+(){}\\[\\]|^$\\\\])"; "\\\(.c)")
      | split("**") | map(gsub("\\*"; "[^/]*") | gsub("\\?"; "[^/]")) | join(".*")) + "$")
    end;
  if type != "object" or (.rules | type) != "array" then error("not a ruleset") else . end
  | (.conditions.ref_name? // {}) as $ref
  | (if .enforcement == "active" then "ok: Protect main is enforced"
     else "gap: Protect main is not enforced: enforcement is \(.enforcement | clean)" end),
    (if .target == "branch" then "ok: Protect main targets branches"
     else "gap: Protect main targets \(.target // "missing" | clean), not branches" end),
    (if any(($ref.include // [])[]?; covers(.)) then "ok: Protect main includes main"
     else "gap: Protect main does not include main" end),
    # covers reads [, ], {, } and \ as literal characters, but GitHub reads a character class, a brace
    # expansion or an escape there, any of which can select main: an exclude pattern holding one is not
    # matched but reported as a gap, so that a pattern the check cannot read never passes as one that holds.
    ([($ref.exclude // [])[]?] | map(select(type == "string" and test("[\\[\\]{}\\\\]")))) as $unreadable
    | ([($ref.exclude // [])[]? | select(covers(.))] | .[0]) as $excluded
    | (if $excluded == null and ($unreadable | length) == 0 then "ok: Protect main does not exclude main"
       else
         ($unreadable[] | "gap: Protect main cannot read exclude pattern \(clean), so it may exclude main"),
         (if $excluded == null then empty else "gap: Protect main excludes main: \($excluded | clean)" end)
       end),
    (([.rules[] | select(type == "object" and .type == "pull_request")] | .[0]) as $rule
    | if $rule == null then "gap: Protect main has no pull_request rule"
      else
        "ok: Protect main requires a pull request",
        (($rule.parameters // {}) as $p
        | (($p.required_approving_review_count) as $n
           | if ($n | type) == "number" and $n >= 1 then "ok: required_approving_review_count is at least 1"
             else "gap: required_approving_review_count is \($n | clean), want at least 1" end),
          (("require_code_owner_review", "dismiss_stale_reviews_on_push", "require_last_push_approval") as $k
           | if $p[$k] == true then "ok: \($k) is true" else "gap: \($k) is \($p[$k] | clean)" end))
      end),
    (.bypass_actors as $actors
    | if $actors != null and ($actors | type) != "array" then error("bypass_actors is not a list")
      elif $actors != null and ($actors | length) > 0 then "gap: bypass_actors is not empty: "
        + ([$actors[] | "\(.actor_type | clean) \(.actor_id | clean) (\(.bypass_mode | clean))"] | join(", "))
      elif ($repo.admin | not) then
        "unverified: bypass_actors cannot be verified with this token (not a repository admin): run make gate-check as the maintainer"
      elif $actors == null then "gap: bypass_actors is missing although this token is a repository admin"
      else "ok: bypass_actors is empty"
      end),
    (if .current_user_can_bypass == "never" then "ok: this token cannot bypass Protect main"
     else "gap: this token can bypass Protect main: current_user_can_bypass is \(.current_user_can_bypass // "missing" | clean)" end)
')" || cannot "the answer of ${ruleset_endpoint} is not a ruleset"

# The gate is on once Protect main requires an approval or code owner review: from then on --transition is
# ignored, so a Makefile that still passes it cannot hold a gap back.
gate_on="$(printf '%s\n' "${ruleset}" | jq 2>/dev/null -r '
  [.rules[] | select(type == "object" and .type == "pull_request")] | .[0] | (.parameters? // {}) as $p
  | if . != null and ((($p.required_approving_review_count | type) == "number"
                       and $p.required_approving_review_count >= 1)
                      or $p.require_code_owner_review == true)
    then "on" else "off" end
')" || cannot "the answer of ${ruleset_endpoint} is not a ruleset"

codeowners_report="$(printf '%s\n' "${codeowners}" | jq 2>/dev/null -r --arg maintainer "${maintainer}" "${jq_lib}"'
  if type != "object" or .encoding != "base64" or (.content | type) != "string" then error("not a file") else . end
  | [.content | gsub("\\s"; "") | @base64d | split("\n")[]
     | sub("#.*$"; "") | [splits("\\s+")] | map(select(. != ""))
     | select(length > 0) | {pattern: .[0], owners: .[1:]}] as $rules
  | (if ($rules | length) > 0 then "ok: CODEOWNERS on main has at least one rule"
     else "gap: CODEOWNERS on main has no rules" end),
    (if any($rules[]; .pattern == "*") then "ok: CODEOWNERS on main has a * rule"
     else "gap: CODEOWNERS on main has no * rule" end),
    ([$rules[] | select(.owners | length == 0) | "gap: CODEOWNERS rule \(.pattern | clean) has no owner"]
     | if length == 0 then "ok: every CODEOWNERS rule has an owner" else .[] end),
    ([$rules[].owners[] | select(ascii_downcase != ($maintainer | ascii_downcase))
      | "gap: CODEOWNERS owner \(clean) is not \($maintainer)"]
     | if length == 0 then "ok: every CODEOWNERS owner is \($maintainer)" else .[] end)
')" || cannot "the answer of ${codeowners_endpoint} is not a base64 encoded file"

errors_report="$(printf '%s\n' "${errors}" | jq 2>/dev/null -r '
  if type != "object" or (.errors | type) != "array" then error("not an errors report") else . end
  | if (.errors | length) == 0 then "ok: GitHub reports no CODEOWNERS errors on main"
    else "gap: GitHub reports \(.errors | length) CODEOWNERS errors on main" end
')" || cannot "the answer of ${errors_endpoint} is not a CODEOWNERS errors report"

report="$(printf '%s\n%s\n%s\n' "${ruleset_report}" "${codeowners_report}" "${errors_report}")"
printf '%s\n' "${report}"

gaps="$(printf '%s\n' "${report}" | grep -c '^gap: ' || true)"
unverified="$(printf '%s\n' "${report}" | grep -c '^unverified: ' || true)"

if [ "${transition}" -eq 1 ]; then
  if [ "${gate_on}" = 'on' ]; then
    printf 'transition: ignored, the gate is on\n'
  else
    if [ "$((gaps + unverified))" -gt 0 ]; then
      printf 'transition: gaps=%s, unverified=%s, not failing the release\n' "${gaps}" "${unverified}"
    fi
    exit 0
  fi
fi

if [ "${gaps}" -gt 0 ]; then
  exit 1
fi
if [ "${unverified}" -gt 0 ]; then
  exit 3
fi
exit 0
