# 0030. Serve the version 2 report on /v1, with estimates, relations and workloads

- Status: Accepted (amends [ADR 0019](0019-serve-assessments-over-a-versioned-http-api-from-a-separate-binary.md), Versioning and the no-echo rule for a region; builds on [ADR 0018](0018-version-the-json-report-format-for-the-cli-and-the-api.md))
- Date: 2026-11-14

## Context

PRD #190 milestone 8 puts what milestones 6 and 7 compute into the reports: each resource's plan and
monthly estimate, its relations and its "affected by a blocker" notes, the per-workload section, the
cost summary with its basis and region, and the source's own cost. The JSON report gains keys a
consumer of `schemaVersion` 1 would find in no schema, and keys that are written only when estimates
are on, so the PRD makes it JSON report `schemaVersion` 2, with new schema files beside the version 1
files as ADR 0018 asks.

ADR 0019 says "a new report `schemaVersion` is a breaking change of the report, and so of the route that
serves it", which read as written would put the version 2 report on a `/v2/` route. The PRD's acceptance
criteria and the maintainer's decision (2026-11-13) keep it on `/v1`: there is one release of the API so
far, no client depends on it beyond this project, and a `/v2/` route that differs from `/v1/` only in the
report's version would double every route, test and OpenAPI entry for nothing a client gains.

The renderers also need to know whether estimates are on, which region the prices are for and whether
the user chose it. Milestones 6 and 7 already put all of it on `assessment.Assessment`.

## Decision

`/v1/assessments` serves the JSON report `schemaVersion` 2, for inventories of version 1 and 2 alike.
The document's `schemaVersion` tells a client which version it holds; the request fields milestone 8
adds (`regions`, `estimate`) are optional and change nothing for a client that leaves them out, except
that estimates are on by default. `api/openapi.json` refers to `assessment-v2.schema.json` and
`comparison-v2.schema.json`. The version 1 schema files stay in the repository unchanged, for documents
an earlier release wrote. ADR 0019's rule stands for every later version: the next report version is a
new route.

The report itself:

- **Estimates off writes no price.** The JSON document has no `estimate`, `sourceCost`, resource `plan`
  or resource `estimate` key at all, rather than an `estimates: false` flag: a key that is absent cannot
  be misread as a zero, and a consumer tests for the `estimate` key. The Markdown report has no Plan or
  Monthly estimate column, no Cost estimate section, and no region line unless a region was chosen, so a
  report of a version 1 inventory without estimates is byte for byte the report before milestone 8.
  The source cost is computed whatever the switch (ADR 0028) but shown only with estimates on.
- **Relations, "affected by" notes and workloads are not prices.** The JSON document always writes a
  resource's `relations` and `affectedBy` (`[]` when empty) and the `workloads` array, whatever the
  switch; a workload's `estimate` and `sourceCost` are written only with estimates on. The Markdown
  report adds the Relations column only when some resource has a relation, and the Workloads section
  only when `Assessment.ShowWorkloads` (some resource names a workload), so neither appears for a
  version 1 inventory.
- **The renderers read the switch and the region from the domain, which gains nothing.** Estimates are
  on exactly when `Assessment.Total` is set; the region the prices are for is `EstimateRegion`; it was
  chosen exactly when `Region` is set. The JSON basis writes them as `region` and `regionChosen`, the
  Markdown header as "(chosen with `--region`)" or "(the target's default for estimates)".
  A chosen region is also a top-level `region` key of the document (and of each comparison target),
  with or without estimates, so a report with estimates off still says which region availability was
  scoped to; with no region chosen the key is absent.
- **Money is a decimal string rounded to the cent.** The domain keeps every amount exact; each amount
  in the report is `estimate.Money.String()`, rounded half away from zero to two fractional digits,
  never a JSON number, so no consumer meets a binary float. A total (`total.amount`, a workload's
  estimate, the source cost) is rounded from the exact sum, not summed from the rounded rows, so a
  consumer that adds up the per-resource amounts can be a cent or so off the total. Estimates are in EUR (ADR 0024); the
  source cost is summed per currency and never converted (ADR 0028). The Markdown report adds a
  thousands separator and the sign of EUR, USD and GBP; any other currency is shown by its code alone.
