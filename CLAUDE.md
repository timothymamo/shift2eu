# CLAUDE.md

Guidance for every agent working in this repository. Claude Code and pi both load this file.

## What this project is

`shift2eu` is a Go CLI that analyses infrastructure running in a hyperscaler (**AWS, Azure, GCP**) and
reports what can be moved to an EU cloud provider (**OVHcloud, Scaleway, UpCloud**): which resources
have a direct equivalent, which a partial one, which have none, and what blocks the move.

**Status:** walking skeleton. `shift2eu assess` works offline: it reads an inventory file and assesses it
against an embedded OVHcloud, Scaleway or UpCloud catalog snapshot, through every layer, or compares
several targets in one report (`--target all` or a list), as Markdown or as versioned JSON
(`--format json`, ADR 0018) (PRD #2, OVHcloud from PRD #11, UpCloud from PRD #77 and the comparison
from PRD #89, the JSON report and the HTTP API from PRD #103, all in `prds/done/`). It reads inventory
schema versions 1 and 2 (ADR 0006, ADR 0025). From a sized inventory it names a plan and estimates a
monthly list price per resource, per workload and in total from the snapshots' dated prices (on by
default, `--no-estimate` turns it off), groups resources by workload, notes blockers on related
resources, and scopes a target to one region with `--region` (PRD #190, ADRs 0024 and 0026-0030, in
`prds/done/`). A second binary,
`shift2eu-server`, serves the same assessments over a versioned HTTP API (PRD #103, ADR 0019), released
as a binary and as the image `ghcr.io/timothymamo/shift2eu-server` beside the CLI's. Live source adapters
do not exist yet. Work is planned as PRDs in `prds/`.

## Ubiquitous language

Use these words, in code and prose, with exactly these meanings.

| Term | Meaning |
|---|---|
| **Source** | The hyperscaler being analysed (AWS, Azure, GCP). Accessed **read-only**. |
| **Target** | An EU cloud provider being compared (OVHcloud, Scaleway, UpCloud). |
| **Inventory** | Normalised, provider-neutral list of resources discovered in a source. |
| **Catalog** | What a target offers: services, plans, regions, capabilities. |
| **Taxonomy category** | Provider-neutral service type (`compute.vm`, `storage.object`, `database.postgresql`, ...). The only bridge between sources and targets. |
| **Capability** | A feature a resource depends on or an offering provides (e.g. point-in-time restore, IPv6). |
| **Mapping** | Matching an inventory resource to candidate catalog offerings, with a **fit**: direct, partial, none. |
| **Assessment** | The verdict per resource and workload: portability, blockers, required changes, readiness score. |

## Architecture

Hexagonal (ports and adapters). Dependencies point inwards only. See `docs/architecture.md` and
`docs/adr/`.

| Layer | Path | May import |
|---|---|---|
| Domain | `internal/domain/{taxonomy,inventory,catalog,mapping,estimate,assessment,text}` | Other domain packages and an allow list of pure standard packages (no `os`, `net/http`, `unsafe`, ...) |
| Application | `internal/app/analysis` | Domain. Declares the ports (interfaces) it consumes |
| Adapters | `internal/adapter/source/{inventoryfile,aws,azure,gcp}`, `internal/adapter/target/{ovhcloud,scaleway,upcloud}`, `internal/adapter/report/{markdown,jsonreport}`, `internal/adapter/request/assessmentrequest` (the HTTP API's request decoder), and `internal/adapter/internal/jsondoc`, the JSON decoder they share | Domain, the ports they implement, SDKs. Never `internal/cli`, `internal/httpapi` or `cmd/` |
| Driving adapters | `internal/cli` (the CLI) and `internal/httpapi` (the HTTP API, ADR 0019) | Application, domain, `internal/version`; never each other, an adapter or `cmd/` |
| Composition roots | `cmd/shift2eu` (the CLI, offline: never imports `internal/httpapi`) and `cmd/shift2eu-server` (the HTTP API, PRD #103 milestone 3; never imports `internal/cli`) | Everything but a cloud SDK; the only places adapters are constructed. Each one's imports are a strict allow list (`composition-root`, `server-composition-root`): the module's own packages and the standard packages it needs, no third-party package |

- The dependency rule is **enforced by `golangci-lint` (depguard)** and, package by package for every
  release target, by `test/architecture` (ADR 0012). Do not add `//nolint:depguard`; fix the design.
  `test/lint` fails on any nolint directive that would hide a depguard or forbidigo issue, and nolintlint wants a named
  linter and a reason on every directive (`//nolint:gosec // why`).
- Cloud SDKs live only in their provider's adapter package (depguard). SDK types never cross the adapter boundary.
  Source adapters call only read verbs on an SDK client and import only a strict allow list (forbidigo
  `cloud-read-only`, depguard, `test/lint`; `docs/architecture.md`, Cloud read-only rule).
- Providers meet only through the taxonomy. Never write a direct source-service-to-target-service table.
- Every package has a `doc.go` stating its responsibility. Read it before changing the package; update it when the responsibility changes.

## Commands

```bash
make worktree BRANCH=feat/12-short-name   # create or resume the worktree for a PR (prints its path)
make check             # lint + test + govulncheck: must pass before every commit
make test              # unit + contract tests, race detector, no network
make test-integration  # real cloud APIs, read-only credentials (never in default CI)
make test-e2e          # build the binary and run it against testdata fixtures
make lint              # golangci-lint natively and for every release target, then test/lint, incl. architecture rules
make fmt               # gofumpt + goimports
make build             # ./bin/shift2eu and ./bin/shift2eu-server (the HTTP API)
make server            # ./bin/shift2eu-server only
```

## Testing

- Test first. Behaviour changes start with a failing test (skill: `go-tdd`).
- Table-driven tests, black-box `package foo_test`, standard `testing` plus `go-cmp`. Fakes, not mocks.
- Shared adapter behaviour is pinned by contract suites: `internal/adapter/source/sourcetest`,
  `internal/adapter/target/targettest`. Every provider adapter runs them.
- Renderers and CLI output use golden files under `testdata/`, updated with `-update`.
- Integration tests use `//go:build integration`; e2e tests use `//go:build e2e` and live in `test/e2e`.
- Default tests never touch the network, never need credentials, never depend on time or map order.

## Rules

1. **Never write to a cloud.** Source adapters use read-only APIs only. No create, update, delete or tag calls.
2. **Never commit real data.** Inventories and reports from real accounts stay out of git. Fixtures are
   sanitised (`123456789012`, `192.0.2.0/24`, `example.com`).
3. **Never handle credentials yourself.** Use the SDK's default credential chain. Never read, print, log or
   persist credentials or the files that hold them. The agents' API keys and tokens are in the
   environment and in `~/.config/dot-agent-deck/daemon.env`: never print the environment (`env`,
   `printenv`, `set`), never read that file, and never copy either into files, output or tests.
4. **Never work on `main`.** Every PRD milestone or issue gets its own branch, `<type>/<issue>-<slug>`, in
   its own worktree under `.worktrees/` (`make worktree BRANCH=...`, skill: `start-work`). Work only
   inside that worktree; the repository root stays on `main` and is never edited. Never commit or push to
   `main`, and never pass `--no-verify` to get past the hooks that refuse it. Commit messages are
   Conventional Commits (`feat(mapping): ...`, `fix(aws): ...`, `test(...)`, `docs(...)`).
5. Architectural decisions get an ADR in `docs/adr/` (skill: `adr`).
6. New third-party dependencies need a reason in the commit message. Prefer the standard library.
7. Version bumps come from Renovate PRs (`.github/renovate.json5`), not by hand. Renovate finds `go.mod`
   and workflow actions by itself; any other pinned version (such as the `param:` versions in
   `dev/lima.yaml`) needs a `# renovate: datasource=... depName=...` comment on the line above. Keep those
   comments when editing, and add one to every new pin of that kind.
8. **Never delete a local branch.** That is the maintainer's job: `.claude/settings.json` denies
   `git branch -D` on purpose. Never work around the deny (`git branch -d`, `git update-ref -d`, deleting
   refs by hand). For a cleanup, write the local branches to delete, one per line, to a file in your
   scratchpad or temporary directory, and give the maintainer its absolute path and the command
   `xargs git branch -D < <file>` (`! xargs git branch -D < <file>` in Claude Code). Remove worktrees
   (`git worktree remove`) and delete remote branches (`git push origin --delete <branch>`) yourself. A
   worker hands the list to the orchestrator, which passes it on.
9. **Change files with your file-editing tool, never through the shell.** No heredoc, `cat >`, `tee`,
   `sed -i` or scripted rewrite (`python3 - <<'EOF'`) of a file; the formatters and generators `make`
   and the tests run (`make fmt`, `-update`) are fine. Write scratch files with that tool too, in your
   scratchpad or temporary directory. Write commit messages and PR bodies longer than one line the same
   way (`git commit -F <file>`, `gh pr create --body-file <file>`). Never `rm` a path held in a variable
   or built at run time (`rm -rf "$dir"`); leave scratch directories for the system to clean. Why: such
   commands stop an unattended pane at a permission prompt nobody answers (issue #251).

## Multi-agent workflow

The project is driven by [dot-agent-deck](https://github.com/vfarcic/dot-agent-deck) with six roles,
defined in `.dot-agent-deck.toml`: **orchestrator**, **coder**, **tester**, **reviewer**, **auditor**,
**releaser**. It runs inside an isolated Lima VM (`docs/dev-environment.md`).

| Role | Agent and model | Skills |
|---|---|---|
| orchestrator | Claude Code, Opus | `start-work`, `prd-create` |
| tester | pi, DeepSeek V4.1 Flash | `go-tdd` |
| coder | Claude Code, Opus | `go-implement`, `add-cloud-provider`, `adr` |
| reviewer | Claude Code, Opus | `review-change` |
| auditor | Claude Code, Opus | `security-audit` |
| releaser | Claude Code, Sonnet | `pr-create`, `release-tag` |

Tests and implementation come from different model families on purpose (`docs/adr/0003-...`).
Skills live only in `.claude/skills`: Claude Code finds them there, and pi loads them through
`.pi/settings.json` (which needs `pi --approve`). Keep skills agent-neutral: refer to files and shell
commands, never to one agent's built-in tools.

Workers start with no memory of earlier tasks: everything needed is in the task text, the PRD, this file
and the skills. The first line of every task, `Worktree: <path>`, names the worktree to work in. Workers report only to their orchestrator via `dot-agent-deck work-done`; units report
to the dispatcher, and only the dispatcher talks to the maintainer. The single human gate is the
maintainer's approval and merge of a PR (PRD #161): no agent approves, merges an unapproved PR or enables
auto-merge. The AI reviewer's comments and change requests are advice the maintainer weighs. After the
switch to the agents App, nobody pushes to an agent's branch by hand: a push by the maintainer could not
be approved (`require_last_push_approval`).

### Dispatcher

Above the orchestrations sits the dispatcher: the pane that talks to the maintainer, dispatches each
issue or PRD milestone as a unit, verifies what the units report, closes a unit once its PR is ready and
cleans up after a merge. Its process is the `dispatch` skill; its memory holds only its state (the queue,
holds and decisions). The skill refers to these values by name:

| Name | Value |
|---|---|
| Orchestration | `shift2eu` |
| CLI | `~/.local/bin/dot-agent-deck` |
| Unit limit | 3 open units (2 when the VM has 8 GiB or less), counting running units only: a unit is closed once its PR is verified and reported ready |
| Review limit | 5 PRs waiting for the maintainer's review or merge, not counting the release PR (label `autorelease: pending`) or Renovate's dependency PRs (author `app/renovate`, label `dependencies`) |
| Checks on a merged scratch tree | `make check` and `make test-e2e` |
| Maintainer notes directory | `.idea/` in the repository root (git-ignored) |
| Release flow | The release-please PR, reviewed with the `release-tag` skill |
