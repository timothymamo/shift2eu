---
name: review-change
description: Review a change in shift2eu for correctness, architecture, test quality and readability, reporting findings without editing code. Use when asked to review a diff, branch, commit range or pull request.
---

# Review a change

## Get the change

Work in the worktree named in the task: run every non-git command from it (`cd <path> && ...`). Run git
as `git -C <path> ...`, never `cd <path> && git ...`. Write the worktree path out in full; do not store it
in a shell variable.

- Branch: `git -C <path> fetch origin`, then `git -C <path> diff origin/main...HEAD` and
  `git -C <path> log --oneline origin/main..HEAD`. The local `main` in the repository root can be out of
  date.
- PR: `gh pr diff <n>` and `gh pr view <n>`
- Read the PRD or issue it claims to implement.

## Check, in this order

1. **Requirements.** Does it do what the PRD's acceptance criteria say, all of them, and nothing unasked?
2. **Correctness.** Edge cases: empty inventories, unknown resource types, regions outside the EU,
   pagination, partial failures from a provider API, nil maps and slices, units (GiB vs GB, vCPU vs core).
3. **Domain fidelity.** Is a mapping verdict justified? A "direct" equivalence must really be
   feature-compatible; when in doubt it is "partial" with the gap stated.
4. **Architecture.** The dependency rule holds (`make lint` catches imports, you catch intent): no business
   rules in adapters or CLI, no SDK types crossing the adapter boundary, ports declared by their consumer.
   Signal only what you started (CLAUDE.md rule 10): to stop a run of your own, signal the pid you kept,
   never a process found by name.
5. **Tests.** Behaviour is pinned at the lowest sensible level; tests assert outcomes, not internals;
   fixtures are sanitised; no network or credentials in default tests. The TDD split held: `test(...)`
   commits touch only `*_test.go`, `testdata/` and `internal/testutil`, and later commits did not weaken
   a test to make it pass (`git -C <path> log -p origin/main..HEAD -- '*_test.go'`).
6. **Readability.** Names use the ubiquitous language; errors carry context; exported identifiers documented.

## Report

For each finding: **severity** (blocker / suggestion / nit), `path/file.go:line`, what is wrong, and a
concrete fix. Say explicitly when you found nothing in a category. End with a verdict:
*approve*, *approve with suggestions*, or *changes required*. The verdict is never a GitHub approval
(`gh pr review --approve`, never): approving a pull request is the maintainer's alone.