- **An estimate says what it assumed.** An estimated resource's JSON `estimate` always carries
  `countAssumed` and `hoursAssumed` (a count of 1, 730 hours a month, taken because the resource gives
  none); a resource that is not estimated carries neither. The Markdown estimate cell adds
  "(count assumed 1)" and "(730 hours assumed)" after the fixed-fee note, so a reader sees which rows
  rest on an assumption.
- **The basis is stated next to every total**: list prices on the snapshot's retrieval date (never an
  age), excluding VAT and discounts, the hours basis "plan price × count × hours / 730", and a
  fixed fee as the offering's listed fee. A plan whose offering caps its billed hours (ADR 0029) has
  its cap stated: the JSON `estimate` carries `billingCapHours`, and the Markdown hours line adds, per
  distinct cap, that such a plan costs plan price × count × min(hours, cap) / cap. A fee recorded as the smallest paid size of a tier, such as
  Scaleway Kubernetes Kapsule Dedicated (its catalog's `SOURCES.md`), is a lower bound; the domain does
  not mark which fees are, so the JSON Schema's `fixedFee` description says so and no new price rule is
  invented.
- **The comparison ranks nothing.** It shows one estimate row per target in the summary, one monthly
  estimate column per target in the resources table and each target's own cost summary in its own
  section, with each target's own region and retrieval date.

The request options and their errors:

- **`regions` and `estimate` are decoded strictly and checked like the CLI's flags.** `regions` is an
  object of strings (at most 64 members, no key twice, no region empty: an empty one would silently mean
  every region, which the CLI's `--region` refuses too) and `estimate` a boolean, true when absent; null
  and any other JSON type are 400 `invalid_request`, named by JSON type only. `jsondoc` gains
  `Walker.Bool` and `Walker.Members`, which reads an object whose keys are data: its errors name a member
  by its index (`regions[0]`), never by its key, so the decoder still echoes nothing. In
  `api/openapi.json`, `regions` is a closed object with one string property per target key, each
  `minLength: 1` (the
  repository's schema validator takes only a boolean `additionalProperties`).
- **A bad region is 400 `invalid_region`**, a new code beside `invalid_targets`, in the existing
  envelope. A `regions` key the request does not select is refused before anything is built; a region
  the target does not list is the use case's `*analysis.RegionError`, found once the catalog is loaded
  and before the inventory is read, so nothing is assessed. The message names the target and the
  target's regions, which are the API's own values.
- **The one value echoed (amends ADR 0019's "no value from a request is ever echoed").** The PRD asks
  for the bad element to be named, so the message names the region the client sent, but only when it is
  region-shaped: 1 to 64 ASCII letters, digits, `-`, `_` or `.`. Anything else is "the region", unnamed.
  Such a value can hold no markup, control character or long text, so the message stays safe to show
  and to store. A `regions` key is named only when it is a supported target key; any other is "a target
  that is not supported". The CLI names whatever was typed (through its `sanitise`), as it always has.

## Alternatives considered

- **A `/v2/assessments` route.** What ADR 0019 asked for. It lost on the maintainer's decision: it
  doubles the API surface for a project with no outside client yet, and `schemaVersion` already tells a
  client which document it has.
- **Keep `schemaVersion` 1 and add the keys.** ADR 0018 allows additive keys, but some of the new keys
  appear only with estimates on, and the version 1 schemas are closed; a consumer validating against
  them would refuse every new document. The PRD asks for version 2.
- **An `estimates: false` flag with empty estimate objects.** It lost: empty amounts are one step from
  being read as zero, and the absent key is the one thing a consumer cannot misread.
- **New fields on the assessment for the switch and the region.** Not needed: `Total`, `Region` and
  `EstimateRegion` already say all of it, and a second field could disagree with them.

## Consequences

- A client of `/v1/assessments` gets a `schemaVersion` 2 document after this release and must read
  `schemaVersion`; the CLI's `--format json` writes the same document. The release notes say so.
- The tests that pinned "a version 2 inventory changes no report" (PRD #190 milestone 1) no longer
  hold: a version 2 inventory's relations and workloads now reach the report.
- The next change of the report's version is a new route, as ADR 0019 says.
