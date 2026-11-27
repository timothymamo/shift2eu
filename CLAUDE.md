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
from PRD #89, the JSON report from PRD #103, all in `prds/done/`), or as a PDF (`--format pdf`, ADR 0049,
PRD #584 milestone 1). It reads inventory
schema versions 1 and 2 (ADR 0006, ADR 0025). From a sized inventory it names a plan and estimates a
monthly list price per resource, per workload and in total from the snapshots' dated prices (on by
default, `--no-estimate` turns it off), groups resources by workload, notes blockers on related
resources, and scopes a target to one region with `--region` (PRD #190, ADRs 0024 and 0026-0030, in
`prds/done/`). The HTTP API (PRD #103) is retired (ADR 0037). A WebAssembly module,
`cmd/shift2eu-web` (ADR 0035), runs the same assessment in the browser and renders it as HTML. The site's
pages (`site/`) load it under a strict Content Security Policy, and `make site` (`ci/sitebuild`) assembles
them into a reproducible bundle that a headless-browser smoke test (`test/browser`, `make test-browser`)
drives (ADR 0036); the release builds, smoke-tests, attests and publishes the bundle, deploys it behind
`SITE_DEPLOY_ENABLED` and checks the live site, and a scheduled workflow (`site-check.yml`) repeats that
check daily (ADR 0043). It is live at `https://www.shift2eu.com` since v0.16.1 (PRD #297, milestones 1
to 4, in `prds/done/`). `infra/` holds the OpenTofu configuration of the site's Scaleway hosting
(`infra/site`, ADR 0039, no Go): the maintainer alone runs its plan and apply, on the admin machine;
agents never plan or apply, and `make infra-check` checks it offline. Live source adapters do not exist
yet. Work is planned as PRDs in `prds/`.

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
| Adapters | `internal/adapter/source/{inventoryfile,aws,azure,gcp}`, `internal/adapter/target/{ovhcloud,scaleway,upcloud}`, `internal/adapter/report/{markdown,jsonreport,pdfreport,htmlreport}` (`pdfreport`: the PDF report and its standard-library writer, given the version line by its constructor, ADR 0049; `htmlreport`: the site's HTML fragment, ADR 0035), and `internal/adapter/internal/jsondoc`, the JSON decoder they share | Domain, the ports they implement, SDKs. Never `internal/cli`, `internal/web` or `cmd/` |
| Driving adapters | `internal/cli` (the CLI) and `internal/web` (the site's WebAssembly adapter, ADR 0035) | Application, domain, `internal/version`; never each other, an adapter or `cmd/`. `internal/web` also never `os`, `net` or `syscall/js`. Repository-wide, only the test files of `internal/cli` and `cmd/shift2eu-web/internal/wire` may import `internal/cli` (`cli-only-in-named-tests`, held exactly by `test/architecture`'s `TestOnlyNamedPackagesTestTheCLI`) |
| Composition roots | `cmd/shift2eu` (the CLI, offline) and `cmd/shift2eu-web` (the site's module, `js && wasm`, the only importer of `syscall/js`) with its untagged wiring package `cmd/shift2eu-web/internal/wire` (ADR 0035) | Everything but a cloud SDK; the only places adapters are constructed, besides `ci/sitebuild` (below). Each one's imports are a strict allow list (`composition-root`, `web-composition-root`, `web-composition-root-wire`): the module's own packages and the standard packages it needs, no third-party package. Among the composition roots' test files, only wire's import `internal/cli`, for the parity tests |
| CI tooling (no part of the product) | `ci/aireview` (the AI reviewer's workflow program, ADR 0033) and its library `ci/aireview/internal/review`; `ci/internal/claudecode`, the Claude Code CLI launcher the reviewer's library shares with the scheduled audit (issue #277, ADR 0040: the model and the tools as parameters, no tools the default); `ci/claudecodepin` (the Claude Code pin check `claude-code-pin.yml` runs against the one pin file `.github/claude-code.yaml`, issues #276 and #277) and its library `ci/claudecodepin/internal/pincheck`, which does no I/O; `ci/auditexec`, the scheduled audit's command wrapper `/srv/audit/bin/audit-exec` (issue #277, ADR 0040); `ci/auditrun`, the launcher the audit's model step runs, and its library `ci/internal/audit`, the audit's six steps from the managed-settings refusal to the sandbox preflight and the audit (issue #277, ADR 0040); `ci/auditpublish/internal/publish`, the scheduled audit's model-free publish core, which does no I/O (PRD #175, ADR 0040); `ci/auditpublish`, the publish program's entry point (its `artifacts`, `image` and `publish` subcommands), and its GitHub side `ci/auditpublish/internal/github`, which runs `gh` and gitleaks by absolute path and checks every GitHub call against a fixed endpoint table (PRD #175, ADR 0040); `ci/sitebuild`, the program `make site` runs to assemble the site's bundle (PRD #297); `ci/sitedeploy`, the site's deploy program (S3 API, SigV4 by hand), and its library `ci/sitedeploy/internal/deploy`; `ci/sitecheck`, the live site check against a release's manifest, and its library `ci/sitecheck/internal/livecheck`, which builds the `gh` commands as argv and runs none (PRD #297 milestone 3, ADR 0043, `site-check-main`, `site-check-library`) | Named standard packages only (`ai-reviewer-main`, `ai-reviewer-is-standard-library`, `claude-code-launcher`, `claude-code-pin-main`, `claude-code-pin-library`, `audit-exec`, `audit-main`, `audit-library`, `audit-publish-library`, `audit-publish-github`, `audit-publish-main`, `site-deploy-main`, `site-deploy-library`, `site-check-main`, `site-check-library`), the reviewer's library and the audit's library also the launcher, which nothing else imports (`claude-code-launcher-importers`), and only `ci/auditrun` and `ci/auditexec` the audit's library (`audit-library-importers`); no release builds them but `ci/sitedeploy` and `ci/sitecheck`, which `release.yml`'s `site-programs` job builds and its `site-deploy` and `site-check` jobs run (PRD #297 milestone 3); the scheduled `site-check.yml` also runs `ci/sitecheck` daily with `go run` (milestone 4, ADR 0043), and Go's internal rule keeps the product from importing them. The exception is `ci/sitebuild` (`site-builder`, ADR 0036): it also imports and constructs the three embedded-snapshot targets, `internal/domain/catalog` and `internal/version` to write the snapshot dates, regions and version into the pages, the one place outside `cmd/` that constructs adapters. A main package nothing imports; the release runs it |
| Site (no Go) | `site/`: the site's pages, stylesheet and one page script (ADR 0036); `site/records/` is never bundled | Nothing: the page script is held by `test/site`'s banned-API scan, and every page by its Content Security Policy |
| Infrastructure (no Go) | `infra/`: the OpenTofu configuration of the site's Scaleway hosting (`infra/site`, ADR 0039), the OpenTofu pins (`infra/tofu.yaml`) | Only the `scaleway/scaleway` provider, run only through `dev/tofu.sh`; held by `dev/infra-rules.sh`'s static rules and checked offline by `make infra-check`. Plan and apply are the maintainer's alone: agents never run them |
| Browser smoke test | `test/browser` (`browser && !js`), run by `make test-browser` against a pinned headless Chromium | Exactly its listed standard and `chromedp` packages (`browser-smoke-test`), no package of the module; only it imports `chromedp` (`chromedp-only-in-the-browser-test`) |
| Site front test | `test/sitefront`: the static Caddyfile test (under `make test`) and the behaviour test (`sitefront && !js`), run by `make test-sitefront` against the pinned Caddy image (ADR 0045) | Exactly its listed standard packages (`sitefront-behaviour-test`), no package of the module; its `doc.go` imports nothing (`sitefront-test-doc`) |

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
make check             # lint + test + govulncheck + infra-check: must pass before every commit
make infra-check       # infra/'s OpenTofu config offline (pinned tofu via dev/tofu.sh); agents never plan or apply
make test              # unit + contract tests, race detector, no network of its own (module fetches: see Testing)
make test-integration  # real cloud APIs, read-only credentials (never in default CI)
make test-e2e          # build the binary and run it against testdata fixtures
make lint              # golangci-lint natively and for every release target, then test/lint, incl. architecture rules
make fmt               # gofumpt + goimports
make build             # ./bin/shift2eu
make site              # the site's bundle into bin/site and its archive bin/shift2eu-site_<version>.tar.gz
make site-programs     # ci/sitedeploy and ci/sitecheck into bin/, static and -trimpath, as the release builds them
make test-browser      # a bundle of its own (never bin/site), then the headless-browser smoke test in a pinned Chromium container
make test-sitefront    # the site front's behaviour test in the pinned Caddy container (host port 8080: one run at a time)
```

Every make target runs exactly go.mod's go line (`GOTOOLCHAIN`, issue #347, ADR 0035). When the local Go is
another release, the go command fetches the pinned toolchain once (checksum-verified via `sum.golang.org`)
before anything runs: the only network access the pin adds (the tests' own is under Testing). The VM
installs the pinned Go, so normally nothing is fetched. `GOTOOLCHAIN=local make ...` never downloads; a
mismatch then fails `TestGoModPinsTheBuildingToolchain` instead. `make infra-check` fetches, on first use
per worktree, only the OpenTofu release `infra/tofu.yaml` pins (SHA-256-checked) and the `scaleway/scaleway`
provider (checked against `infra/site/.terraform.lock.hcl`); it fetches nothing else, and never contacts Scaleway.

## Testing

- Test first. Behaviour changes start with a failing test (skill: `go-tdd`).
- Table-driven tests, black-box `package foo_test`, standard `testing` plus `go-cmp`. Fakes, not mocks.
- Shared adapter behaviour is pinned by contract suites: `internal/adapter/source/sourcetest`,
  `internal/adapter/target/targettest`. Every provider adapter runs them.
- Renderers and CLI output use golden files under `testdata/`, updated with `-update`.
- Integration tests use `//go:build integration`; e2e tests use `//go:build e2e` and live in `test/e2e`.
- Default tests never open a connection themselves, never need credentials, never depend on time or map
  order. Only go commands fetch: the one that runs them may fetch the pinned toolchain, once, before any test starts
  (see Commands), and on an empty module cache `TestNoProductBinaryReachesChromedp`'s `go list` fetches
  modules through GOPROXY, verified by go.sum (issue #391).

## Rules

1. **Never write to a cloud.** Source adapters use read-only APIs only. No create, update, delete or tag calls.
2. **Never commit real data.** Inventories and reports from real accounts stay out of git. Fixtures are
   sanitised (`123456789012`, `192.0.2.0/24`, `example.com`).
3. **Never handle credentials yourself.** Use the SDK's default credential chain. Never read, print, log or
   persist credentials or the files that hold them. The agents' API keys and tokens are in the
   environment and in `~/.config/dot-agent-deck/daemon.env`: never print the environment (`env`,
   `printenv`, `set`), never read that file, and never copy either into files, output or tests.
4. **Never work on `main`.** Every PRD milestone or issue gets its own branch, `<type>/<issue>-<slug>`, in
   its own worktree under `.worktrees/` (`make worktree BRANCH=...`, skill: `start-work`); a Renovate
   PR's existing `renovate/<name>` branch is resumed with `make worktree BRANCH=renovate/<name>`, never
   created. Work only
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
   `xargs git branch -D < <file>` (`! xargs git branch -D < <file>` in Claude Code), which the maintainer
   runs in the VM, never in the workspace on the Mac (`docs/dev-environment.md`, The host clone). Remove
   worktrees (`git worktree remove`) and delete remote branches (`git push origin --delete <branch>`)
   yourself. A worker hands the list to the orchestrator, which passes it on.
9. **Change files with your file-editing tool, never through the shell.** No heredoc, `cat >`, `tee`,
   `sed -i` or scripted rewrite (`python3 - <<'EOF'`) of a file; the formatters and generators `make`
   and the tests run (`make fmt`, `-update`) are fine. Write scratch files with that tool too, in your
   scratchpad or temporary directory. Write commit messages and PR bodies longer than one line the same
   way (`git commit -F <file>`, `gh pr create --body-file <file>`). Never `rm` a path held in a variable
   or built at run time (`rm -rf "$dir"`); leave scratch directories for the system to clean. Why: such
   commands stop an unattended pane at a permission prompt nobody answers (issue #251).
10. **Signal only what you started.**
    - Never (other agents' runs share this machine, and your run shares its group with your shell or
      your agent's tool):
      - send a signal (`kill`, `pkill`, `killall`) to a process you did not start yourself;
      - pick a process to signal by its name (`pgrep`, `pidof`, `pkill`);
      - signal a process group yourself (stopping a background run through your agent's tool is the
        exception: the tool started that group).
    - To be able to stop a run of your own: prefer your agent's tool's background mode and stop it through
      that tool, which stops the whole run. Otherwise:
      - start it as
        `cd <path> && { <cmd> < /dev/null > <log file in your scratchpad or temporary directory> 2>&1 & echo $!; ps -o lstart= -p $!; }`
        (the braces background only `<cmd>`; the redirects keep your shell tool from waiting on it) and
        note the pid and its start time;
      - to stop it, signal only that pid, and only after `ps -o lstart=,args= -p <pid>` shows both the
        start time and the command you noted: a pid is reused once its process exits, and every unit runs
        the same `make check`.
    - Leftovers: processes the run started (test binaries, a container) may outlive it; wait for the log to
      stop growing and report what still runs; never look them up (by name, `ps | grep` or group) to signal
      them.
    - If you kept no pid, or the run has exited, signal nothing: wait, or report it.
    - Why: a coder sent SIGTERM to the process group of a `make` it found with `pgrep -x make`, which may
      have been another unit's run (issue #465).

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
commands, never to one agent's built-in tools. Rule 3 has a second layer in both agents: the deny rules of
`.claude/settings.json`, and pi's project extension `.pi/extensions/deny-env-printing.ts`, which refuses
the commands `.pi/deny-env-printing.json` lists (ADR 0044). The tester starts through `dev/pi-tester.sh`,
which keeps the other agents' keys out of pi's environment (and, with its marker, out of login shells) and
refuses to start pi unless node loads that extension (issue #662); it still runs as the same user, so the
keys file and the App token file stay readable to it, guarded only by the deny patterns.

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
holds, decisions and the unit branches of waiting PRs). The skill refers to these values by name:

| Name | Value |
|---|---|
| Orchestration | `shift2eu` |
| CLI | `~/.local/bin/dot-agent-deck` |
| Unit limit | 3 running units (2 when the VM has 8 GiB or less): a unit is closed once its PR is verified and reported ready |
| Review limit | 10 PRs waiting for the maintainer's review or merge from 09:30 until 20:00, 20 from 20:00 until 09:30 (the local clock of the machine the deck runs on; counted as the sentence below the table says), not counting Renovate's PRs (author `app/renovate`; their label `dependencies` alone excludes nothing) or release-please's release PR (author `app/shift2eu-release`, head branch `release-please--branches--*` and label `autorelease: pending`, never the label alone) |
| Checks on a merged scratch tree | `make check` and `make test-e2e` |
| Maintainer notes directory | `.idea/` in the repository root (git-ignored) |
| Release flow | The release-please PR, reviewed with the `release-tag` skill |

The review limit that counts is the one in force when a unit is dispatched, against the waiting PRs plus
the running units that will open a PR plus the new unit; after 09:30, units still running finish and their
PRs count, nothing new is dispatched until that count, the new unit included, fits the day limit, and a
finished PR is never held back or closed to get under it.
