# 0044. Refuse printing the environment in pi with a project extension, and in Claude Code with deny rules

- Status: Accepted
- Date: 2026-11-24

## Context

The agents' API keys and tokens are in their environment (CLAUDE.md rule 3). On 2026-11-24 the tester
(pi on DeepSeek, [ADR 0003](0003-pi-with-deepseek-for-the-tester-role.md)) ran `env | grep` while looking
for its scratch directory and printed `DEEPSEEK_API_KEY` in its pane; the key had to be rotated
(issue #540). Only words held the rule:

- pi runs with `--approve` and has no permission prompts; `.pi/settings.json` only loaded the skills;
- `.claude/settings.json`'s `permissions.deny` had no rule for `env`, `printenv` or `set`;
- nothing told the tester where its scratch directory is, so it went looking.

pi 1.5.2 (the version `dev/lima.yaml` pins) has no setting that refuses a command, but it auto-discovers
project-local extensions in `.pi/extensions/`, and an extension's `tool_call` handler sees every tool call
before it runs and can return `{ block: true, reason }`; a handler that throws blocks the call too. pi loads
project extensions only once the project is trusted, and `--approve` trusts it for the process, so the
tester's command line already loads them.

## Decision

Hold rule 3 with a second layer in both agents, beside the words in CLAUDE.md:

- **pi:** `.pi/extensions/deny-env-printing.ts` hooks `tool_call`. It refuses a bash command that matches
  any pattern of `.pi/deny-env-printing.json` (`{"reason": ..., "patterns": [...]}`, read relative to the
  extension file), with the list's reason, and a read, grep, find, ls, edit or write call whose path
  resolves to `/proc` or below it (after `~` and `..`, against the working directory) or whose path, glob
  or pattern names `daemon.env`. It tests `event.toolName` itself and imports pi's package for types
  alone (`import type`), so the guard does not depend at run time on the module it guards.
  - **What fails closed, and what does not.** pi reports an extension that fails to load and carries on
    without it: a load failure fails open. So the extension's top level does nothing that can throw (the
    list's path is computed inside the loader's `try`), and the factory never throws. After load it fails
    closed: when the list cannot be found, read or parsed, or a pattern does not compile, it refuses every
    bash command and says why, and a handler that throws blocks the call it was checking (pi turns the
    throw into a blocked call; the handler also catches it and refuses). A `session_start` handler shows
    "env-printing refusal loaded" (or that the list failed) when the pane has a UI (`ctx.hasUI`), and
    nothing otherwise.
  - **What the patterns refuse.** Every pattern for a command word shares one prefix: a separator
    (start, newline, `;`, `&`, `|`, `(`, a backtick, `{`, a quote), then any number of assignments
    (`FOO=1 env`), shell keywords (`then`, `do`, `else`, `elif`, `if`, `while`, `until`, `!`) and wrappers
    that run their argument (`sudo`, `command`, `builtin`, `exec`, `nohup`, `time`, `xargs`, `timeout`,
    `nice`, `ionice`, `stdbuf`, `doas`, `busybox`, `eval`, `env`, `bash`/`sh`/`zsh`/`dash`/`ksh`, each with
    options, numeric or duration words and assignments: `timeout 5 env`, `bash -c 'env'`), then an
    optional quote. After that prefix they refuse: `env` followed only by options and assignments, then the
    end or a separator (`env`, `env -0`, `env FOO=bar | grep`); `printenv`; bare `set`; bare `export` and
    `export -p`; `declare`/`typeset` bare or with `-p`/`-x`; `compgen -v`/`-e`/`-A`; `ps` whose first
    argument is a BSD option word with `e` (`ps e`, `ps eww`, `ps auxeww`); `make` with an option holding
    `p` or `--print-data-base` (`make -p`, `make -pn`). Anywhere in a command they refuse a `/proc` path
    (`ls /proc/1`, `cat /proc/self/env*`), `daemon.env`, the expansion of a key by name (`$..._KEY`,
    `${..._TOKEN}`, `$..._SECRET`, `$..._PASSWORD`, and `${!` indirection), and the interpreter one-liners
    that read the environment themselves (`ENVIRON`, `os.environ`, `os.getenv`, `process.env`, `%ENV`,
    `$ENV{`, `getenv(`).
  - **What they leave alone.** `env` followed by a command word runs that command and prints nothing:
    `env GOTOOLCHAIN=local make check`, `env -u FOO make test`, `env -C dir make`,
    `/usr/bin/env bash dev/x.sh` are allowed in pi, unless the command word is itself a printer
    (`env A=1 printenv`, `env X=1 env`), which the shared prefix catches. So are `set -e`, `export FOO=bar`,
    `go env GOPATH`, `ps -o lstart=,args= -p <pid>` (rule 10), `ps -ef`, `ps aux`, `make -C dir check` and
    `make -j4 check`.
  - The patterns are written in the syntax Go's RE2 and JavaScript share (no lookaround, no backreference,
    no inline flag), so the Go test that holds them to the refused and allowed tables
    (`test/architecture/pienvdeny_test.go`) tests what pi runs.
