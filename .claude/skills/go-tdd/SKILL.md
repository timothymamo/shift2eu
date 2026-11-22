---
name: go-tdd
description: Test-first workflow and Go testing conventions for shift2eu. Use when writing or changing tests, when asked to produce a failing (RED) test for a behaviour, or when confirming a test passes (GREEN) after implementation.
---

# Go test-driven development

Work in the worktree named in your task (`cd <path> && ...` for every non-git command), never in the
repository root and never on `main`. Run git as `git -C <path> ...`, never `cd <path> && git ...`.
Write the worktree path out in full; do not store it in a shell variable.
Change files with your file-editing tool, never with a shell command or script that rewrites them.
Never `rm` a path held in a variable or built at run time; leave scratch directories for the system to
clean (CLAUDE.md, rule 9).

## RED: pin the behaviour

1. Find where the behaviour belongs (see the layer table in CLAUDE.md) and the closest existing test.
   Prefer **extending an existing table-driven test** over writing a new function.
2. Pick the lowest test level that can observe the behaviour:

   | Level | Where | Use for |
   |---|---|---|
   | Unit | `*_test.go` next to the code, `package foo_test` | Domain rules, use cases with fakes |
   | Contract | `internal/adapter/{source,target}/*test` | Behaviour every provider adapter must share |
   | Golden | `testdata/*.golden` next to the test | Report renderers, CLI output |
   | Integration | `//go:build integration` | Real cloud APIs, read-only credentials, never in default CI |
   | E2E | `test/e2e`, `//go:build e2e` | Build the binary, run it against `testdata/` fixtures |

   `e2e` and `integration` are the only custom build tags. Never negate them (`!e2e`, `!integration`):
   lint always sets both, so a file behind a negated tag is never linted.

3. Write the test, run only it (`go test ./internal/... -run 'TestName/case'`) and confirm it fails
   **for the right reason** (an assertion on the missing behaviour, not a compile error in an unrelated
   file). A compile failure because the new function does not exist yet is acceptable RED.
4. Commit the failing test alone: `test(<pkg>): pin <behaviour>`. Report the exact failure output.

## GREEN: confirm

Re-run the test, then `go test -race ./<pkg>/...`, and report the test names and results.
Never edit production code in this role.
Signal only what you started (CLAUDE.md rule 10): stop a run of your own only by the pid you kept, after checking it is still your command; never a process found by name, never a group.

## Conventions

- Table-driven tests with `t.Run(tc.name, ...)`; call `t.Parallel()` in the test and each subtest unless
  they share state.
- Black-box by default (`package foo_test`). Use an internal test file only for unexported logic that
  cannot be reached otherwise, and say why in a comment.
- Standard library `testing` only; for deep comparisons use `github.com/google/go-cmp/cmp` and report
  `cmp.Diff(want, got)`. No assertion frameworks.
- Fakes, not mocks: hand-written in-memory implementations of the port interface, kept in the test file
  or in `internal/testutil` when shared.
- Golden files: update with `go test ./<pkg> -update` and review the diff before committing.
- Fixtures in `testdata/` are sanitised: no real account IDs, ARNs, subscription IDs, project IDs, IPs
  or names. Use documentation values (`123456789012`, `192.0.2.0/24`, `example.com`).
- Tests never call real clouds, never need credentials and never depend on wall-clock time or the order
  of map iteration.
- Helpers call `t.Helper()`; use `t.TempDir()`, `t.Setenv()` and `t.Context()` instead of manual setup.
