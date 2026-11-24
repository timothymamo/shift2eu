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
  extension file), with the list's reason, and a read, grep, find or ls call on `/proc/*/environ`. It fails
  closed: when the list cannot be read or parsed, or a pattern does not compile, it refuses every bash
  command and says why. The patterns refuse bare `env` and `printenv` (also by path, after a separator,
  in a pipeline or a subshell), bare `set` and `export`, `export -p`, `declare` and `typeset` bare or with
  `-p`/`-x`, `compgen -v`/`-e`/`-A variable`, and any `/proc/*/environ`. They leave `env VAR=value cmd`
  alone: an `env` whose arguments hold an assignment runs a command instead of printing. The patterns are
  written in the syntax Go's RE2 and JavaScript share (no lookaround, no backreference), so the Go test
  that holds them to the refused and allowed tables (`test/architecture/pienvdeny_test.go`) tests what pi
  runs.
- **Claude Code:** `.claude/settings.json`'s `permissions.deny` refuses `env` and `env *`, `/usr/bin/env*`,
  `printenv` and `printenv *`, bare `set` and `export`, `export -p*`, `declare` and `typeset` bare and with
  `-p*`/`-x*`, `compgen -v*`/`-e*`, `*/proc/*/environ*`, and reading `/proc/*/environ`
  (`Read(//proc/**/environ)`; `//` is Claude Code's absolute path). A prefix rule cannot tell
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
- It is a second layer, not a boundary. Quoting (`bash -c 'env'`), an interpreter (`python3 -c ...`) or a
  tool that is not bash still reaches the environment; the Lima VM stays the boundary (ADR 0003).
- A pattern Go accepts but JavaScript does not would refuse nothing in pi and pass every Go test; a
  reviewer holds the patterns to the common syntax.
- A command that only mentions such a word after a separator (`git commit -m "x; env"`) is refused too:
  write it differently.
- pi now executes project code (`.pi/extensions/`) under `--approve`. A change to `.pi/` is reviewed like
  any other code, and the auditor reads it.
