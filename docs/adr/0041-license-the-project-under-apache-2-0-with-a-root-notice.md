# 0041. License the project under Apache-2.0 with a root NOTICE

- Status: Accepted
- Date: 2026-11-23

## Context

Issue #493. `shift2eu` has been licensed under the MIT License. The maintainer wants anyone to keep
being able to use, fork and modify it, and wants anyone who redistributes it, or a work based on it, to
credit this repository. MIT asks only that its copyright line and licence text stay inside copies; it
asks for no attribution a user of a redistribution would see, and it grants no patent rights in so many
words.

All commits so far come from the maintainer and the project's own bots, so the maintainer holds every
right needed to relicense, and no other contributor has to agree.

## Decision

**From the next release on, `shift2eu` is licensed under the Apache License 2.0, with a short `NOTICE`
file at the repository root that every release archive ships beside `LICENSE`.**

- `LICENSE` holds the unmodified text of https://www.apache.org/licenses/LICENSE-2.0.txt, with no
  copyright line edited into its appendix, so GitHub detects the licence as Apache-2.0.
- `NOTICE` holds the attribution in the form Apache recommends: the project's name, its copyright line
  and that the product includes software developed as `shift2eu`, with the repository's URL. Section
  4(d) requires every redistribution of the work or a derivative that ships a `NOTICE` to carry its
  attributions.
- The release archives ship `NOTICE` beside `LICENSE` (`.goreleaser.yaml`, held by
  `test/architecture`'s `check.GoReleaserArchives` and by `.github/scripts/verify-archives.sh`). The
  container image carries neither file; its `org.opencontainers.image.licenses` label says `Apache-2.0`.
  `test/architecture`'s `TestTheProjectIsApacheLicensed` pins `NOTICE`, the licence header and the label.
- Releases already published stay under MIT, the licence they shipped with.

## Alternatives considered

- **Keep MIT.** Permissive, but no attribution a redistribution must carry beyond the licence text
  itself, and no explicit patent grant.
- **AGPL-3.0.** Its copyleft, which reaches use over a network, would make companies wary of running,
  embedding or forking the tool, which is the opposite of what the maintainer wants.
- **CPAL-1.0.** Its attribution requirement holds for network use and shows the attribution to users,
  which is heavier than a `NOTICE` and rarely accepted by companies' licence policies; it would hurt
  adoption the same way.

## Consequences

- A redistribution of `shift2eu`, or a work based on it, must carry `NOTICE`'s attribution, and users get
  an explicit patent grant from contributors (section 3), which ends for anyone who sues over the work's
  patents.
- Anyone can still use, fork and modify the project, as under MIT.
- Every artefact that ships `LICENSE` must ship `NOTICE` too; a new artefact that ships the licence adds
  both, and its test holds both.
- Per-file licence headers are not added; the root `LICENSE` and `NOTICE` cover the repository.
- The catalog snapshots' sources keep their own licences (`internal/adapter/target/*/SOURCES.md`); this
  decision does not touch them.
