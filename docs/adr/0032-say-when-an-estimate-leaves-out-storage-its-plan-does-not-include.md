# 0032. Say when an estimate leaves out storage its plan does not include

- Status: Accepted
- Date: 2026-11-18

## Context

Issue #226. An estimate priced by plan ([ADR 0027](0027-match-plans-and-estimate-monthly-prices-in-a-domain-package.md))
is the plan price, plus the storage the resource gives above what the plan includes × the offering's
per-GiB storage price, plus the offering's fixed fee. A resource that gives no `storageGiB` is read as
needing no storage beyond the plan. That is right for a plan that includes a disk, and wrong for one
that includes none: OVHcloud's Gen 3 instances (`b3-*`, `c3-*`, `r3-*`, ..., `storageGiB` 0 in the
snapshot since #219, PR #225) boot from a Block Storage volume billed per GiB, so an estimate for such a
VM with no disk size is the plan price alone, too low, and nothing in the report says so. The OVHcloud
`SOURCES.md` already records this gap. The maintainer decided on 2026-11-14 not to charge a guessed
default boot volume: the report should say what is missing, not invent a size.

The same shape exists in the other embedded snapshots today. Plans priced by plan whose
`storageGiB` is 0 on an offering with a storage price, per target:

| Target | Offerings | Category | What the target's `SOURCES.md` says |
|---|---|---|---|
| OVHcloud | Instances (23 of 28 plans), Managed Kubernetes Service (23 of 28), Managed Kubernetes Service 3-AZ (23 of 23) | `compute.vm`, `kubernetes.managed` | A Gen 3 instance's disk is all Block Storage |
| Scaleway | Instances (18 of 18), Kubernetes Kapsule and Kapsule Dedicated (18 of 18 each) | `compute.vm`, `kubernetes.managed` | "an Instance's root and data volumes are Block Storage volumes, billed separately"; "a Kapsule node's root volume ... Block Storage" |
| Scaleway | Managed Database for PostgreSQL and MySQL, and its High Availability tier (12 of 12 each) | `database.postgresql`, `database.mysql` | Database storage is priced per GB and month, apart from the node |
| UpCloud | Cloud Servers, the Cloud Native plans (42 of 75) | `compute.vm` | Cloud Native includes no storage, "whose storage is an add-on" |
| UpCloud | Managed MySQL, MySQL Multi-node, PostgreSQL, PostgreSQL Multi-node (19 of 19 each) | `database.postgresql`, `database.mysql` | Database storage is priced per GiB, apart from the plan |

No offering whose plans include storage has a plan with `storageGiB` 0, and no offering with
zero-storage plans lacks a storage price, in any snapshot. Every row above is the same gap: the estimate
would charge the resource's whole storage at the per-GiB price if the inventory gave a size, and charges
nothing when it does not. For servers and Kubernetes nodes the missing storage is a boot (root) volume;
for a managed database it is the service's data storage.

Forces:

- The rule decides what an estimate means, so it belongs in the domain (`internal/domain/estimate`),
  never in a renderer; the Markdown and JSON reports, single target and comparison, and the HTTP API
  must say the same thing.
- A rule must not be wrong for a future target. The less a snapshot author has to remember, the less
  can drift.
- The JSON report is versioned ([ADR 0018](0018-version-the-json-report-format-for-the-cli-and-the-api.md),
  version 2 since [ADR 0030](0030-serve-the-version-2-report-on-v1.md)): adding an optional key keeps
  the version, any change that can break a reader is a new one.
- The issue's acceptance: goldens change only where the note appears.

## Decision

**Derive the note in the domain from what the estimate already prices; add no catalog field.** An
estimate by plan carries a note that storage is not included exactly when all of these hold:

1. the resource is estimated (`Estimated` true; the note is never set on a resource that is not
   estimated);
2. the chosen plan's `IncludedStorageGiB` is 0;
3. the chosen plan's offering has a storage price in the estimate region
   (`catalog.Offering.StoragePrice(region)` returns true);
4. the resource gives no storage size (`Size.StorageGiB` is nil). A resource that gives `storageGiB` 0
   said it needs none and gets no note; one that gives any size has that size priced, as today, and
   gets no note;
5. the resource's count is not 0. A resource whose count is 0 is estimated at 0.00 and leaves nothing
   out, so it gets no note (only a hand-built resource can have one: `inventory.New` refuses a count
   below 1).

The note says what is left out, by category:

- `compute.vm`, `compute.baremetal`, `kubernetes.managed`: the **boot volume** (per instance or node);
- `database.postgresql`, `database.mysql` (the categories whose storage is stored once, `storedOnce`):
  the **data storage**.

