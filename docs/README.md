# Documentation

The index of `shift2eu`'s documentation: every page, in reading order, grouped into the user guide,
the contributor guide, the architecture and the architecture decision records. The
[README](../README.md) introduces the project; start there if you are new to it.

## User guide

1. [Install](guide/install.md): the release archives, the images, and building from source.
2. [Try it](guide/try-it.md): a walkthrough from install to a report on a sample inventory, with the
   CLI, the image and the HTTP API, and how to read the report.
3. [Build an inventory from your cloud](guide/inventory.md): turn an AWS, GCP or Azure estate into
   an inventory file with the provider's read-only CLI and `jq`.
4. [Assess an inventory](guide/assess.md): the `assess` command, its Markdown and JSON reports, the
   comparison of several targets, and its exit codes.
5. [Run with Docker](guide/docker.md): running the image offline as your own user, mounts, reports
   and, for live sources, credentials.
6. [Verify a release](guide/verify-releases.md): the images' signatures, build provenance and SBOMs,
   and the releases published before the rename.
7. [Run the HTTP API](guide/http-api.md): `shift2eu-server`, its routes, its image and example
   requests.
8. [Behind a reverse proxy](guide/reverse-proxy.md): deploying the server, its flags and variables,
   and a minimal nginx configuration.

## Contributor guide

1. [Development](contributing/development.md): requirements, make targets and how a change reaches
   `main`.
2. [Repository layout](contributing/repository-layout.md): where everything lives.
3. [Working with agents](contributing/agents.md): the agents' roles and how to start them.
4. [Development environment](dev-environment.md): the Lima VM the agents run in, its credentials,
   tools and isolation.

## Architecture

[Architecture](architecture.md): the hexagon's layers, the taxonomy that joins sources and targets,
and the rules that hold them apart.

## Architecture decision records

Each lasting decision, in the order it was taken. An accepted record is never rewritten; a later one
supersedes it.

