# PRD #646: Observability of the agent setup with Honeycomb

**Status:** Draft
**Issue:** #646

## Problem

The project is built by six agent roles that dot-agent-deck runs in a Lima VM on the maintainer's Mac
Mini ("crunch"; `docs/dev-environment.md`), with a dispatcher above them that keeps up to three units
running day and night. What that setup costs and where it stalls is known today only from what the
maintainer happens to see in the deck's panes, the dispatcher's notes in `.idea/` and GitHub:

- **Who needs the maintainer.** Which agent, model or role asks for help most, and how often a unit stops
  at a prompt nobody answers (issue #251) or misses a wake-up and has to be restarted by hand (the
  dispatcher's restart log).
- **Where the time goes.** How much of a unit's time is spent in `make check`, `make test-e2e`, `go test`
  or CI, and how long a unit sits idle or waits for a report to be read.
- **What a PR costs.** Tokens and the estimated cost per unit and per PR, per model (Opus, Sonnet,
  DeepSeek), and CI time per PR.
- **The machine.** The VM has 4 CPUs, 16 GiB of memory and 40 GiB of disk (`dev/lima.yaml`). A full disk
  has already forced `go clean -cache` more than once, and the unit limit drops to two on a smaller VM.
  Nothing records CPU, memory or disk headroom over time, so a full disk is found when a build fails.

dot-agent-deck can already send all of this as OpenTelemetry (its `docs/telemetry.md`, read for this PRD
on 2026-11-26 at tag v0.51.2, the version `dev/lima.yaml` pins), and Honeycomb's free plan is large enough
for one maintainer's agents. Nothing is set up: telemetry is off because no endpoint is configured.

## Outcome

The deck's traces and logs, Claude Code's and Pi's own telemetry, the deck's per-run resource figures and
the VM's machine metrics reach one Honeycomb environment on the free plan, through one OpenTelemetry
Collector on the VM that holds the only copy of the ingest key in the VM and drops what must not leave.
`dev/lima.yaml` provisions everything except the key, pinned by version and checksum with Renovate
comments, and `make check` pins that provisioning. `docs/dev-environment.md` has a runbook to create the
account and key, put the key on the VM, check that data arrives, and turn it all off. A saved set of
Honeycomb queries answers the questions above. No agent ever reads, prints or holds the key, and no
prompt, file content, tool argument, e-mail address or local path leaves the machine.

## Scope

| | In scope | Out of scope |
|---|---|---|
| Source providers | None: this PRD changes the development environment, not the product | AWS, Azure, GCP |
| Target providers | None | OVHcloud, Scaleway, UpCloud |
| Taxonomy categories | None | All |
| Machines | The Lima VM `shift2eu`; as an option, its Mac host (the Mini) and Lima's own processes | The admin machine (`mac-mini-home`), the maintainer's other Macs, Scaleway's site hosting (`infra/site`) |
| Signals | The deck's traces and warning logs; Claude Code's metrics and events; Pi's usage logs; `dot-agent-deck-collector`'s per-run metrics; host metrics of the VM (and, optional, the Mac) | Claude Code's beta tracing; any content switch; the deck's local debug log |
| CI | The deck's own `agent.ci` spans (CI time as the deck sees it) | GitHub Actions traces, and anything sent from the scheduled audit, the release or any other workflow: the CI's data stays in GitHub (decision 3) |
| Backend | Honeycomb, free plan, one environment | Paid plans, a self-hosted backend, a second vendor |

The product (`shift2eu` and the site) gains no telemetry: nothing here touches `cmd/`, `internal/` or
`site/`.

## What is available today

Read and confirmed for this PRD on 2026-11-26; each line names its source.

### dot-agent-deck (v0.51.2, `docs/telemetry.md` at that tag)

- **Off unless configured.** The daemon sends nothing until `OTEL_EXPORTER_OTLP_ENDPOINT` (or the
  traces-only endpoint) is in the **daemon's** environment when it starts. It reads the variables once;
  a change needs a daemon restart, which stops the running agents. OTLP over HTTP only (`http/protobuf`
  or `http/json`), no gRPC, no client certificates, no redirects followed.
- **Traces.** `agent.run` (one per run: agent, role, orchestration, dispatch name, `deck.work.id`,
  repository name, starting branch, PR numbers, interventions, operations, skills and CI rounds counts,
  tokens per model with provenance, cost estimate, how it ended), with span events `deck.status_change`,
  `deck.intervention` (kind only), `deck.skill` (name of a skill defined in a skill directory),
  `deck.ci.change` and `deck.model_usage`; children `agent.status` (one per stretch in one status),
  `agent.operation` (one per shell command: program and first word only, by strict rules),
  `agent.ci` (one per stretch of a PR's checks running, sent once per PR), `agent.handoff` and
  `agent.handoff.read` (an orchestrator's delegations and the reading of their reports). `daemon.health`
  about once a minute (memory, open runs, hook and attach latency, dropped spans).
- **Logs.** Every warning and error the daemon logs, as an OTLP log record with its source location and
  field names only, never the message.
- **Never sent:** prompts, typed input, file contents, agent output, tool arguments, task and report text,
  secrets, environment values, header values, command lines (beyond the program and first word), working
  directory paths, remote URLs, error messages. Sent as written: `OTEL_SERVICE_NAME`,
  `OTEL_RESOURCE_ATTRIBUTES`, orchestration and role names, the dispatch name, the repository name, the
  branch an agent starts on (`vcs.ref.head.name`) and skill names.
- **Agents' own telemetry.** The deck passes its `OTEL_*` variables to every agent it starts, adds the
  run's ids to `OTEL_RESOURCE_ATTRIBUTES` (`deck.run.id`, `deck.work.id`, ...), and switches on:
  Claude Code's metrics and events (`CLAUDE_CODE_ENABLE_TELEMETRY=1`, OTLP exporters, content switches
  left off), and Pi's usage logs through the deck's Pi extension (`pi.model_usage`, `pi.tool_result`,
  session and agent start and end), always as OTLP/JSON. Each joins the deck's run on `deck.run.id`.
- **Per-run resources.** `dot-agent-deck-collector`, a separate program, Linux only, released beside the
  deck (`dot-agent-deck-collector-linux-{amd64,arm64}`, present in v0.51.2's assets and its
  `checksums.txt`). Every 15 s (`--interval`) it sends each run's CPU time, proportional memory, process
  count and, for a daemon run as the service, the run's cgroup CPU and memory; every 5 min
  (`--disk-interval`) each worktree's disk use under a hashed `deck.worktree.id`. It runs as a systemd
  user service with its own `EnvironmentFile`. It never sends process names, command lines, paths or user
  names unless asked (`--send-worktree-paths`, `--all-users`).
- **Trimming.** The deck's docs show an OpenTelemetry Collector on `127.0.0.1:4318` that deletes
  attributes (`vcs.ref.head.name`, `deck.operation.word`, ...), deletes `deck.skill.name` from span
  events and drops whole spans, then exports to Honeycomb with the key from its own environment.
- **Honeycomb example.** `OTEL_EXPORTER_OTLP_ENDPOINT=https://api.honeycomb.io` (EU team:
  `https://api.eu1.honeycomb.io`), `OTEL_EXPORTER_OTLP_HEADERS=x-honeycomb-team=<ingest key>`; the deck's
  spans land in a dataset named after the service, `dot-agent-deck`, Claude Code's events in
  `claude-code`, metrics in the environment's metrics dataset.

### Claude Code (`https://code.claude.com/docs/en/monitoring-usage`, read 2026-11-26)

- Metrics every 60 s by default (`OTEL_METRIC_EXPORT_INTERVAL`): `claude_code.session.count`,
  `token.usage`, `cost.usage`, `lines_of_code.count`, `pull_request.count`, `commit.count`,
  `code_edit_tool.decision`, `active_time.total`. Events every 5 s: `user_prompt` (length only),
  `assistant_response` (length only), `tool_result`, `tool_decision`, `api_request` (model, tokens,
  cost, duration), `api_error`, `api_refusal` and others.
- **Every metric and event carries `user.email` when available, with no switch to turn it off**, and
  `organization.id`, `user.account_uuid`, `user.account_id` (switch `OTEL_METRICS_INCLUDE_ACCOUNT_UUID`),
  `user.id` and `session.id`. These identify the maintainer's Anthropic account.
- Content switches, all off by default and never set by the deck: `OTEL_LOG_USER_PROMPTS`,
  `OTEL_LOG_ASSISTANT_RESPONSES`, `OTEL_LOG_TOOL_DETAILS`, `OTEL_LOG_TOOL_CONTENT`,
  `OTEL_LOG_MANAGED_SETTINGS`, `OTEL_LOG_RAW_API_BODIES`.

### Our VM (`dev/lima.yaml`, `docs/dev-environment.md`)

- The deck is pinned at `v0.51.2` (`param: dotAgentDeckVersion`, Renovate comment); v0.51.3 is out. Its
  binary is checked against the release's `checksums.txt`.
- The daemon runs as a systemd user service with two drop-ins written by provisioning:
  `credentials.conf` (`EnvironmentFile=-%h/.config/dot-agent-deck/daemon.env`) and `terminal.conf`.
- `daemon.env` is written by `make vm-env` from the Mac's host clone, over standard input, and **the
  default `VM_ENV` replaces the whole file**: a line added to it by hand on the VM is gone after the next
  plain `make vm-env`. Every login shell in the VM sources it (`/etc/profile.d/shift2eu.sh`), and every
  agent gets every variable in it ("Every agent gets every variable").
- The VM user has passwordless sudo (`dev/lima.yaml`, ADR 0031).
- `dev/lima.yaml` declares no `portForwards`, so Lima's default applies: a TCP port a guest process
  listens on at `127.0.0.1` is forwarded to the Mac's `127.0.0.1`.
- Rule 3's deny rules: `.claude/settings.json` (`Bash(*daemon.env*)`, `Bash(*.config/dot-agent-deck*)`,
  `Read(~/.config/dot-agent-deck/**)`, the `$*API_KEY`/`$*TOKEN` expansions, `/proc/*/environ`, ...) and
  `.pi/deny-env-printing.json` (ADR 0044).
- The repository is public, so branch names, the repository name and skill names are public already.

### Honeycomb's free plan (read 2026-11-26)

| Limit | Value | Source |
|---|---|---|
| Events | Up to 20M a month | `https://www.honeycomb.io/pricing` |
| Metrics data points | Up to 100M a month | `https://www.honeycomb.io/pricing` |
| Triggers | 2 | `https://www.honeycomb.io/pricing` |
| SLOs | None | `https://www.honeycomb.io/pricing` |
| Retention | Events and logs 60 days from ingest; time-series metrics 13 months by default | `https://docs.honeycomb.io/get-started/manage-costs/how-honeycomb-calculates-usage.md` |
| What counts | Each span is an event, and so is each span event and link; metrics count by data points ingested, not by series; rejected and pre-sampled data does not count | same |
| Daily target | Monthly limit ÷ 30.4: about 658,000 events and 3.29M data points a day | same |
| Over the limit | After 10 days above the daily target Honeycomb throttles: it accepts 1 event in 10, until the rate stays under target for 72 hours. A day above twice the target is not counted, up to 3 times a calendar month | same |
| EU region | Available on the free plan, sign-up at `https://ui.eu1.honeycomb.io/signup`, ingest at `https://api.eu1.honeycomb.io`; a team cannot move region, only sign up again | Honeycomb's EU data residency post and support article "Can I migrate to Honeycomb's EU instance" |
| Rate limit per second | *To be checked:* the only free-plan figure found (2,000 events a second) is from an old help article; the current support page does not show a number in what could be read | `https://support.honeycomb.io/articles/2222640834` |

## The signals and the estimate

Assumptions, to be replaced by the figures from milestone 1's first week (acceptance criterion M1.6):
3 units running, about 25 agent runs a day (20 Claude Code, 5 Pi), about 12 runs open at any time, each
run about 250 status changes and 150 shell commands, each Claude Code run about 300 API requests.

### Events a day (limit: about 658,000)

| Signal | Count a day | How |
|---|---|---|
| Deck: `agent.run`, `agent.status`, `agent.operation`, `agent.ci`, handoffs, and the span events on `agent.run` | ~16,500 | ~660 per run × 25 |
| Deck: `daemon.health` | 1,440 | one a minute |
| Deck: warning and error logs | ~500 | unknown until measured |
| Claude Code events (`api_request`, `tool_decision`, `tool_result`, `assistant_response`, prompts) | ~24,000 | ~1,200 per run × 20 |
| Pi logs (`pi.model_usage`, `pi.tool_result`, start/end) | ~3,000 | ~600 per run × 5 |
| **Total** | **~45,000 (≈7 % of the target)** | five times the assumption is still about 34 % |

### Metrics data points a day (limit: about 3.29M)

| Signal | Interval | Count a day |
|---|---|---|
| Claude Code metrics, ~12 series per open session, ~12 sessions | 60 s | ~207,000 |
| `dot-agent-deck-collector`, ~8 points per run × 12 runs + unattributed | 15 s (default) | ~576,000 (~144,000 at 60 s) |
| Its worktree disk figures, ~6 worktrees | 300 s | ~1,700 |
| VM host metrics (CPU per core and state, memory, load, paging, root and workspace filesystems, disk I/O, the main interface), ~150 points | 60 s | ~216,000 |
| Mac host metrics (option), ~200 points | 60 s | ~288,000 |
| **Total** | | **~1.29M (≈39 %) with everything at the defaults; ~860,000 (≈26 %) with the per-run collector at 60 s** |

What keeps it within the limit, in the order to use them: the per-run collector's `--interval` (60 s
instead of 15 s cuts its share by three quarters), the host metrics' scrapers and the mount points and
interfaces they include, `OTEL_METRIC_EXPORT_INTERVAL` for Claude Code, dropping `agent.status` spans
(about a third of the deck's events; status time can then come from the span events alone), and last
`agent.operation`. No sampling of traces is needed at these volumes, and the dispatcher's question set
(who waits, what costs) needs every run, so sampling is not recommended. One of the two free triggers
(milestone 5) watches the usage against the daily target.

## The pipeline

**Option A: direct.** The daemon, the agents and `dot-agent-deck-collector` each send to
`https://api.eu1.honeycomb.io` with the key in their own environment. Nothing to install for the traces.
But the key must be in the daemon's environment, so the deck passes `OTEL_EXPORTER_OTLP_HEADERS` to
**every agent it starts**: every agent process holds the key in its environment, and every agent's
tools can read it (`/proc/self/environ` is denied, a child's `env` is denied, but the key is in the
process all the same). Nothing can drop an attribute, so `user.email` and the account ids reach
Honeycomb. Host metrics need a collector anyway.

**Option B: a local OpenTelemetry Collector (recommended).** `otelcol-contrib`, pinned by version and
SHA-256 with a Renovate comment like every other pin in `dev/lima.yaml`, runs on the VM and:

- receives OTLP over HTTP on `127.0.0.1:4318` only (traces, metrics and logs, protobuf and JSON);
- runs the `hostmetrics` receiver at 60 s (cpu, memory, load, paging, disk, filesystem, network,
  processes), with filesystems limited to `/` and the workspace mount and the interface limited to the
  VM's main one;
- sets `host.name` to a fixed value (`vm-shift2eu`) and adds `deployment.environment.name=agents`;
- deletes what must not leave (Privacy, below), and drops whole spans when decision 6 says so;
- batches, and exports with `otlphttp` to Honeycomb, the key read from its own environment file.

The daemon, the agents and `dot-agent-deck-collector` then get only
`OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318`: no key, no header, nothing secret. The non-secret
endpoint goes in a new drop-in written by provisioning, not in `daemon.env` (decision 4). One process
holds the key, it is the one place to trim, and it buffers and retries when Honeycomb is down.

Its cost is one more pinned binary and one more service; its risk is that a Collector stopped or
misconfigured loses data silently, which the runbook's check and milestone 5's trigger cover.

**Recommendation: B**, run as a **system** service under a dedicated system user (`otelcol`), with the key
in a root-owned file only that user can read (`/etc/shift2eu/otelcol.env`, `root:otelcol`, mode 0640).
The agents run as the VM user, so the key is outside their files and their processes' environment. The VM
user's passwordless sudo means this keeps an agent from the key by rule and by accident, not against an
agent set on it, as ADR 0031 already says of the App token; rule 3's deny rules (below) are the second
layer, and an ingest key can only write data, not read it, so a leak costs junk data, not the record.

## Secrets

- The Honeycomb ingest key is the maintainer's alone. They create it in Honeycomb (an **ingest** key for
  the one environment, not a configuration or management key) and put it on the VM themselves. No agent
  asks for it, reads it, writes it, prints it or copies it; it is never in the repository, a task file, a
  dispatch report, a commit or a PR.
