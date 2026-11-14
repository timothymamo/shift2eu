# 0027. Match plans and estimate monthly prices in a domain package, with exact money

- Status: Accepted. Amended by
  [0029](0029-cap-the-billed-hours-of-an-offerings-plans-in-target-snapshot-format-v2.md): on an offering with
  `planBillingCapHours`, the plan part of a `ByPlan` estimate is min(hours, cap) / cap of the plan price;
  fixed fees and storage are unchanged
- Date: 2026-11-14

## Context

PRD #190 milestone 6 adds, for each resource assessed against a target, the plan it would run on and
its estimated monthly list price, the reasons a resource is "not estimated", the totals, availability
in one target region the user chooses, and a switch that turns estimates off. The PRD fixes the
formula and the plan order; ADR 0024 fixes the catalog's prices, units and rounding; ADR 0025 fixes the
inventory's sizes, their absence and their defaults; ADR 0026 fixes the fixed fee. Several choices are
left open by all of them, and milestone 6 is test first, written by another model family (ADR 0003),
so they have to be fixed by name before any test is written:

- where plan matching and estimating live, and the API the assessment and the application layer use;
- how to compute exactly when the inventory's numbers are `float64` (ADR 0025) and the catalog's are
  integers (ADR 0024), without an overflow;
- which offering a plan is chosen from when several candidates tie;
- what each priced category needs and how its storage is counted;
- the closed list of "not estimated" reasons and which wins when several apply;
- which regions a target "lists", against which a chosen region is checked.

## Decision

### A new domain package, `internal/domain/estimate`

Plan matching and estimating are one rule set over one mapping result, independent of how an
assessment gathers verdicts. They live in a new domain package `estimate`, which imports `catalog`,
`inventory`, `mapping` and `taxonomy`; `assessment` imports `estimate`. Its entry point is
`estimate.For(result mapping.Result, region string) Estimate`, and `estimate.Sum` gives the totals. The
package is added to every depguard rule that lists the domain packages for the domain, the application
layer and the driving adapters, and to `test/architecture`'s pinned rules. The reporters' rule gains it
in milestone 8, when a reporter first needs it.

Region scoping is a mapping rule (which offerings are candidates), so it is `mapping.MapIn(r, c,
region)`, beside `Map`. `MapIn` with region `""` is `Map`. A resource whose category some offering
serves, but none in the region, gets fit `none` and one reason `mapping.NotOfferedInRegion`, whose
sentence is "not offered in `<region>`" (parts: a literal `not offered in ` and a new part kind,
`mapping.PartRegion`). A category no offering serves anywhere keeps `NoOffering`.