- [0001. Use a hexagonal architecture with a provider-neutral taxonomy](adr/0001-hexagonal-architecture-with-provider-neutral-taxonomy.md)
- [0002. Run the agent team in a Lima VM rather than devbox](adr/0002-lima-vm-for-agent-isolation.md)
- [0003. Run the tester role with pi on DeepSeek V4.1 Flash](adr/0003-pi-with-deepseek-for-the-tester-role.md)
- [0004. Build every pull request in its own git worktree under .worktrees/](adr/0004-one-worktree-per-pull-request.md)
- [0005. Declare three context-first ports in the use case and pin adapters with fstest-style contract suites](adr/0005-context-first-ports-and-fstest-style-contract-suites.md)
- [0006. Use a strictly decoded, versioned JSON inventory file as the interchange format between discover and assess](adr/0006-json-inventory-file-format-v1.md)
- [0007. Read every embedded target catalog snapshot through one strictly decoded, versioned format](adr/0007-shared-target-catalog-snapshot-format-v1.md)
- [0008. Mount a workspace directory so dot-agent-deck's dispatcher can run in the VM](adr/0008-mount-a-workspace-directory-for-dispatcher-mode.md)
- [0009. Decode adapter JSON documents through one strict decoder](adr/0009-decode-adapter-json-documents-through-one-strict-decoder.md)
- [0010. Create a dispatched unit's pull request worktrees inside its dispatch worktree](adr/0010-create-pull-request-worktrees-inside-the-dispatch-worktree.md)
- [0011. Scope high availability by level, give each tier its own offering, and define capabilities strictly](adr/0011-scope-capabilities-and-one-offering-per-tier.md)
- [0012. Check the layers' dependencies by package for every release target](adr/0012-check-layer-dependencies-by-package-for-every-release-target.md)
- [0013. Ship shift2eu as a distroless container image, built but not yet published](adr/0013-ship-shift2eu-as-a-distroless-container-image.md)
- [0014. Hold source adapters to read-only cloud access with probe-pinned lint guardrails](adr/0014-lint-guardrails-for-read-only-cloud-access.md)
- [0015. Publish the signed container image to GHCR from release jobs of their own, with docker buildx](adr/0015-publish-the-signed-image-to-ghcr-with-docker-buildx.md)
- [0016. Compare targets through a second reporter port and use case](adr/0016-compare-targets-through-a-second-reporter-port-and-use-case.md)
- [0017. Run a rootless container runtime (nerdctl-full) and cosign in the Lima VM](adr/0017-run-a-rootless-container-runtime-in-the-lima-vm.md)
- [0018. Version the JSON report format, shared by the CLI and the HTTP API](adr/0018-version-the-json-report-format-for-the-cli-and-the-api.md)
- [0019. Serve assessments over a versioned HTTP API from a separate binary](adr/0019-serve-assessments-over-a-versioned-http-api-from-a-separate-binary.md)
- [0020. Rename the project to shift2eu, keep published releases and history as they are](adr/0020-rename-the-project-to-shift2eu.md)
- [0021. Propose releases with release-please and tag them with a GitHub App](adr/0021-propose-releases-with-release-please-and-tag-with-a-github-app.md)
- [0022. Lint shell scripts with shellcheck and workflows with actionlint](adr/0022-lint-shell-scripts-and-workflows-with-shellcheck-and-actionlint.md)
- [0023. Separate bare-metal servers from virtual machines in the taxonomy](adr/0023-separate-bare-metal-servers-from-virtual-machines-in-the-taxonomy.md)
- [0024. Add plans, monthly EUR prices and a default region in target snapshot format version 2](adr/0024-add-plans-prices-and-a-default-region-in-target-snapshot-format-v2.md)
- [0025. Add inventory file format version 2 for sizing, usage, workloads, relations and cost](adr/0025-json-inventory-file-format-v2.md)
- [0026. Add an optional fixed monthly fee per offering to target snapshot format version 2](adr/0026-add-an-optional-fixed-monthly-fee-per-offering-to-snapshot-format-v2.md)
- [0027. Match plans and estimate monthly prices in a domain package, with exact money](adr/0027-match-plans-and-estimate-monthly-prices-in-a-domain-package.md)
- [0028. Group workloads, note blocked relations and sum the source's cost in the assessment](adr/0028-group-workloads-note-blocked-relations-and-sum-the-source-cost-in-the-assessment.md)
- [0029. Cap the billed hours of an offering's plans in target snapshot format version 2](adr/0029-cap-the-billed-hours-of-an-offerings-plans-in-target-snapshot-format-v2.md)

## Writing a page

The pages are written so that a static site generator can publish them later without rework:

- **One topic per file.** Each file starts with a single `#` heading, its title, then a one-paragraph
  summary that can serve as its description. No YAML front matter for now: GitHub renders it as a
  table. Architecture decision records keep their own structure (the title, then the status and date),
  set by the `adr` skill.
- **Relative links between files** (`../guide/install.md#the-images`), never
  `https://github.com/timothymamo/shift2eu/blob/...` or `.../tree/...` URLs, so that links survive a
  move to a site. Links to issues and pull requests are fine.
- **CommonMark, plus GitHub's tables and fenced code only.** No raw HTML, and no GitHub-only syntax
  (such as `> [!NOTE]` alerts) that a generator may not render. Use `#` headings, not underlined
  ones.
- **Images and other assets under `docs/assets/`.**
- **Stable headings.** Other pages link to their anchors; when a heading must change, update every
  link to it.
- **Every page is listed here,** in its group and in reading order.

`make check` holds the pages to part of this: `test/docs` fails on a relative link or `#anchor` in
`README.md`, `docs/` or `.claude/skills/` that names no existing file or heading, on a link to this
repository's files by URL, on a page without its single `#` title, and on a page under `docs/` that
this index does not list. It never fetches an external URL.

The release archives ship `README.md` with `docs/` beside it, so the README's links work in an
unpacked archive too. Links from these pages to the repository's source files (`api/openapi.json`, the
JSON Schemas) resolve in the repository, not in an archive.
