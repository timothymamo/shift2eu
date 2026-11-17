#!/bin/sh
# The Protect main gate check (PRD #161 milestone 2,
# docs/adr/0031-agents-open-pull-requests-as-a-github-app-with-tokens-minted-on-the-mac.md): checks that no
# change lands on main without the maintainer's approval. It reads the repository's `Protect main` ruleset
# and .github/CODEOWNERS on main, and prints one line per check, `ok: ...` or `gap: ...`:
#
#   - Protect main is enforced (enforcement active) and holds a pull_request rule, whose
#     required_approving_review_count is at least 1 and whose require_code_owner_review,
#     dismiss_stale_reviews_on_push and require_last_push_approval are true (without the rule those four
#     cannot be read, and only the missing rule is reported);
#   - its bypass_actors list is empty. GitHub returns the list only to a token that may change the ruleset
#     (Administration write), which the agents App never gets: a ruleset without the field is a gap saying
#     the list could not be read, which the maintainer settles by running the check with their own token;
#   - CODEOWNERS on main has rules, a * rule, an owner for every rule, and no owner but the maintainer.
#
# Usage: dev/protect-main-check.sh [--transition]   (make gate-check, from the repository root)
#
# Exit status:
#   0  every check holds, or --transition is given and the gaps were only reported
#   1  at least one gap, without --transition
#   2  it could not check: gh failed on a read, there is not exactly one ruleset named Protect main, an
#      answer is not what GitHub documents, or an unknown argument (in both modes)
#
# The transition flag, --transition, is for the time before the switch-over: it reports the gaps, adds the
# line "transition: gaps=<n>, not failing the release" and exits 0, so a release goes on. The Makefile passes
# it while GATE_TRANSITION is 1, its default until PRD #161 milestone 3 turns it off.
#
# It reads GitHub through gh only, with three GETs (gh fills {owner} and {repo} from the repository's remote),
# parses the answers with jq, never with gh --jq, writes nothing anywhere and reads no environment variable:
# gh's own credentials are gh's business.
set -eu

maintainer='@timothymamo'

transition=0
for arg in "$@"; do
  case "${arg}" in
  --transition) transition=1 ;;
  *)
    printf 'protect-main-check: unknown argument %s; usage: dev/protect-main-check.sh [--transition]\n' "${arg}" >&2
    exit 2
    ;;
  esac
done

cannot() {
  printf 'protect-main-check: cannot check: %s\n' "$*" >&2
  exit 2
}

# read_api <endpoint>: prints the API's answer for endpoint, or stops with status 2.
read_api() {
  gh api "$1" || cannot "gh api $1 failed"
}

# Printed values come from the API: control characters are replaced, so an answer cannot rewrite the
# terminal or forge a line of the report.
jq_lib='def clean: tostring | gsub("[\u0000-\u001f\u007f]"; "?");'

list_endpoint='repos/{owner}/{repo}/rulesets'
rulesets="$(read_api "${list_endpoint}")" || exit 2
ids="$(printf '%s\n' "${rulesets}" | jq -r '
  if type != "array" then error("not a list") else . end
  | .[] | select(type == "object" and .name == "Protect main") | .id
')" || cannot "the answer of ${list_endpoint} is not a list of rulesets"
case "${ids}" in
'') cannot "no ruleset is named Protect main in ${list_endpoint}" ;;
*[!0-9]*) cannot "not exactly one ruleset is named Protect main in ${list_endpoint}, or its id is not a number: ${ids}" ;;
esac

ruleset_endpoint="${list_endpoint}/${ids}"
ruleset="$(read_api "${ruleset_endpoint}")" || exit 2

codeowners_endpoint='repos/{owner}/{repo}/contents/.github/CODEOWNERS?ref=main'
codeowners="$(read_api "${codeowners_endpoint}")" || exit 2

ruleset_report="$(printf '%s\n' "${ruleset}" | jq -r "${jq_lib}"'
  if type != "object" or (.rules | type) != "array" then error("not a ruleset") else . end
  | (if .enforcement == "active" then "ok: Protect main is enforced"
     else "gap: Protect main is not enforced: enforcement is \(.enforcement | clean)" end),
    (([.rules[] | select(type == "object" and .type == "pull_request")] | first) as $rule
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
    (if (has("bypass_actors") | not) or .bypass_actors == null then
       "gap: bypass_actors could not be read: the token cannot read the ruleset'"'"'s bypass list, so run make gate-check as the maintainer"
     elif (.bypass_actors | type) != "array" then error("bypass_actors is not a list")
     elif (.bypass_actors | length) == 0 then "ok: bypass_actors is empty"
     else "gap: bypass_actors is not empty: "
       + ([.bypass_actors[] | "\(.actor_type | clean) \(.actor_id | clean) (\(.bypass_mode | clean))"] | join(", "))
     end)
')" || cannot "the answer of ${ruleset_endpoint} is not a ruleset"

codeowners_report="$(printf '%s\n' "${codeowners}" | jq -r --arg maintainer "${maintainer}" "${jq_lib}"'
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

report="$(printf '%s\n%s\n' "${ruleset_report}" "${codeowners_report}")"
printf '%s\n' "${report}"

gaps="$(printf '%s\n' "${report}" | grep -c '^gap: ' || true)"
if [ "${gaps}" -eq 0 ]; then
  exit 0
fi
if [ "${transition}" -eq 1 ]; then
  printf 'transition: gaps=%s, not failing the release\n' "${gaps}"
  exit 0
fi
exit 1