`assessment.AssessWith(inv, cat, assessment.Options{Region, Estimate})` maps with `MapIn` and, when
`Estimate` is true, sets `Verdict.Estimate` (a pointer, nil when off), `Assessment.EstimateRegion`
(`Region`, or else the snapshot's default region) and `Assessment.Total`. `Assess(inv, cat)` is
unchanged and equals `AssessWith` with the zero `Options`, so every existing caller, test and golden is
untouched until milestone 8 switches the drivers over. The comparison needs no new type: each
`TargetAssessment` carries its own `Total` and regions.

The application layer takes the choices through `Analysis.RunWith(ctx, w, meta, analysis.Options)` and
`Comparison.RunWith(ctx, w, meta, analysis.ComparisonOptions)`, with the comparison's region per
target on `analysis.Target.Region`. `Run` is `RunWith` with the zero options. A region the target's
catalog does not list is a `*analysis.RegionError` (target key, region, the target's regions) wrapping
`analysis.ErrUnknownRegion`. Every region is checked before anything is assessed and before the
inventory is read: when a region is chosen, `RunWith` loads the catalog (in a comparison, every
target's catalog, in key order) and checks each region right after its catalog is loaded, then reads
the inventory, then assesses. A bad region on the last target of a comparison therefore reads no
inventory and assesses no target, and the drivers (milestone 8) need not check regions themselves.
Without a chosen region the order stays the one `Run` has always had: the inventory first.

### The regions a target lists

The domain does not hold a snapshot's declared regions (ADR 0024), so a target's regions are
`catalog.Catalog.Regions()`: the sorted union of every offering's regions and the snapshot's default
region. A region declared in the snapshot but served by no offering is not listed: choosing it could
only make every resource "not offered". Comparison is exact, as for every region (ADR 0026).

### Exact money: `math/big`, and the inventory's floats read as the decimals they print as

`estimate.Money` is an immutable amount of euros held as a `*big.Rat` (zero value 0 EUR), compared with
`Equal` (which go-cmp uses) or `Cmp`, built from a catalog amount with `FromAmount` or from a fraction
with `Ratio`, and formatted with `String` (rounded to the cent, half away from zero, two fractional
digits, ADR 0024) or `Exact` (`"num/den"`). The domain allow list gains `math/big`, which is pure.

The inventory's numbers are `float64`. Each is read as the exact decimal of its shortest round-trip
representation (`strconv.FormatFloat(v, 'g', -1, 64)`), the number the user wrote, then as a rational,
rounded to **at most six fractional digits, half away from zero**. So `0.1` GiB is exactly 1/10 GiB,
not the binary float just above it, `hoursPerMonth: 365` gives exactly half of a 730-hour price,
`5e-324` hours read as 0 and `100.0000004` GiB as 100.

The rounding bounds every denominator at 10^6 (times the 730 of the hours share and the catalog's
10^4). Without it, a security audit of the milestone found that valid but adversarial numbers
(subnormals such as `5e-324`, values with 17 significant digits) make rationals of about 1,100 bits,
about 1 s of CPU per request at the inventory's resource cap across every target: cheap CPU time for
an HTTP API client (milestone 8). Six digits are far finer than anything a size or an hour count
means (about a kibibyte, under four milliseconds of running time).

A value no valid inventory holds, NaN, ±Inf or a negative number (and a negative `count`), which only
a resource built without `inventory.New` can carry, is read as no size at all: the resource is "not
estimated: no size", never estimated at 0 EUR or at a negative amount. `Money` is never negative.

Every product is exact and nothing can wrap. With checked `int64` the worst case does not fit: a plan
at `MaxAmount` (10^10 units) × `count` 100,000 × 744 hours is about 7.4 × 10^17 units before dividing
by 730, and a fractional `hoursPerMonth` or `storageGiB` with up to 17 significant digits needs a
common denominator of up to 10^6 more. A rational has no such bound, and the sizes are capped
(ADR 0024, ADR 0025) and read to six decimals, so the numbers stay small enough to compute instantly.
Money is rounded only in `Money.String`, once per displayed figure; totals are summed exactly.

Plan coverage is compared exactly in the catalog's units: a plan covers a resource when
`plan.VCPUs ≥ vcpus` and `plan.MemoryMilliGiB ≥ memoryGiB × 1000`, both as rationals.

### How each category is priced

`estimate.PricingOf(category)`:

| Category | Method | Needs | Estimate |
|---|---|---|---|
| `compute.vm`, `compute.baremetal`, `kubernetes.managed` | `ByPlan` | `vcpus` and `memoryGiB` | plan × count × hours / 730 + max(0, storageGiB − included) × per-GiB × count + fee × hours / 730 |
| `database.postgresql`, `database.mysql` | `ByPlan` | `vcpus` and `memoryGiB` | plan × count × hours / 730 + max(0, storageGiB − included) × per-GiB + fee × hours / 730 |
| `storage.block` | `ByStorage` | `storageGiB` | storageGiB × per-GiB × count |
| `storage.object` | `ByStorage` | `storageGiB` | storageGiB × per-GiB |
| `serverless.function`, `messaging.queue` | `ByUsage` | — | not estimated |
| `unclassified` | `NotPriced` | — | never estimated (its fit is always `none`) |

- `vcpus` and `memoryGiB` are both needed for a plan: with either missing the resource is "not
  estimated: no size". A smaller plan is never guessed.
- An absent `storageGiB` on a `ByPlan` category is no storage beyond the plan. An absent `count` is 1
  and an absent `hoursPerMonth` 730 (ADR 0025); the estimate records each assumption
  (`CountAssumed`, `HoursAssumed`) so the report can say so.
- **Storage per instance, or for the whole service.** `storageGiB` on compute is per instance and on
  Kubernetes per node (ADR 0025), so the storage above the plan is multiplied by `count`. On a managed
  database it is the service's data: the snapshots record a database plan's included storage and its
  per-GiB price for the whole service at its node count (OVHcloud `SOURCES.md`, *Managed databases*),
  so it is counted once, not × `count` again.
- Storage is not scaled by `hoursPerMonth`: the PRD's formula scales the plan price only, and stored
  data is billed while it exists. The fixed fee is scaled by hours and never multiplied by `count`
  (ADR 0026), and is added only to an estimate that has a plan price: a fee alone never makes one.
- A storage category whose offering has plans ignores them: it is priced per GiB.

### Choosing the plan

The plan comes from the **best candidate offerings**: the verdict's candidates with the same fit and
as many missing capabilities as the first one, the set `Verdict.Ties` names beside the best. This is
narrower than "every candidate with the best fit": a partial candidate that lacks more capabilities is
a worse fit, and choosing it for its price would contradict the verdict. It is wider than the best
offering alone: the remaining ties are tiers of one product (ADR 0023), and the plan order is meant to
pick the cheapest among equals, not the one whose name sorts first.

Across those offerings, a plan **covers** the resource when its vCPUs and memory are each at least the
resource's. The chosen plan is the covering plan with a price in the estimate region
(`Offering.PlanPrice`) that is cheapest, ties broken by fewer vCPUs, less memory, plan name and then
offering name (byte order). `Verdict.BestOffering` does not change; `Estimate.Offering` names the
chosen plan's offering.

- "Cheapest" is the plan price alone, not the plan plus storage: the PRD's rule, and deterministic.
- When covering plans exist but none has a price in the region, the resource is "not estimated: no
  price" and `Estimate.Plan` names the covering plan first in the order fewer vCPUs, less memory, plan
  name, offering name, with `Priced` false.
- When no plan covers it, the resource is "not estimated: no plan large enough" and `Estimate.Largest`
  names the largest plan: most vCPUs, then most memory, then the smaller plan name, then the smaller
  offering name. Its fit is unchanged (PRD #190, *Decisions*).
- When the best candidate offerings list no plan at all (Scaleway and UpCloud until their price
  milestones), the reason is "no plan".
- A `ByStorage` resource is priced on the best candidate offering whose storage price in the region
  is lowest, ties by offering name, and is "not estimated: no price" when none has one.

### The reasons a resource is not estimated

`estimate.Reason`, checked in this order, the first that applies winning:

| Reason | When |
|---|---|
| `no-fit` | the fit is `none`: no offering, not classified, or not offered in the chosen region |
| `usage-priced` | the category is `ByUsage` (or `NotPriced`) |
| `no-size` | a `ByPlan` resource without `vcpus` or `memoryGiB`, or a `ByStorage` one without `storageGiB`; or a size or usage it reads that is NaN, infinite or negative |
| `no-plan` | `ByPlan`, and the best candidate offerings list no plan |
| `no-plan-large-enough` | `ByPlan`, and no plan covers the resource |
| `no-price` | no covering plan (`ByPlan`) or no storage (`ByStorage`) has a price in the estimate region |
| `no-storage-price` | `ByPlan` with storage above the chosen plan, and no per-GiB price for it in the region |

The order goes from facts about the resource and the target to facts about one region's price list,
so a resource reads "not offered" before "no size", and "no size" before anything a price list could
fix. An estimate with a reason has `Estimated` false and a zero `Monthly`; `Sum` leaves it out of the
total and counts it in `Resources`, never as 0 EUR.

### The switch

With `Options.Estimate` false no verdict has an estimate and the assessment has no estimate region and
no total. Sizes never reach the mapping, so the fits are those of the same inventory without sizes.

## Alternatives considered

- **Estimating inside `assessment`.** One package fewer to register, but `assessment` would hold two
  rule sets (verdicts and pricing), and the pricing rules could only be tested through whole
  assessments. A package of its own is tested resource by resource.
- **A region filter in `assessment`, not `mapping`.** The region decides which offerings are
  candidates, which is what `mapping` decides; doing it later would leave `mapping.Result` with
  candidates the assessment then drops, and two places that build fits.
- **Checked `int64` arithmetic.** The inputs are now read to six decimals, but the worst case of the
  caps overflows anyway (above).
- **Refusing tiny values in the inventory's validation instead of rounding.** Also bounds the
  denominators, but needs a minimum per field and still lets 17-digit values through; rounding bounds
  every input with one rule, in the one place the numbers become exact.
- **`float64` money.** Refused by ADR 0024.
- **`big.Rat` read straight from the float (`SetFloat64`).** Exact, but exact for the binary float:
  `0.1` GiB becomes 0.1000000000000000055 GiB and needs a larger plan than a 0.1 GiB one.
- **The plan from the best offering only.** Simpler, but between tiers of one product the name order
  would pick a dearer plan than an equally good tier's, which is not "the cheapest covering plan".
- **The plan from every candidate with the best fit.** Proposed while planning the milestone. Refused for
  partial fits: it could put a resource on an offering that lacks more of its capabilities than the
  verdict's best, only because it is cheaper.
- **Cheapest by the whole estimate (plan plus storage).** Arguably closer to a bill, but not the
  PRD's rule, and it couples plan choice to storage prices.
- **Database storage × count.** What the inventory's per-node vCPU and memory would suggest, but the
  snapshots price database storage for the whole service, so it would count it twice.
- **Validating the region in the domain.** `AssessWith` would need an error for a value only a user
  supplies; checking it where the user's choices arrive, the application layer, gives one typed error
  for the CLI and the HTTP API alike (milestone 8).
- **The snapshot's declared regions as the target's regions.** Not in the domain (ADR 0024); adding
  them to `catalog.Snapshot` would change the catalog for regions no offering serves.

## Consequences

- The domain may import `math/big`; `estimate.Money` is the only type that holds a `big.Rat`, never
  exposed, so callers cannot mutate a shared value.
- `Verdict`, `Assessment` and `analysis.Target` grow fields that are nil or empty until a caller asks
  for them, so milestone 6 changes no report and no golden. Milestone 8 calls `RunWith` from the CLI
  (`--region`, `--no-estimate`, `RegionError` as exit 2) and the HTTP API (`regions`, `estimate`), and
  renders `mapping.PartRegion`.
- A plan is never chosen on an offering that fits worse than the verdict's best: the plan set follows
  the verdict, so a report's plan and its fit never disagree.
- UpCloud caps a month's bill at 672 hours; the formula is applied as the PRD writes it (price ×
  hours / 730), so an UpCloud estimate for more than 672 hours a month is above UpCloud's capped bill.
  No rule handles it; the PR notes it. (Handled by ADR 0029: plan prices on a capped offering scale
  by min(hours, cap) / cap.)
- Milestone 7 builds per-workload totals with `estimate.Sum` over a workload's verdicts.
