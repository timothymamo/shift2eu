---
name: pr-create
description: Take committed work on a branch to a verified GitHub pull request - push, open the PR from the template, wait for CI, report results, and stop before merging. Use when work is committed and ready for a pull request, or when explicitly told to merge an approved PR.
---

# Create and verify a pull request

You never modify source code in this flow. You never approve a pull request (`gh pr review --approve`,
never), never enable auto-merge (`gh pr merge --auto`, never), and never merge a pull request the
maintainer has not approved: the maintainer's approval and merge are the single human gate (PRD #161).

## Open

1. Work in the worktree named in the task: run every non-git command from it (`cd <path> && ...`). Run
   git as `git -C <path> ...`, never `cd <path> && git ...`. Write the worktree path out in full; do
   not store it in a shell variable. Confirm the tree is clean (`git -C <path> status`) and the branch
   is not `main` (`git -C <path> branch --show-current`). If either check fails, or the task names no
   worktree, stop and report.
2. Run `make check`. If it fails, report the exact output and stop.
3. If this PR completes a file PRD: set its status to `Complete`,
   `git -C <path> mv prds/<n>-<slug>.md prds/done/`, fix links to the old path, and commit
   `docs(prd): complete PRD #<n>`.
4. `git -C <path> push -u origin HEAD`
5. `gh pr create --base main --title "<conventional commit style title>" --body-file <file>`, using
   `.github/pull_request_template.md` as the structure. Use `Closes #<n>` only when the PR completes the
   PRD or issue, otherwise `Refs #<n>`.

## Verify

6. `gh pr checks <n> --watch --required --fail-fast` until every required check completes. Some checks
   are not required (`Container image`, `Dependency review`), so then wait for every check but
   `AI review`: every 30 seconds run
   `gh pr checks <n> --json name,bucket --jq '[.[] | select(.name != "AI review" and .bucket == "pending")] | length'`
   until it prints `0`. Then `gh pr checks <n>` for every check.
7. On a failed check other than `AI review` (required or not, bucket `fail` or `cancel`):
   `gh run view <run-id> --log-failed`, report the failing job and the exact error, and stop. Do not
   diagnose or fix; the orchestrator re-delegates. A failed `AI review` is no such failure: report it as
   step 8 says.

## Report and stop

8. Report through `dot-agent-deck work-done`: the PR URL, each check and its result, the head commit, and
   anything notable. Always include the `AI review` check (ADR 0033), which runs on every pull request,
   and summarise the reviewer's findings (`gh pr view <n> --json reviews,comments`) as advice for the
   maintainer, never a reason to merge or not. It is not a required check: when it fails or errors,
   report that with its error (`gh run view <run-id> --log-failed`); do not wait on it, re-run it or work
   around it. **Do not merge, do not approve and do not enable auto-merge.** The pull request now waits
   for the maintainer's approval.

## Merge (only when explicitly instructed)

9. Check the maintainer's approval first, from the repository root:
   `gh pr view <n> --json author,headRefOid,reviewDecision,reviews`.
   - If the pull request's `author.login` is the maintainer, `timothymamo` (opened under the maintainer's
     own token before the switch to the agents App), its author cannot approve it: report that the
     maintainer merges it themselves, and stop.
   - Otherwise merge only if **both** hold:
     - the maintainer's (`author.login` exactly `timothymamo`) **latest** review that is not a comment,
       that is the last by `submittedAt` with `state` `APPROVED`, `CHANGES_REQUESTED` or `DISMISSED`,
       has `state` `APPROVED` and its `commit.oid` is the current `headRefOid`. An earlier approval
       followed by a change request, or one since dismissed, does not count. This prints `approved`
       only then:

       ```bash
       gh pr view <n> --json headRefOid,reviews --jq '.headRefOid as $head
         | [.reviews[] | select(.author.login == "timothymamo"
             and (.state == "APPROVED" or .state == "CHANGES_REQUESTED" or .state == "DISMISSED"))]
         | sort_by(.submittedAt) | last
         | if . != null and .state == "APPROVED" and .commit.oid == $head then "approved" else "not approved" end'
       ```

       and
     - `reviewDecision` is `APPROVED` or empty. It can be empty even after an approval while
       `Protect main` does not yet require reviews. Never merge when it is `CHANGES_REQUESTED` or
       `REVIEW_REQUIRED`.

   Otherwise report "waiting for the maintainer's approval" with the head commit and stop. An approval
   by anyone else, an App or a bot included, does not count. Never try `--admin`, `--auto` or any other
   way past a missing approval, and never approve the pull request yourself.
10. From the repository root, remove the worktree: `git -C <repository root> worktree remove <path>`.
    It refuses when the worktree has uncommitted changes; then report and stop, and never force it.
11. Still from the root:
    `gh pr merge <n> --squash --delete-branch --match-head-commit <headRefOid from step 9>`, which
    refuses if the branch moved after the approval you checked; then confirm the linked issue's state
    matches the PR body (`gh issue view <n> --json state`) and report. If the merge fails, report the
    error. To bring the worktree back for a fix, run `make worktree BRANCH=<branch>` from the checkout
    the unit started in, as in step 2 of the `start-work` skill: the repository root, or the dispatch
    worktree of a dispatched unit, so the worktree comes back where the unit's agents can reach it.