- **Where it lives:** only `/etc/shift2eu/otelcol.env` on the VM (Option B), as
  `HONEYCOMB_API_KEY=<key>`, read by the Collector through `EnvironmentFile=`. **Not** in `daemon.env`:
  everything there reaches every agent and every login shell, and the default `make vm-env` rewrites the
  whole file, so a key added by hand would vanish (decision 5).
- **How it gets there** (decision 5): either by hand in the VM (a `read -rs` prompt piped into
  `sudo install -m 0640 -o root -g otelcol /dev/stdin /etc/shift2eu/otelcol.env`, never on a command
  line), or by a new make target on the Mac's host clone, `make vm-telemetry-key`, which, like
  `make vm-env`, reads one named variable (`HONEYCOMB_API_KEY`) from the environment the host clone's
  `.envrc` sets (where that `.envrc` takes it from, a Keychain lookup or a value, is the maintainer's
  choice, as for `VM_ENV`'s credentials; `docs/dev-environment.md`, The host clone) and writes it over
  standard input. It survives `make vm-delete` and a rebuild, as the key stays on the Mac.
- `dot-agent-deck-collector` runs as the VM user (it must see the agents' processes) and sends to the
  local Collector, so it needs no key either; its `EnvironmentFile` holds only the endpoint and is
  written by provisioning.
- **Provisioning** (`dev/lima.yaml`) installs and pins `otelcol-contrib` and `dot-agent-deck-collector`,
  creates the `otelcol` user, the config, the units and the drop-in, and leaves the key file absent. The
  Collector's unit has `ConditionPathExists=/etc/shift2eu/otelcol.env`, so without the key it does not
  start, and the daemon's `telemetry.conf` is absent too (decision 4), so the deck sends nothing.
- **Rule 3's deny rules** gain, in both `.claude/settings.json` and `.pi/deny-env-printing.json`:
  `Read(//etc/shift2eu/**)` and `Bash(*/etc/shift2eu*)`; `Bash(*otelcol.env*)`; the expansions
  `Bash(*$HONEYCOMB*)` and `Bash(*${HONEYCOMB*)` (only these, so an agent writing M1's tests can still
  name the variable in a command); the Collector's `print-config` subcommand
  (`Bash(*otelcol* print-config*)`), which can print a resolved configuration; and
  `Bash(*systemctl show otelcol*)` style reads of the unit's environment. CLAUDE.md rule 3 names the new
  file beside `daemon.env`.

## Privacy and data minimisation

What each signal may carry, and what happens to it. "Dropped in the Collector" means at **every level**
an attribute can arrive at: the resource (a `resource` processor) and the span, span event, metric data
point and log record (`transform` statements in each of those contexts; an `attributes` processor alone
does not reach resource attributes or span events). `test/devenv` checks both levels for each name.

| Item | Where | Kept or dropped | Why |
|---|---|---|---|
| Prompts, responses, tool input and output, raw API bodies | Claude Code (content switches) | Never sent: the switches stay unset; the Collector also deletes `prompt`, `prompt_text`, `response`, `tool_parameters`, `tool_input`, `error`, `body` should one ever be set | Second fence |
| `user.email`, `user.account_uuid`, `user.account_id`, `organization.id` | Every Claude Code metric and event | Dropped in the Collector; `OTEL_METRICS_INCLUDE_ACCOUNT_UUID=false` in the drop-in too | Identifies the maintainer's account; no question needs it |
| `user.id` | Claude Code | Dropped in the Collector | A stable per-installation id (kept in `~/.claude.json`), so a pseudonymous id of the maintainer's install; no question needs it |
| `session.id` | Claude Code | Kept | Per session; joins events within a session, beside `deck.run.id` |
| `vcs.ref.head.name` (branch), `vcs.repository.name`, `deck.skill.name`, `deck.orchestration.name`, `deck.dispatch.name`, role names | Deck | Kept | The repository is public, so its branches, skills and roles are public; they are how a run is joined to a PR (decision 7) |
| `deck.operation.program`, `.word`, `.commands`, `.wrapped.*` | Deck `agent.operation` | Kept | The "where does shell time go" question needs them; the deck's rules already drop paths, quotes and the arguments of `curl`, `grep`, `ssh` and the like |
| `host.name` | Everything | Replaced by a fixed name (`vm-shift2eu`, `mac-host`) | A Mac's hostname can carry the owner's name |
| Mount points, device and interface names | Host metrics | Limited to the included filesystems and interface; the workspace mount point replaced by a fixed label | The workspace path holds the maintainer's user name |
| Process names and command lines | Host metrics' `process` scraper | Not enabled | Command lines can hold anything |
| Worktree paths | `dot-agent-deck-collector` | Not sent (no `--send-worktree-paths`) | Paths hold the user name; `deck.worktree.id` is enough |

## Repository changes (for the milestones)

- `dev/lima.yaml`: new `param:` pins with Renovate comments for `otelcol-contrib` (version and the
  release's SHA-256 per architecture, `datasource=github-releases depName=open-telemetry/opentelemetry-collector-releases`
  and `github-release-attachments` for the digests, as for `cli/cli` and `cosign`); `dot-agent-deck-collector`
  from the same pinned deck release, checked against the same `checksums.txt`; the `otelcol` user,
  `/etc/shift2eu/otelcol.yaml`, the system unit, the user unit `dot-agent-deck-collector.service` and its
  endpoint-only env file, and the daemon's drop-in `telemetry.conf`, written only while the key file
  exists and removed while it does not (decision 4). No key.
- `dev/otelcol.yaml` (or inline in `dev/lima.yaml`): the Collector's configuration, reviewed like code.
- `docs/dev-environment.md`: a section **Observability** with the runbook: create the Honeycomb account
  (EU region) and environment, create an ingest key, put it on the VM, restart, check (the daemon's log
  line `telemetry: exporting agent-run traces over OTLP`, the Collector's journal, the first `agent.run`
  in Honeycomb), the usage page, rotate the key, and turn everything off.
- `docs/adr/`: one ADR (skill `adr`) recording the pipeline (local Collector), the vendor and region
  (Honeycomb EU, free plan), the pins, where the key lives, and what is never sent.
- `test/devenv`: tests in its existing style pinning the provisioning: the pins and their Renovate
  comments, the checksum checks, that the Collector listens on `127.0.0.1` only, that no content switch
  and no key or header appears in `dev/lima.yaml` or the drop-ins, that the trim list is in the
  Collector's config, that the deny rules cover the new file in both agents.
- `.claude/settings.json`, `.pi/deny-env-printing.json`, `CLAUDE.md` rule 3: the new deny rules.
- `docs/observability-queries.md` (milestone 5): the saved queries and boards.

## Acceptance criteria

- [ ] Given the VM provisioned from `dev/lima.yaml` without a key file, when it boots, then the deck runs
  as before (no `telemetry.conf` drop-in, so no `OTEL_*` variable reaches the daemon or an agent), the
  Collector is installed but not started, and nothing is sent anywhere.
- [ ] Given the key file in place, when the maintainer follows the runbook's check, then an `agent.run`
  span, a `claude_code.api_request` event, a `pi.model_usage` record, a `deck.run.cpu.time` point and a
  `system.cpu.time` point for the VM appear in Honeycomb, all joined by `deck.run.id` where they belong
  to a run.
- [ ] Given a week of data, when the queries of `docs/observability-queries.md` run, then each question of
  the Problem section has an answer.
- [ ] Given any event or data point in Honeycomb, then it holds no `user.email`, no account id, no prompt
  or response text, no tool input, no path under `/Users` or the VM user's home, and its `host.name` is
  one of the fixed names.
- [ ] Given `make check`, then `test/devenv` fails when a pin loses its checksum or Renovate comment, when
  the Collector would listen beyond `127.0.0.1`, when a content switch or an `x-honeycomb-team` value
  appears in the provisioning, or when the deny rules stop covering the key file in either agent.
- [ ] Given an agent in a deck pane, when it tries to read the key file or print the Collector's
  configuration or environment, then the deny rules refuse it.
- [ ] Given the runbook's "turn it off" (remove the key file, then `make deck-restart`, which removes the
  `telemetry.conf` drop-in and restarts the daemon; decision 4), when the maintainer follows it, then the
  Collector is stopped, nothing leaves the VM, and the agents start without `OTEL_*` variables.
- [ ] Given a month of data, then Honeycomb's usage page shows events and data points under the daily
  targets, and the trigger of milestone 5 has not fired.

## Milestones

Each ships as one PR, smallest first. Milestones 2 to 4 each add a signal, so each can be stopped after.

1. [ ] **M1: the local Collector, the deck and the agents.** ADR; `otelcol-contrib` pinned and
   provisioned as the `otelcol` system service with the OTLP receiver, the trim, batching and the
   Honeycomb exporter; the daemon's `telemetry.conf` drop-in pointing the daemon (and so Claude Code and
   Pi) at `http://127.0.0.1:4318` with `OTEL_METRICS_INCLUDE_ACCOUNT_UUID=false`, which provisioning
   and `make deck-restart` write while the key file exists and remove while it does not; the deny rules;
   the runbook; `test/devenv` tests.
   - M1.1 Without the key file the Collector does not start, the drop-in is absent and the deck and the
     agents run exactly as before.
   - M1.2 With it, the runbook's check finds the deck's spans and logs, Claude Code's events and metrics
     and Pi's records in Honeycomb.
   - M1.3 The trim holds: no `user.email`, account id, content attribute or real `host.name` arrives,
     whether it came as a resource attribute or as a span, span-event, data-point or log-record attribute.
   - M1.3a `dev/lima.yaml` has a `portForwards` rule ignoring guest port 4318 (decision 9), pinned by
     `test/devenv`.
   - M1.4 `test/devenv` pins the version, the checksums, the Renovate comments, the `127.0.0.1` listener,
     the trim list and the absence of a key or content switch.
   - M1.5 The deny rules refuse reading `/etc/shift2eu/otelcol.env`, `$HONEYCOMB*` expansions and
     `print-config`, in Claude Code and in pi.
   - M1.6 After a week, the dispatcher records the measured events and data points a day in this PRD,
     replacing the estimate.