The list is explicit: any other category, including one later routed to an estimate by plan, gets no
note until it is added here and to the switch in `storageNotIncluded`.

Resources priced by storage or by usage, and resources not estimated, never carry it. The amount, the
breakdown and the totals do not change: this ADR adds a statement, not a price.

### Domain

In `internal/domain/estimate`:

```go
// StorageNotIncluded names the storage an estimate by plan leaves out: the
// chosen plan includes none, its offering prices storage per GiB in the
// estimate region, and the resource gives no storage size (ADR 0032).
type StorageNotIncluded string

const (
	// BootVolume is left out of an estimate for compute.vm, compute.baremetal
	// and kubernetes.managed: each instance's or node's boot volume.
	BootVolume StorageNotIncluded = "boot-volume"
	// DataStorage is left out of an estimate for database.postgresql and
	// database.mysql: the service's data storage.
	DataStorage StorageNotIncluded = "data-storage"
)
```

- `Estimate` gains `StorageNotIncluded StorageNotIncluded`: `BootVolume` or `DataStorage` when the
  rule above holds, `""` otherwise. It is set only when `Estimated`, and only by `byPlan`.
- `Total` gains `StorageNotIncluded int`: how many of the estimated resources carry a non-empty
  `StorageNotIncluded`. `Sum` counts it, so the assessment's total and every workload's total
  (`assessment.Workload.Total`) have it with no change in `internal/domain/assessment`.
- The package documentation (`doc.go`) states the rule.

### JSON report: schema version 2 stays

Both additions are optional keys, written only when they say something, so a document without a
resource that carries the note is byte for byte what it is today. Under ADR 0018 an added optional key
keeps the `schemaVersion`; a reader that ignores keys it does not know reads every document as before.
No new schema files, no new route; `assessment-v2.schema.json` and `comparison-v2.schema.json` are
updated in the same change, with identical definitions.