- **Claude Code:** `.claude/settings.json`'s `permissions.deny` refuses `env` and `env *`, `/usr/bin/env*`,
  `printenv` and `printenv *`, bare `set` and `export`, `export -p*`, `declare` and `typeset` bare and with
  `-p*`/`-x*`, `compgen -v*`/`-e*`/`-A*`/`*-A*`, `*/proc/*/environ*`, `make -p*` and
  `make --print-data-base*`, `ps e*`, `*daemon.env*`, and a command holding a `$` before `API_KEY` or
  `TOKEN` (`*$*API_KEY*`, `*$*TOKEN*`); and reading `/proc` whole (`Read(//proc)`, `Read(//proc/**)`,
  `Read(//proc/**/environ)`) and the deck's directory (`Read(~/.config/dot-agent-deck/**)`). `//` is
  Claude Code's absolute path: a single leading `/` is relative to the project, so the earlier
  `Read(/proc/*/environ)` matched nothing and is gone. No rule is written for `make` with `-p` after
  another option (`make -C dir -p`) or for BSD `ps` forms other than `ps e*` (`ps auxe`): a prefix rule
  wide enough for them (`make *-p*`, `ps *e*`) would also refuse a target or argument that holds `-p`
  and rule 10's `ps -o lstart=,args= -p <pid>`; pi's patterns hold those forms. A prefix rule cannot tell
  `env VAR=value cmd` from `env | grep`, so the Claude Code roles refuse `env` with arguments altogether;
  no skill, make recipe the agents type or dev script an agent runs uses `env VAR=value cmd` (recipes and
  scripts that use `env -i` or `env -u` run inside make or a script, where no permission rule applies).
  The project settings load for every role, the workers too: `--settings .claude/worker-settings.json`
  adds a layer, it does not replace the project's. `test/architecture/claudesettings_test.go` pins the rules.
- **The tester's prompt** names its scratch directory, `.scratch/` in its worktree (git-ignored), and
  repeats rule 3 in one line (`test/docs/testerprompt_test.go`). An ignored `.scratch/` does not stop
  `git worktree remove`, which refuses only untracked or modified files; it is removed with the worktree.

This is a new ADR rather than an amendment of ADR 0003: ADR 0003 decides which agent and model run the
tester and stays as it is; this one adds a mechanism, a project extension pi executes, and the matching
Claude Code rules, which ADR 0003 does not cover.

## Alternatives considered

- **Words only** (CLAUDE.md and the prompt). What failed on 2026-11-24.
- **A global extension in the VM** (`~/.pi/agent/extensions/`, provisioned by `dev/lima.yaml`). It would
  load without project trust, but it lives outside git, is not reviewed with the change that needs it and
  is not tested by `make check`; the project extension is, and `--approve` loads it already.
- **Keep the keys out of the tester's environment** (a key helper or a wrapper that passes the key only to
  pi's provider). Out of scope (issue #540: how the deck passes keys); it would remove the value rather than
  the command, and stays worth doing.
- **Parse the command line in the extension.** A shell parser would tell more forms apart, but it is code
  the Go tests could not run; pattern data both sides read is testable here.

## Consequences

- A tester or Claude Code role that runs `env`, `printenv` or bare `set` is refused before the command
  runs, and told why and where its scratch directory is.
- It is a second layer, not a boundary. Patterns cannot parse a shell: a script file the agent writes and
  runs, a variable that holds the command word, an interpreter reached in a form the list does not name,
  or a tool that is not bash still reaches the environment; the Lima VM stays the boundary (ADR 0003).
- A load failure of the extension (a syntax error, a module that cannot be resolved) fails open: pi logs
  it and runs the tester without the refusal. Only the paths after load fail closed. The `session_start`
  notice shows a TUI pane whether the refusal loaded.
- A pattern Go accepts but JavaScript does not would refuse nothing in pi and pass every Go test; a
  reviewer holds the patterns to the common syntax.
- Known false positives in pi, each refused although it prints nothing: `declare -x FOO=bar` (it sets and
  exports); a quoted argument that starts with a printer word, or a separator and a printer word inside a
  quoted argument (`git commit -m "env"`, a multi-line `--body` with a line that is only `set`); and a
  command that only names `daemon.env`, `process.env`, `os.environ`, `ENVIRON` or a `/proc/...` path, such
  as a `grep` for them in the tree. Write such a command differently: a message through a file
  (`git commit -F`, `--body-file`), a search through the file tools, which refuse only `/proc` paths and
  `daemon.env`. A quote is a separator to the patterns (it is what refuses `bash -c 'env'`), so
  `git commit -m "x; env"` and `git commit -m "env"` are refused too; `git commit -m "fix env bug"` is not.
- Rollout: the extension and the tester's `.scratch/` line reach only tester panes started from a dispatch
  worktree created after the merge; a pane started before it runs without them until it is restarted, and
  a branch cut before the merge does not ignore `.scratch/` (its `.gitignore` predates the line).
- pi now executes project code (`.pi/extensions/`) under `--approve`. A change to `.pi/` is reviewed like
  any other code, and the auditor reads it.