2. [ ] **M2: per-run resources.** `dot-agent-deck-collector` installed from the pinned deck release
   (checksum-checked), as a user service sending to the local Collector at the interval of decision 8.
   - M2.1 `deck.run.cpu.time`, `deck.run.memory.pss`, `deck.run.cgroup.*` and
     `deck.worktree.disk.usage` arrive, joined to runs by `deck.run.id`; no path is sent.
   - M2.2 A deck upgrade by Renovate upgrades the collector with it (same `param:`), and `test/devenv`
     pins that.
3. [ ] **M3: the VM's machine metrics.** The `hostmetrics` receiver at 60 s with the scrapers, filesystems
   and interface named above.
   - M3.1 CPU, memory, load, paging, root and workspace filesystem, disk I/O and network points arrive under
     `host.name=vm-shift2eu`; no mount point or device name holds a user name.
   - M3.2 A query shows the free space of `/` and the Go build cache's share of it over time (the cache's
     size from `deck.worktree.disk.usage` or a filesystem point, whichever the milestone settles).
4. [ ] **M4 (optional): the Mac host and Lima.** `otelcol-contrib` for macOS as a LaunchAgent on the Mini,
   host metrics only, sending to the VM's Collector through Lima's port forward (decision 9), so the Mac
   holds no key; Lima's processes through the `process` scraper limited to `limactl`'s names, with no
   command lines.
   - M4.1 Mac CPU, memory and disk points arrive under `host.name=mac-host`.
   - M4.2 The Mac side is installed and removed by make targets in the host clone with runbook steps
     (PASS/FAIL lines), in the style of the VM-boot LaunchDaemon.
