# PRD #190: Sizing, cost estimates and workloads

**Status:** Draft
**Issue:** #190 (takes in #186)

## Problem

`shift2eu assess` answers "does this target offer something for each resource?" It doesn't answer the
two questions that come right after it in every migration conversation:

1. **What will it cost?** A platform engineer or a finance owner who sees a `direct` fit still can't
   tell whether moving is cheaper or dearer. The inventory has no size (vCPU, memory, storage, node
   count), and the target catalogs have no plans or prices. So the tool can't pick a plan, let alone
   price one. Every PRD so far (#2, #11, #14, #89) has left sizing, plan matching and cost out of scope.
2. **What does this mean for my application?** An inventory is a flat list. It can't say that a
   database is a read replica of another, that a queue is another queue's dead-letter queue, or that a
   group of resources is one workload, for example "shop". The assessment therefore reports per
   resource only, never per workload. Yet "can the shop move, and what does it cost there?" is the
   question people bring (issue #186, raised while building the sample inventories in #179).

Both need new inventory fields, so both need a new inventory format version (ADR 0006: any new field is
a new version). Doing them together changes the format once instead of twice, before
`shift2eu discover` exists and starts writing files in the wild.

## Outcome

A user adds sizes, an optional usage profile, workloads and relations to an inventory (schema version
2), and `shift2eu assess` reports, per target:

- for each resource, the **plan** it would run on (the smallest plan that covers its size) and an
  **estimated monthly price** from the target's dated, embedded price list;
- per **workload** and in total, the fits, the blockers and the estimated monthly price. Where the
  inventory carries the source's own monthly cost, it is shown next to the estimate.

The comparison report (`--target all`) shows the estimates side by side. It still ranks nothing.

Every estimate is labelled as an estimate: list prices on the snapshot's retrieval date, excluding VAT,
with no discounts. A resource that can't be priced (no size, no matching plan, or a usage-priced
category) says so and why. It never counts as zero. A version 1 inventory still works and produces
today's report plus "size unknown" notes. The tool stays offline and needs no credentials.

Estimates are on by default and `--no-estimate` turns them off: the report then has fits, workloads and
relations, but no plans or prices. Independently of prices, a user can name one region per target
(`--region scaleway=fr-par`). The assessment then counts only the offerings available in that region,
so running the same inventory against two regions of one target shows which resources each region can
serve. Without `--region`, availability is judged across all of a target's regions, as today, and
estimates use the target's default region.

## Scope

| | In scope | Out of scope |
|---|---|---|
| Source providers | None live. The inventory file, schema versions 1 and 2. Docs (#178's guide) on how to read sizes from AWS, GCP and Azure | Live discovery (`shift2eu discover`). Reading the source's billing data automatically |
| Target providers | OVHcloud, Scaleway and UpCloud: plans and list prices added to their embedded snapshots | Live pricing APIs. New targets. Committed-use, reserved or negotiated prices |
| Taxonomy categories | Priced: `compute.vm`, `compute.baremetal` (servers by monthly plan; ADR 0023), `kubernetes.managed` (control plane and nodes), `storage.block`, `storage.object` (stored GiB), `database.postgresql`, `database.mysql` | Priced by usage, so shown as "not estimated" with the reason: `serverless.function`, `messaging.queue`. `unclassified` is never priced. Network egress, IP addresses, load balancers, support plans |
| Inventory | Schema version 2: per resource `size`, `usage`, `workload`, `relations` and the source's `monthlyCost`. Version 1 still read | Account or tenant fields. Tags |
| Assessment | Plan matching, a monthly estimate per resource (on by default, `--no-estimate` to turn it off), workload grouping, relations in the report. Availability in one target region the user names (`--region`) | A readiness score, ranking or recommended target. Changing a resource's fit because of its size (see *Decisions*). Choosing a region for the user, such as the nearest one or the cheapest |
| Output | Markdown and JSON reports (JSON report schema version 2), single target and comparison. The HTTP API takes version 2 inventories and returns the new report | HTML. Currency conversion. Charts |

## Acceptance criteria

All criteria are pinned by tests that run offline under `make test` or `make test-e2e`, unless marked
*review*.

**Inventory format version 2**

- [x] An inventory with `"schemaVersion": 2` may give each resource:
  - `size`: `vcpus`, `memoryGiB`, `storageGiB` and `count` (instances or nodes), each optional;
  - `usage`: `hoursPerMonth` (default 730);
  - `workload`: a free-text name;
  - `relations`: a list of `{"type", "target"}`, where `type` comes from a closed vocabulary: `replica-of`,
    `dead-letter-queue-of`, `attached-to` and `depends-on`, and `target` is another resource's `id`;
  - `monthlyCost`: `{"amount", "currency"}`.

  It's read with the same strict rules and caps as version 1.
- [x] A version 1 inventory gives the same verdicts as today. Its resources get no plan and no estimate,
  and the report says that the sizes are unknown.
- [x] Given a relation whose `target` names no resource, a resource that relates to itself, an unknown
  relation `type`, a negative or non-numeric size, or a `size` field that makes no sense for the
  category (for example `vcpus` on `storage.object`), the run exits 1 with one error line that names
  the resource and the field by path (`resources[3].relations[0].target`). Nothing is written.
- [x] *Review:* an ADR records format version 2 and how it relates to ADR 0006 (supersedes or amends):
  [ADR 0025](../docs/adr/0025-json-inventory-file-format-v2.md) amends it.

**Catalog plans and prices**

- [ ] Each target's snapshot lists, per offering, the plans that the priced categories need:
  - name, vCPU, memory and included storage;
  - a monthly list price in EUR excluding VAT, and the regions it applies to where prices differ;
  - per-GiB monthly prices for block storage, object storage and database storage.

  Each snapshot also carries the date the prices were retrieved, their source URL and the target's
  default region for estimates, which must be one of its regions. Its `SOURCES.md` cites every price.
- [ ] *Review:* an ADR records the snapshot format version that adds plans and prices (amends ADR 0007),
  including the rounding. Prices are stated as monthly prices only; a target that publishes hourly
  prices has them converted at 730 hours, and `SOURCES.md` says so.
- [x] The snapshot reader refuses, with the existing strict errors, a plan without a price, a negative
  price, a currency other than EUR, or two plans of one offering with the same name.

**Plan matching and estimates**

- [x] Given a sized resource with a fit other than `none`, the report names the plan on the best
  offering: the cheapest plan whose vCPU and memory are each at least the resource's. Ties are broken by
  fewer vCPU, then less memory, then name.
- [x] Given a size that no plan covers, the resource shows "no plan large enough" with the largest plan's
  size, and gets no estimate. Its fit is unchanged.
- [ ] The monthly estimate of a resource is plan price × `count` × `hoursPerMonth` / 730, plus storage
  above what the plan includes × the per-GiB price. It's rounded to the cent only for display. A test
  table pins one example per priced category on each target.
- [x] A resource with no size, no fit, a usage-priced category or no price for its region shows "not
  estimated" with that reason. Totals sum only the estimated resources and say how many weren't
  estimated ("€1,234.56 per month for 9 of 13 resources").
- [x] The report states, next to every total, that it is an estimate: list prices on the snapshot's
  retrieval date, excluding VAT and discounts. It also names, per target, the region the prices are for.

**Regions and the estimate switch**

- [x] `--region` takes one `<target>=<region>` pair per target, comma-separated
  (`--region scaleway=fr-par,ovhcloud=GRA`). A region the target doesn't list, a target that isn't
  selected, or a target named twice is a usage error: exit 2, naming the bad element and listing that
  target's regions. Nothing is read.
- [x] Given `--region` for a target, an offering not available in that region is not a candidate. A
  resource whose only offerings are elsewhere gets fit `none`, with the reason "not offered in
  `<region>`". A resource with an offering in the region keeps the fit it gets from that offering's
  capabilities. The report header names the region per target.
- [x] Without `--region`, fits are exactly as today: availability is judged across all of a target's
  regions. A test pins that the existing goldens, other than the new plan and estimate columns, don't
  change.
- [x] Estimates use the region named with `--region`, or else the target's default region from its
  snapshot. A plan without a price in that region is "not estimated" with that reason.
- [x] Given the same inventory and target with two different `--region` values, the two reports differ
  only in the region names and in the resources whose offerings differ between the regions. Pinned
  with an invented catalog in which one offering exists in one region only.
- [x] With `--no-estimate`, the report has no plan or estimate column and no cost summary, and says
  nothing about prices. Fits, workloads and relations are unchanged. A version 2 inventory with sizes
  then gives the same fits as the same inventory with its sizes removed.

**Workloads and relations**

- [x] Resources with the same `workload` are reported together. Each workload gets its fit counts, its
  blockers and its estimate. Resources without a workload are grouped under "No workload". A version 1
  inventory has only that group, and the per-workload section is left out.
- [x] Each resource's row lists its relations. A blocker on a resource is also listed under every
  resource that `depends-on` it, or that is a `replica-of` it, as "affected by a blocker on `<id>`". No
  fit changes.
- [x] Given a `monthlyCost` on resources, each workload and the total show the source's cost next to the
  estimate, per currency, with no conversion. Where some resources have no `monthlyCost`, the total says
  how many it covers.

**Reports, comparison and API**

- [ ] The Markdown report adds a plan and an estimate column to the resources table, a "Workloads"
  section, and a cost summary. Golden files pin a single-target and a comparison report for each sample
  inventory under `examples/` (updated to version 2 with sizes, workloads and relations).
- [x] The comparison report adds one estimate column per target in its summary and resources tables,
  and the per-workload estimates per target. It contains no ranking, "cheapest" or recommendation: the
  "no ranking" test from PRD #89 gains "cheapest" and "best price".
- [ ] The JSON report moves to schema version 2 (ADR 0018) with the plan, the estimate, its basis
  (price date, currency, excluded items), the workloads and the relations. Its JSON Schemas are updated
  and the examples validate against them.
- [x] `POST /v1/assessments` accepts version 1 and version 2 inventories and returns the version 2
  report. Its request takes the same choices as the CLI: optional `regions` (an object from target key to
  region) and `estimate` (default `true`), with the same validation and errors. The path stays `/v1`:
  the report's `schemaVersion` says which version a response is, and the new request fields are
  optional. *Review:* if ADR 0019's versioning rules don't allow that, an ADR records why and what
  changes.
- [x] Given the same inventory twice, the reports are byte-identical. Nothing depends on the current
  date, so a snapshot's age is shown as its retrieval date, never "N days old".

**Architecture**

- [ ] `make check` passes, including depguard and `test/architecture`. Plan matching and estimating are
  domain logic (`internal/domain`). Prices enter only through the catalog port. No source-service to
  target-service price table exists anywhere.
- [ ] Default tests use invented plans and prices (`internal/testutil`). Real prices appear only in the
  embedded snapshots and the goldens built from them.

## Milestones

Each milestone ships as one PR, in dependency order. Milestones 3, 4 and 5 can run in parallel.

1. [x] **Inventory format version 2.** Domain: size, usage, workload, relations (closed vocabulary) and
   source cost on `inventory.Resource`, validated by `inventory.New`. The inventory file reader accepts
   versions 1 and 2. ADR for the format. The sourcetest contract suite covers the new fields. No report
   change.
2. [x] **Catalog plans and prices.** Domain: plans and prices on `catalog.Offering`, validated by
   `catalog.New`, and the target's default region for estimates. The snapshot format version that adds
   them, with the shared reader, the targettest contract suite and an ADR
   ([0024](../docs/adr/0024-add-plans-prices-and-a-default-region-in-target-snapshot-format-v2.md)). The existing snapshots move
   to the new version with no plans yet.
3. [x] **OVHcloud plans and prices** (monthly) and its default region, in its snapshot and
   `SOURCES.md`. Depends on 2.
4. [x] **Scaleway plans and prices** (monthly) and its default region, in its snapshot and
   `SOURCES.md`. Paid Kubernetes control-plane fees are recorded as the offering's `fixedFees`
   (ADR [0026](../docs/adr/0026-add-an-optional-fixed-monthly-fee-per-offering-to-snapshot-format-v2.md)), not left out. Depends on 2.
5. [x] **UpCloud plans and prices** (monthly) and its default region, in its snapshot and `SOURCES.md`.
   Paid Kubernetes control-plane fees are recorded as the offering's `fixedFees` (ADR 0026), not left
   out. Depends on 2.
6. [x] **Plan matching, estimates and regions.** Domain: the plan per verdict, the monthly estimate, the
   "not estimated" reasons and the totals, per assessment and in the comparison. Availability in a
   chosen region, with the "not offered in `<region>`" reason, and the estimate's region (the chosen one
   or the default). Estimates can be switched off. The application layer passes both choices through;
   the CLI flags come in milestone 8. The estimate adds the offering's fixed fee,
   `Offering.FixedFee(region)` in the estimate region, once per resource (one cluster), never multiplied
   by its `count` (its nodes), and scaled by hours / 730 as plan prices are; a fee alone never makes an
   estimate: a resource with no plan price for the region stays "not estimated" (ADR 0026,
   *Estimates*). Design and API: ADR
   [0027](../docs/adr/0027-match-plans-and-estimate-monthly-prices-in-a-domain-package.md). Depends on 1
   and 2.
7. [x] **Workloads and relations in the assessment.** Domain: per-workload summaries and estimates, and
   the "affected by a blocker" notes. `Assessment.Workloads` groups resources by workload, and
   `ShowWorkloads()` is false when no resource names one; a blocker is noted on the resources with a direct
   `depends-on` or `replica-of` relation to the blocked one, never transitively; and
   `estimate.SumSourceCost` sums the source's `monthlyCost` per currency, which milestone 8 shows only
   when estimates are on. Design and API: ADR
   [0028](../docs/adr/0028-group-workloads-note-blocked-relations-and-sum-the-source-cost-in-the-assessment.md).
   Depends on 6.
8. [x] **Markdown and JSON reports, CLI and API options.** Both renderers, the comparison, JSON report
   schema version 2 and its Schemas. The CLI's `--region` and `--no-estimate`, and the HTTP API's
   `regions` and `estimate` request fields, on `/v1`. Depends on 7 and on 3, 4 and 5 for the real-price
   goldens.
9. [ ] **Examples, docs and e2e.** `examples/` move to version 2 with sizes, workloads, relations and
   some `monthlyCost`, and `docs/guide/try-it.md`, `assess.md`, `http-api.md` and the inventory guide
   (#178) cover the new fields and how to read sizes from each source's CLI. E2e goldens. README and
   `CLAUDE.md` status. Moves this PRD to `prds/done/`, closes #190 and #186. Depends on 8.

## Decisions and open questions

- **Proposed: one PRD and one inventory format version for sizing, cost, workloads and relations.** Each
  of them needs new resource fields, and each format version is a reader, an ADR and a migration of the
  samples. Doing them together costs one version instead of two. #186 is taken in here.
- **Proposed: sizing doesn't change the fit.** Fit stays "does the target offer this category with these
  capabilities". "No plan large enough" is reported next to the fit. Folding it into the fit would change
  verdicts for every existing user, and a too-large VM is often split or re-architected rather than
  blocked. *Open:* revisit once users have seen both.
- **Proposed: list prices only, EUR, excluding VAT, monthly.** All three targets publish EUR list
  prices. VAT depends on the buyer, and discounts on the contract. The report says all of this next to
  every total, so an estimate is never mistaken for a quote.
- **Proposed: never zero for unknown.** A resource that can't be priced is counted and named, never
  added as €0, so a total can't look cheaper than it is. That's the same principle as ADR 0006's "a
  silently ignored mistake is worse than a refused file".
- **Proposed: usage-priced categories are not estimated in this PRD.** Functions and queues are priced
  per request or per message. An honest estimate needs usage fields that sources expose unevenly.
  *Open:* a follow-up PRD with `usage.requestsPerMonth` and similar fields.
- **Proposed: the cheapest covering plan.** It's the usual right-sizing rule, and it's deterministic. It
  can pick a plan with more memory than a slightly dearer one with an exact fit. The tie-break order is
  fixed in the acceptance criteria. *Open:* whether to honour a CPU family (general, compute- or
  memory-optimised). This needs a vocabulary and stays out unless reviews ask for it.
- **Proposed: relations add notes, not verdicts.** "Affected by a blocker on `<id>`" makes a hidden
  dependency visible without inventing a rule for when a dependency blocks a move. *Open:* a workload
  readiness verdict, once there's agreement on what it means (PRD #2's readiness score).
- **Proposed: the source's own cost comes from the inventory, never from the tool.** The tool doesn't
  read billing APIs or know hyperscaler prices. A user who wants a comparison exports their bill and
  sets `monthlyCost`. That keeps the tool offline and never wrong about the source's discounts.
- **Accepted risk: prices go stale.** Prices are dated and cited, and the report shows the retrieval
  date. Updating them is a snapshot change with its `SOURCES.md`. *Open:* a scheduled check that flags
  snapshots older than a set age, which needs a workflow, not the binary (tests may not depend on time).
- **Decided (maintainer, 2026-11-13): one region per target, chosen with `--region`, with a default.**
  Estimates use the named region, or else a default region recorded in each target's snapshot. Listing
  every region's price was rejected: it multiplies the report for little gain. Picking a region for the
  user (the nearest or the cheapest) was rejected: the tool doesn't know where the user's users are.
- **Decided (maintainer, 2026-11-13): `--region` also scopes availability, independently of prices.**
  Today the mapping never compares regions: an offering in any region counts. Naming a region lets a
  user run the same inventory against two regions of one target and see which resources each can serve,
  with or without prices. Without `--region`, fits stay exactly as today.
- **Decided (maintainer, 2026-11-13): estimates are on by default, and `--no-estimate` turns them off.**
  Opt-in (`--estimate`) was the alternative: safer for scripts, but it would hide the main new feature.
  An inventory without sizes gets no plans or prices anyway, so nothing changes for those users.
- **Decided (maintainer, 2026-11-13): monthly prices only.** Snapshots hold monthly list prices. A target
  that publishes hourly prices has them converted at 730 hours, with `SOURCES.md` saying so.
- **Decided (maintainer, 2026-11-13): keep `/v1` for the HTTP API if ADR 0019 allows it.** The report's
  `schemaVersion` already says which version a response is, and the new request fields are optional.
  Milestone 8 checks ADR 0019 and records an ADR only if `/v1` can't be kept.
