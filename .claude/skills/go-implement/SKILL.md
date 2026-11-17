---
name: go-implement
description: Implementation conventions for shift2eu Go code - layering, errors, context, logging, dependencies. Use when writing or changing production Go code, especially when making a failing test pass.
---

# Implementing Go code

## Before you write code

1. Work in the worktree named in the task (`cd <path> && ...` for every non-git command), never in the
   repository root and never on `main`. Run git as `git -C <path> ...`, never `cd <path> && git ...`.
   Write the worktree path out in full; do not store it in a shell variable.
2. Read the task's PRD or issue and the failing test you were given.
3. Locate the layer (CLAUDE.md, "Architecture") and read the package's `doc.go`.
4. Make the smallest change that makes the test pass, then refactor with the tests green.
5. Change every file, the `Makefile` and scratch files included, with your file-editing tool, never
   with a shell command or script that rewrites it (heredoc, `cat >`, `sed -i`, `python3 -`). Never
   `rm` a path held in a variable or built at run time; leave scratch directories for the system to
   clean (CLAUDE.md, rule 9).

## Layering (enforced by `make lint` via depguard)

- `internal/domain/...`: other domain packages and the pure standard packages on the allow list of the
  `domain-is-pure` rule in `.golangci.yml`. A new standard package needs its own entry there, never one
  that does I/O. A new domain package needs one too, as do the rules that list the domain packages for
  the application layer and the CLI. Pure types and functions. No I/O, no `context`, no clock (pass `time.Time` in), no
  logging. `forbidigo` (group `domain-is-pure`) refuses the I/O and clock calls of `fmt` and `time` there,
  such as `fmt.Println`, `time.Now`, `time.Local` and `time.Parse`; use `time.UTC` and
  `time.ParseInLocation` with an explicit location instead.
- `internal/app/...`: use cases. Declares the port interfaces it consumes, next to the consumer. Imports
  domain only. Never imports adapters or cloud SDKs.
- `internal/adapter/...`: implements ports. Cloud SDKs live only here, in the provider's own package.
  Adapters translate external models into domain types at the boundary; SDK types never leak out.
  Adapters never import `internal/cli` or `cmd/`.
- `internal/cli`: parses input, calls use cases, prints output. No business rules. Only `cmd/shift2eu`
  imports it.
- `cmd/shift2eu`: composition root. The only place that constructs adapters and injects them.

If a change seems to need a forbidden import, the design is wrong: add or change a port instead, and
mention it in your work-done summary.

## Conventions

- Errors: wrap with context, `fmt.Errorf("discover ec2 instances in %s: %w", region, err)`. Sentinel errors
  as `var ErrX = errors.New("...")` in the package that owns the concept; compare with `errors.Is/As`.
  Never `panic` for expected failures.
- `context.Context` is the first parameter of anything that does I/O; respect cancellation.
- Logging: `log/slog`, injected, adapters and CLI only.
- Constructors return concrete types (`func New(...) *Client`); accept interfaces.
- Keep exported API minimal; every exported identifier has a doc comment.
- New third-party dependency: only with a reason in the commit message; prefer the standard library.
- No global mutable state; no `init()` beyond registering embedded data.

## Done means

`make check` passes, the change is committed with a Conventional Commit message
(`feat(mapping): ...`, `fix(aws): ...`), and `git -C <path> status` is clean.