5. [ ] **M5: queries, boards and the trigger.** `docs/observability-queries.md`: each question of the
   Problem section as a saved Honeycomb query (starting from the deck's own examples: interventions per
   role, worked time per run, shell time by command, CI time per PR, tokens and cost per `deck.work.id`
   and per PR, blocked and idle time, handoff wait, VM headroom); one board; one of the two free triggers
   on usage against the daily target.
   - M5.1 Each query in the doc runs as written against a week of data.
   - M5.2 The trigger fires in a test (a threshold set low for a minute) and is then set to 80 % of the
     daily target.

## Decisions and open questions

For the maintainer to answer; each has a recommendation.

1. **Vendor and region.** Honeycomb, free plan, in the **EU region** (`api.eu1.honeycomb.io`), one new
   team and environment for the agents. *Recommended: EU*, as the project is about moving to EU
   providers and the free plan offers it; the region cannot be changed later without a new sign-up.
2. **Pipeline.** Direct export (A) or a local Collector (B). *Recommended: B*, as a system service under
   its own user: the only way to keep the key out of every agent's environment and to drop `user.email`.
3. **CI and workflows.** Send GitHub Actions traces, or anything from the scheduled audit or the release?
   *Recommended: no.* The deck's `agent.ci` spans answer "CI time per PR"; the CI's own data stays in
   GitHub, and no workflow gets a Honeycomb secret.
