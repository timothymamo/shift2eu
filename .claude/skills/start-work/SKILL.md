---
name: start-work
description: Start or resume work on a PRD, PRD milestone or GitHub issue in shift2eu in its own git worktree and branch, never on main. Use before the first change for any PRD, milestone or issue, and to reopen the worktree of an existing branch.
---

# Start work in a worktree

Nothing is ever changed on `main` or in the repository root, which stays on `main`. Every pull request
has its own branch, and every branch its own worktree under `.worktrees/`
(`docs/adr/0004-one-worktree-per-pull-request.md`).

## Steps

1. Name the branch `<type>/<issue>-<slug>`:
   - `<type>`: the Conventional Commit type of the main change (`feat`, `fix`, `test`, `docs`,
     `refactor`, `perf`, `build`, `ci`, `chore`), or `prd` for writing a new PRD.
   - `<issue>`: the GitHub issue number; for a PRD milestone, the PRD's issue.
   - `<slug>`: a few kebab-case words, for example `feat/12-aws-ec2-discovery`.

   One branch per PR, so a PRD gets one branch per milestone.
2. Run `make worktree BRANCH=<branch>` from the checkout you started in: the repository root, or the
   dispatch worktree of a dispatched unit. It fetches `origin`, creates `.worktrees/<type>-<issue>-<slug>`
   in that checkout on a new branch cut from the latest `origin/main`, and prints the worktree's absolute
   path. For a branch that already has a PR worktree (one directly under a `.worktrees/`) it prints that
   worktree's path, wherever it is, and a branch without one it reopens, so this is also how to resume.
   It refuses a branch that is checked out anywhere else, such as the main checkout, which stays on
   `main`, or a dispatch worktree. It also refuses when a worktree on a detached HEAD sits at
   `.worktrees/<type>-<issue>-<slug>` in any checkout, which may be the branch in the middle of a rebase.
   When it refuses, do what its message says; most say to leave the worktree where it is and report it.
3. Work only in that worktree: run every non-git command from it (`cd <path> && ...`), and read and edit
   files only under it. Run git as `git -C <path> ...`, never `cd <path> && git ...`. Write the
   worktree path out in full; do not store it in a shell variable. `make check`, `go test ./...` and
   `git` then act on this branch alone. Signal only what you started (CLAUDE.md rule 10): stop a run
   of your own only by the pid and start time you noted; never a process found by name, never a group.
4. When delegating, make `Worktree: <absolute path>` the first line of every task.

## Finish

The PR's merge step (skill `pr-create`) removes the worktree with
`git -C <repository root> worktree remove <path>`, which refuses while there are uncommitted changes.
`dot-agent-deck worktree list` shows leftover worktrees.

A PR worktree inside a dispatch worktree is removed before that unit is closed. Closing the unit
(`dot-agent-deck close`, or closing its card or tab in the deck) runs `git worktree remove` on the
dispatch worktree, which deletes everything in it, nested worktrees included, even dirty or locked ones;
the branches and their commits survive
(`docs/adr/0010-create-pull-request-worktrees-inside-the-dispatch-worktree.md`). If `make worktree` then
reports that the branch's worktree no longer exists, do what it says, which clears that one entry:
`git worktree remove <path>`, after `git worktree unlock <path>` when it says the entry is locked. Then
run it again. Never run `git worktree prune`: it clears every stale entry in the repository, including
those of healthy worktrees on a disk that is not mounted.

## Rules

- Never commit or push to `main`, and never pass `--no-verify`: the hooks in `.githooks/` that refuse
  it are the safety net, not an obstacle.
- A pull request's worktree lives under `.worktrees/` in the main checkout, or in the dispatch worktree
  of a dispatched unit. Never create one elsewhere (for example `../`), and never `git switch` the
  repository root to another branch.
- The directories beside the repository, `../shift2eu-dispatch-*` on `agent/dispatch-*` branches,
  belong to dot-agent-deck's dispatcher, which creates and removes them
  (`docs/adr/0008-mount-a-workspace-directory-for-dispatcher-mode.md`). An orchestration started in one
  still runs `make worktree` for each pull request, from the dispatch worktree, which puts the PR's
  worktree inside it where the unit's agents can reach it without a permission prompt. It never commits
  on the `agent/dispatch-*` branch.