- `$defs.resourceEstimate` gains `storageNotIncluded`: a `string`, present only when the resource is
  estimated and the note applies, never `""`. Values today: `"boot-volume"`, `"data-storage"` (the
  domain constants' values). It is an **open set**, like a not-estimated `reason`: no `enum` in the
  schema, and its description says a consumer must accept a value it does not know. Example:
  `"estimate": {"estimated": true, "amount": "40.88", "countAssumed": true, "hoursAssumed": true, "storageNotIncluded": "boot-volume", "breakdown": {...}}`
  (key order follows the Go struct: after `billingCapHours`, before `breakdown`).
- `$defs.total` gains `storageNotIncluded`: an `integer`, `minimum` 1, present only when at least one
  estimated resource carries the note, absent when none does (absent means 0). The same definition is
  used by `estimate.total` (the assessment), each workload's `estimate`, and in the comparison each
  target's `estimate.total` and workloads' `estimate`, so all of them carry it. Key order: after
  `resources`.
- Neither key is in a `required` list. Like `resourceEstimate.estimated` (a boolean) and
  `total.estimated` (a count), the same key name is the per-resource fact and the count of it.
- The HTTP API serves the same reporter's bytes: `/v1/assessments` gains the keys with no change in
  `internal/httpapi` or `api/openapi.json`, which refers to the v2 schema files.

### Markdown report

- **Estimate cell** (the Monthly estimate column of the single-target resources table, and each
  target's column in the comparison's resources table, both `renderEstimate`): after the existing
  "(count assumed 1)" and "(730 hours assumed)", one of
  - ` (boot volume not included)` for `BootVolume`,
  - ` (data storage not included)` for `DataStorage`.

  Example: `€40.88 (count assumed 1) (730 hours assumed) (boot volume not included)`. A value the
  renderer does not know is written as ` (storage not included)`.
- **Totals** (`renderTotal`, so the Cost estimate section's "Total" line, each workload's "Monthly
  estimate" line and each target's row of the comparison's summary): when `Total.StorageNotIncluded`
  is above 0, append ` (storage not included in N estimate)` for N = 1 and
  ` (storage not included in N estimates)` otherwise. Example:
  `€2,535.00 per month for 4 of 8 resources (storage not included in 2 estimates)`. With N = 0 the
  line is unchanged.

The renderers only map the domain's value to these words; neither decides when the note applies.

## Alternatives considered

- **A catalog fact: an explicit field on the offering or plan** (for example `bootVolume: true`, or
  `storageIncluded: false`, in snapshot format v2). It lost because it states a second time what
  `storageGiB` and `storagePrices` already state, and the two can disagree: a plan marked as needing a
  volume with `storageGiB` above 0, or a new zero-storage plan its author forgets to mark. The derived
  rule cannot disagree with the price: it fires exactly when a given size would have been charged in
  full at the offering's per-GiB price, so the note marks exactly the part the estimate would add. A
  field would also change the snapshot format (decoded strictly, [ADR 0009](0009-decode-adapter-json-documents-through-one-strict-decoder.md),
  [ADR 0024](0024-add-plans-prices-and-a-default-region-in-target-snapshot-format-v2.md)), need
  marking 9 offerings across three snapshots by hand and a `targettest` check, for the same result
  today. A future target is safer with the derived rule: a snapshot author already has to record
  included storage and a storage price correctly for estimates to be right, and nothing more is asked.
  The one case the derived rule gets wrong is a plan that includes no storage, sells storage per GiB,
  and yet needs none to run (a diskless or network-booted server); no target has one, and if one
  appears its snapshot can record the storage it includes for free as `storageGiB`.
- **Only OVHcloud, only `compute.vm`, or only "boot volume".** It lost because Scaleway Instances and
  Kapsule and UpCloud Cloud Native have the same gap today and their sources say so; a note only for
  OVHcloud would make the comparison look as if the others were complete. For managed databases the
  missing part is data storage, not a boot volume, so the note names which.
- **Charge a default boot volume (for example 10 GiB).** Rejected by the maintainer on 2026-11-14: a
  guessed size would be a price nobody can trace to the inventory or a catalog.
- **A boolean per estimate (`bootVolumeNotIncluded: true`).** It lost because it either says "boot
  volume" for databases, which is wrong, or needs a second boolean for them. One open string carries
  both and leaves room for another kind without a new key.
- **Always write the keys (`false` and 0).** It lost because every estimated resource and every total
  in every golden would change, against the issue's acceptance, and an absent key already reads as
  "no note" for a consumer that tests for the key, as with `billingCapHours`.
- **A new `schemaVersion` 3.** Not needed: both keys are optional additions, which ADR 0018 keeps in
  the current version, and a new version would need a new route (ADR 0019, ADR 0030).

## Consequences

- Estimates that leave out a boot volume or data storage say so, in every report and in the API. The
  amounts do not change.
- Besides OVHcloud's Gen 3 instances and Managed Kubernetes nodes, the note appears today for Scaleway
  Instances, Kubernetes Kapsule and Kapsule Dedicated nodes and Managed Database plans, and UpCloud
  Cloud Native servers and Managed MySQL and PostgreSQL plans, whenever the resource gives no storage
  size. No OVHcloud database, OVHcloud or Scaleway bare-metal server, UpCloud Starter or Premium server
  or UpCloud Managed Kubernetes plan carries it: their plans include storage.
- Changes, for the tester and the coder:
  - `internal/domain/estimate`: the type and constants, `Estimate.StorageNotIncluded`,
    `Total.StorageNotIncluded`, `byPlan` and `Sum`, `doc.go`; tests in `estimate_test.go` and
    `sum_test.go` (a Gen 3 VM with no disk size has `BootVolume`; with a disk size, or with
    `storageGiB` 0, none; a plan that includes storage, none; an offering without a storage price in
    the region, none; a database has `DataStorage`; a resource not estimated, none).
  - `internal/adapter/report/markdown`: `renderEstimate`, `renderTotal`, `doc.go`; goldens.
  - `internal/adapter/report/jsonreport`: the resource estimate and total structs
    (`storageNotIncluded` with `omitempty`), `doc.go`; `schema/assessment-v2.schema.json` and
    `schema/comparison-v2.schema.json`; goldens, which the schema test validates.
  - No change in the catalog snapshots, `internal/domain/catalog`, `internal/domain/assessment`,
    `internal/httpapi` or `api/openapi.json`.
  - Docs: `docs/guide/assess.md` (the estimate cell and total notes), `docs/guide/http-api.md` (the
    two keys), and `docs/guide/try-it.md` where its examples show an affected estimate.
  - Goldens that change: `test/examples/testdata/*-estimates.*.golden`, `*-all-estimates.md.golden`,
    `aws-scaleway-nl-ams.md.golden`, and `test/e2e/testdata/examples-*.golden` and
    `examples-aws-scaleway.json.golden`, wherever an affected resource gives no storage size, plus the
    renderers' own goldens that hold such an estimate. Nothing else changes.
- A future target needs nothing new: recording `storageGiB` and `storagePrices` correctly, as an
  estimate already requires, is enough for the note to be right.