4. **Where the non-secret settings go.** A provisioned drop-in `telemetry.conf` (endpoint
   `http://127.0.0.1:4318`, `OTEL_METRICS_INCLUDE_ACCOUNT_UUID=false`, the metric interval), written
   always, or only once the maintainer turns telemetry on. *Recommended: written by provisioning whenever
   the key file exists, and removed when it does not*, with the Collector starting only when its key file
   exists: one switch, the key file, re-read by provisioning and by `make deck-restart`, the two steps
   that write the daemon's drop-ins. "Off" is removing the key file, then `make deck-restart`, which
   removes the drop-in and restarts the daemon, so the agents get no `OTEL_*` variables at all; a
   drop-in left without a key would keep switching on Claude Code's telemetry with nowhere to send it.
5. **How the key reaches the VM.** By hand in the VM (a `read -rs` prompt into `sudo install`), or a
   `make vm-telemetry-key` target on the Mac's host clone that reads one variable from the host clone's
   environment its `.envrc` sets and writes it over standard input, like `make vm-env`. *Recommended: the make
   target*: it survives a VM rebuild and reuses `make vm-env`'s checks; never `daemon.env`.
6. **Drop whole spans?** `agent.operation` (the deck's only command-line-derived data) and
   `agent.status` (a third of the events). *Recommended: keep both* at first: the volume is far under
   the limit and both answer a question; revisit after M1.6.
7. **Branch and unit names.** Keep `vcs.ref.head.name`, `deck.dispatch.name` and the orchestration name, or
   delete them. *Recommended: keep*: the repository and its branches are public, and they join runs to
   PRs. Delete if the repository ever goes private.
8. **The per-run collector's interval.** 15 s (default, ~576,000 points a day, 1.4 % of a core on a busy
   machine per the deck's docs) or 60 s (~144,000). *Recommended: 60 s*: questions are about units that
   run for hours, and it leaves room for the Mac.
9. **The Mac host.** Leave out, send directly with a second copy of the key, or send through the VM's
   Collector over Lima's default forward of `127.0.0.1:4318`. *Recommended: through the VM's Collector*,
   so there is one key; or leave out until the VM's figures show a need. Either way, **M1** adds a
   `portForwards` rule to `dev/lima.yaml` that ignores guest port 4318, pinned by `test/devenv`, so from
   the Collector's first day no process on the Mac can write into the Honeycomb environment through the
   VM's key; only M4, if the maintainer picks this option, lifts it for that port.
10. **The Collector distribution.** `otelcol-contrib` (every component, larger) or a build of our own with
    the OpenTelemetry Collector Builder holding only `otlp`, `hostmetrics`, `attributes`, `transform`,
    `filter`, `resource`, `batch` and `otlphttp`. *Recommended: `otelcol-contrib`*, pinned and
    checksum-checked: no build to maintain; revisit if its size or its CVEs become a burden. Pin a release
    at least two weeks old (v0.166.0 was published on 2026-11-24).
11. **Deck version.** v0.51.3 is out; let Renovate's PR bring it before M1 or pin M1 to v0.51.2.
    *Recommended: whichever is on `main` when M1 starts*; the telemetry described here is in v0.51.2.
12. **Who reads Honeycomb.** The maintainer alone, or also agents (a Honeycomb MCP server or API key with
    query rights for the dispatcher). *Recommended: the maintainer alone* for this PRD; an agent with read
    access is a separate decision with its own key and ADR.
