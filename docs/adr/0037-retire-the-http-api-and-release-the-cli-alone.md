# 0037. Retire the HTTP API and release the CLI alone

- Status: Accepted
- Date: 2026-11-20
- Supersedes: [ADR 0019](0019-serve-assessments-over-a-versioned-http-api-from-a-separate-binary.md)

## Context

[ADR 0019](0019-serve-assessments-over-a-versioned-http-api-from-a-separate-binary.md) (PRD #103) added a
second binary, `shift2eu-server`, which serves the assessment over a versioned HTTP API. Its purpose was to be
the backend of a website: a client sends an inventory and gets the assessment back. Every release has shipped it
as archives of its own, as the signed image `ghcr.io/timothymamo/shift2eu-server` and with the bundled OpenAPI
document `shift2eu-server_openapi.json` (issue #139).

PRD #297 then built the website without a backend. The WebAssembly module of
[ADR 0035](0035-run-the-assessment-in-the-browser-as-a-webassembly-driving-adapter.md) runs the same
assessment in the browser, and the pages of [ADR 0036](0036-hold-the-site-to-three-trust-layers-and-test-them-in-a-headless-browser.md)
load it under a strict Content Security Policy. Nothing needs to be hosted and no inventory leaves the user's
machine, which is a stronger promise than an API could make. The CLI covers scripts and offline use.

The API still costs what it did: a binary, its image, SBOM, signature and attestations, the OpenAPI document,
three release jobs and a verify step, the CI smoke tests, the parity tests, and an authenticated network
service whose attack surface every audit has to cover. Nothing in the project still depends on it.

The maintainer decided on 2026-11-20 to stop publishing and maintaining it (issue #393).

## Decision

Retire `shift2eu-server`. The CLI and the site are the two ways to run an assessment. The release ships the
CLI alone: one GoReleaser build and archive, `checksums.txt` with no extra files, the CLI's image and
nothing else. `test/architecture`'s release checks and `test/container` require that shape and refuse any
mention of `shift2eu-server` or the OpenAPI document in the release configuration.

It happens in two pull requests, both under issue #393:

1. **Stop publishing** (this ADR's change): `.goreleaser.yaml`, `release.yml`, the `Dockerfile`, CI's image
   dry run, the archive dry run (`.github/scripts/verify-archives.sh`), the `release-tag` skill and the
   release guides. The server's code stays in the module for now and is still built and tested by
   `make check` and `make test-e2e`, but no release builds it.
2. **Remove the code and its docs**: `cmd/shift2eu-server`, `internal/httpapi`,
   `internal/adapter/request/assessmentrequest`, `api/`, `test/apiparity`, the depguard rules and probes
   that exist only for them, `make server`, and the API's guide pages.

The JSON report ([ADR 0018](0018-version-the-json-report-format-for-the-cli-and-the-api.md)) stays: the
CLI writes it and the site offers it as a download. `internal/adapter/internal/jsondoc` stays: the inventory
reader uses it.

## Alternatives considered

- **Keep the API as a supported product.** It would keep a use case the site already covers better, at the
  cost of a hosted service users would have to run and secure themselves, and of every release job and audit
  it needs.
- **Keep the code but stop releasing it, indefinitely.** Unreleased code still has to pass every check and
  keeps its rules in the architecture tests, so it costs nearly as much as a released binary while nobody can
  use it. It is acceptable only for the short time between the two pull requests.
- **Remove everything in one pull request.** Simpler history, but a diff too large to review well. Stopping
  the publishing first also makes the first release without the server independent of the code removal.

## Consequences

- 0.10.1 and the pending 0.10.2 still ship `shift2eu-server`. The first release after this change does not:
  no server archives, no `shift2eu-server_openapi.json` and no new server image. Its release notes say so
  (the change is a breaking `chore!` commit) and point to the site and the CLI.
- The images `ghcr.io/timothymamo/shift2eu-server` up to the last release that shipped them stay available,
  signed and attested as they were, but get no further updates, security fixes included. Archiving or
  deleting the GHCR package and its tags is the maintainer's manual step.
- ADR 0019 is superseded. ADR 0018 keeps the JSON report for the CLI and the site.
- `release.yml` has ten jobs instead of thirteen, its `verify` job is "Verify the published image", and CI's
  image job builds and smoke-tests the CLI's image only.
- Until the follow-up pull request lands, the server's code, its tests and its docs remain in the
  repository; its docs describe a binary the release no longer ships.
