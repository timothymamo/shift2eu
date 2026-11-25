# 0004. Build every pull request in its own git worktree under .worktrees/

- Status: Accepted; dispatcher mode, rejected below, is adopted alongside it in
  [0008](0008-mount-a-workspace-directory-for-dispatcher-mode.md), and a dispatched unit's `.worktrees/`
  is the one in its dispatch worktree
  ([0010](0010-create-pull-request-worktrees-inside-the-dispatch-worktree.md))
- Date: 2026-11-09

## Context

Several agents work on the repository at once, and the user keeps a checkout of it too. If the work
happened in that one checkout, an agent switching branches would change the files under everyone else,
and a commit could land on `main` by accident. dot-agent-deck recommends one git worktree per
orchestration, and its dispatcher mode creates them next to the project (`../<repo>-dispatch-*`). The
Lima VM (ADR 0002) mounts only this repository, so a directory next to it is outside the VM's reach.

## Decision

Every PRD milestone or issue is built on its own branch, `<type>/<issue>-<slug>`, checked out in its own
worktree at `.worktrees/<type>-<issue>-<slug>`: inside the repository, so the VM can see it, and
git-ignored. `make worktree BRANCH=...` (`dev/worktree.sh`) creates it from the latest `origin/main`, or
reopens it for an existing branch. A Renovate PR's branch, `renovate/<name>`, is the one other name it
accepts, and only to resume `origin/renovate/<name>` in `.worktrees/renovate-<name>`, never to create it;
a local `renovate/<name>` is resumed only at origin's commit and tracking it (issue #573).

- The repository root stays on `main`. Agents read from it but never change files or commit there.
- The orchestrator creates the worktree and names it on the first line of every task; workers run every
  command in it (skill `start-work`).
- The releaser removes the worktree just before merging the PR.
- Git hooks in `.githooks/`, enabled by `make git-hooks` (which `make vm-up` runs), refuse commits on
  `main` and pushes to it.

## Alternatives considered

- **Branches in the main checkout.** One working tree for every agent and the user: a branch switch or a
  stray commit affects everyone.
- **dot-agent-deck dispatcher mode.** One orchestration per worktree with clean-up built in, but the
  worktrees are created beside the repository, outside the VM's only mount. Mounting the parent directory
  instead would expose everything else in it.
- **Claude Code's own worktrees (`claude --worktree`).** One per agent session rather than per pull
  request, and the tester runs pi, not Claude Code.

## Consequences

- A pull request in progress cannot disturb another one or the user's checkout, and a PR waiting for
  review keeps its worktree while the next one starts.
- Agents still start in the repository root, so every task must name its worktree and every worker must
  stay in it. The reviewer can tell from `git log` whether that held.
- `go ... ./...`, golangci-lint and ripgrep skip `.worktrees/` when run from the root, so root-level
  commands do not see worktree copies.
- The hooks are local and `--no-verify` skips them; agents are told never to use it. Only a branch
  ruleset on GitHub enforces the rule on the server.
- The repository's very first commit and push to `main` happen before the hooks are enabled, or with
  `--no-verify`.
