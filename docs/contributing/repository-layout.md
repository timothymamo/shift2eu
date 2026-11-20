# Repository layout

Where everything lives in the repository: the two composition roots, the hexagon's layers under
`internal/`, the repository-wide tests under `test/`, the documentation, and the files that set up the
agents' environment. [Architecture](../architecture.md) explains why it is laid out this way.

```
cmd/shift2eu/                 CLI entry point and composition root
cmd/shift2eu-web/             The site's WebAssembly module and composition root
internal/
  domain/                     Pure business rules, standard library only
    taxonomy/                   provider-neutral service categories
    inventory/                  what runs in the source today
    catalog/                    what a target offers
    mapping/                    matching resources to offerings
    assessment/                 verdicts, blockers, readiness
  app/analysis/               The analyse use case and the ports it needs
  adapter/
    internal/jsondoc            the strict JSON decoder adapters share
    source/inventoryfile        the JSON inventory file
    source/{aws,azure,gcp}      read-only discovery   (+ sourcetest contract suite)
    target/{ovhcloud,scaleway,upcloud}  EU catalogs   (+ targettest contract suite)
    report/{markdown,jsonreport,htmlreport}  renderers
  cli/                        Command-line parsing (driving adapter)
  web/                        The site's driving adapter
  config/                     Configuration loading
  version/                    Build metadata
  testutil/                   Test-only helpers (golden files, fixtures)
test/e2e/                     Black-box tests of the built binary
test/lint/                    Source rules golangci-lint cannot express
test/architecture/            Layer dependencies checked package by package, per release target
test/container/               The Dockerfile's image pinned to go.mod and .goreleaser.yaml
test/docs/                    Every relative link and anchor in the Markdown resolves (no network)
test/examples/                The sample inventories stay valid, sanitised and complete; their report goldens
testdata/                     Shared, sanitised fixtures
examples/                     Dummy sample inventories, one per source, to try shift2eu (docs/guide/try-it.md)
docs/                         User and contributor guides, architecture, ADRs (index: docs/README.md)
prds/                         Product requirements, one per feature
Dockerfile, .dockerignore     The container image (make image)
dev/lima.yaml                 Isolated VM for running the agents
dev/worktree.sh               Creates the worktree for a branch (make worktree)
.githooks/                    Refuse commits and pushes to main (make git-hooks)
.worktrees/                   One git worktree per pull request (git-ignored)
.dot-agent-deck.toml          Multi-agent orchestration
.claude/skills/               Skills used by the agents (Claude Code and pi)
.pi/settings.json             Points pi at .claude/skills
```
