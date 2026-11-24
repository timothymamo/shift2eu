# 0003. Run the tester role with pi on DeepSeek V4.1 Flash

- Status: Accepted; [ADR 0044](0044-refuse-printing-the-environment-in-pi-with-a-project-extension.md)
  adds a project extension that refuses the commands printing the environment (issue #540)
- Date: 2026-11-09

## Context

The tester writes the failing test that defines each behaviour before the coder implements it. When
one model writes both the tests and the code, it tends to make the same assumptions in both, and the
tests confirm the code instead of challenging it. We have access to DeepSeek models, and
dot-agent-deck supports [pi](https://pi.dev), a minimal agent harness with a built-in DeepSeek provider,
as a first-class agent (status, delivery, `work-done`).

## Decision

The tester role runs `pi --provider deepseek --model deepseek-flash --thinking high --approve`. Every
other role stays on Claude Code.

- Pi reads `CLAUDE.md` as context, like Claude Code.
- Skills stay in `.claude/skills` as the single source. `.pi/settings.json` adds that directory to pi's
  skill paths; pi loads project settings only when the project is trusted, hence `--approve`.
- Node.js and pi are installed in the Lima VM at pinned versions (`dev/lima.yaml`) that Renovate updates.
- The DeepSeek API key is `DEEPSEEK_API_KEY` from the host shell, which `make vm-env` copies into the VM
  (`docs/dev-environment.md`).

## Alternatives considered

- **Claude for every role.** Simplest, but tests and code share one model's blind spots.
- **DeepSeek through another host** (OpenRouter, Azure AI Foundry; pi supports both). Useful later for
  data-residency or billing reasons; the direct API is what we have access to now.
- **pi's own installer** (`pi.dev/install.sh`) instead of npm. It pins pi's transitive dependencies, but
  it cannot pin pi's own version and needs a terminal to install Node.js, so it does not fit unattended
  VM provisioning. npm with an exact version is reproducible at the top level and Renovate-trackable.

## Consequences

- Tests are written by a different model family from the code they check.
- Source code, test output and anything else the tester reads goes to DeepSeek's API, operated
  outside the EU. That is acceptable for code meant to be published; it is why real inventories and
  reports must stay outside the repository directory (see `docs/dev-environment.md`).
- Pi has no permission prompts. The tester's limits (test files only) are instructions, not
  enforcement; the Lima VM is the security boundary, and the reviewer checks that the tester's commits
  touched only tests.
- Skills must stay agent-neutral: they are read by two different harnesses.
