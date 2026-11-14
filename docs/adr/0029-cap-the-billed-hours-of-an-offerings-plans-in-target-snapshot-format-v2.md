# 0029. Cap the billed hours of an offering's plans in target snapshot format version 2

- Status: Accepted. Amends [0024](0024-add-plans-prices-and-a-default-region-in-target-snapshot-format-v2.md):
  an offering may carry an optional `planBillingCapHours` key, within schema version 2. Amends
  [0026](0026-add-an-optional-fixed-monthly-fee-per-offering-to-snapshot-format-v2.md): a second
  recorded exception to [0007](0007-shared-target-catalog-snapshot-format-v1.md)'s version policy and
  "every key required" rule, which ADR 0026 said would not extend to other keys; fixed fees stay scaled
  by hours / 730. Amends [0027](0027-match-plans-and-estimate-monthly-prices-in-a-domain-package.md):
  the plan part of a `ByPlan` estimate on a capped offering is min(hours, cap) / cap of the plan price
- Date: 2026-11-14

## Context

Issue #218. UpCloud bills Cloud Servers by the hour, capped at 672 hours (24 h × 28 days) a month. The
snapshot stores the plans' monthly list prices as the pricing page shows them, which is what a server
running at least 672 hours pays (`internal/adapter/target/upcloud/SOURCES.md`, *Prices*). An estimate
scales a plan price by `hoursPerMonth` / 730 (ADR 0027), so for a resource running fewer hours it is
lower than UpCloud's bill, min(hours, 672) / 672 of the price: at 336 hours, 46% of the price against
50%. ADR 0027's *Consequences* accepted this for PRD #190; the maintainer accepted it there too
(2026-11-14) and opened #218 to fix it.

What UpCloud's pricing page (<https://upcloud.com/pricing/>, the maintainer's saved copy read
2026-11-14, as `SOURCES.md` cites it) says, short sentences only:

- under the *Starter*, *Premium* and *Cloud Native* Cloud Server tables: "billed by the starting hour,
  maximum of 28 days per month";
- under the Managed Kubernetes data plane: "All node plans are billed by the starting hour, maximum of
  28 days per month";
- under the Managed Kubernetes control plane, the managed database tables and others: "Resources are
  charged by the hour. Monthly prices are estimates based on a 30-day month.";
- under *Block Storage*: "Block Storage is priced by allocated size per GB. Resources are charged by the
  hour. Monthly prices are estimates for 1 GB based on a 30-day month.";
- under the GPU plans (left out of the snapshot): "billed by the starting hour. Monthly prices are
  estimates based on a 30-day month."

The Cloud Server configurations page, already cited in `SOURCES.md`, gives the figure: "Cloud Servers
are billed hourly and capped at 672 hours (24 h x 28 days) per month".

So the cap is on **server and node plans only**. The control-plane fee, the databases, Block Storage
(including a server's extra devices) and Object Storage are charged by the hour with no cap, and their
monthly figures are for a 30-day month, which the snapshot already converts to 730 hours.

OVHcloud and Scaleway (their `SOURCES.md` files, not their sites): neither records a cap. OVHcloud's
page gives an hourly price for every plan and says "The monthly price is estimated based on 730 hours
per month"; its database guide gives "Monthly price = Hourly price × 730 hours". Scaleway publishes
every recorded price per hour, and its "Approx. per month" figures are hourly × 730. Both list a cheaper
monthly *billing* option (OVHcloud B2 and Metal Instances, Scaleway Elastic Metal), which is a
commitment the snapshots leave out on purpose, not a cap on hourly billing. Neither target needs the
new key.

Forces:

- **ADR 0007's strictness and version policy**, as ADR 0026 weighed them: every snapshot in the
  repository but three UpCloud offerings has no cap to state, and they must load unchanged.
- **Exact money** (ADR 0024, ADR 0027): the share is a rational, computed with `estimate.Money`'s
  `big.Rat`, never a float, rounded only for display.
- **Never zero for unknown, and one spelling per fact** (ADR 0026): "no cap" must have one spelling.
- **Tests come first, from another model family** (ADR 0003): every key, field, constant, sentinel and
  the formula are named here.

## Decision

An offering in a version 2 snapshot may have **one more optional key, `planBillingCapHours`**: an
integer from 1 to 744, the most hours a month the target bills for one instance of one of the
offering's plans. Its plans' monthly prices are the price of that many hours. The schema version stays
**2**. `catalog.Offering` gains `PlanBillingCapHours int64`, 0 meaning uncapped. A `ByPlan` estimate on
a capped offering prices the plan at **min(hours, cap) / cap** of its monthly price; on an uncapped
offering at hours / 730, exactly as today. Fixed fees and storage are not capped.

### The document

```json
{
  "name": "Cloud Servers",
  "categories": ["compute.vm"],
  "capabilities": ["ipv6", "snapshots"],
  "regions": ["de-fra1", "fi-hel1"],
  "plans": [ ... ],
  "storagePrices": [ ... ],
  "planBillingCapHours": 672
}
```

| Field | JSON type | Required | Meaning |
|---|---|---|---|
| `planBillingCapHours` | integer, 1 to 744 | **no** | The most hours a month the target bills one instance of one of the offering's plans; the plans' prices are what that many hours cost. Applies to every plan and every region of the offering. It never applies to `storagePrices` or `fixedFees` |

- The key sits on the **offering** object, beside `fixedFees`, spelt `planBillingCapHours` character
  for character. One cap per offering: UpCloud's cap is the same for every plan and zone, and a
  target with plans billed differently gets them in separate offerings (ADR 0011).
- **Absent**: uncapped. Every snapshot written before this ADR reads unchanged and gives the same
  `catalog.Catalog` (with `PlanBillingCapHours` 0 on every offering).
- **Present**: an integer written as ADR 0024 writes integers (the reader's `integer`,
  `-?(0|[1-9][0-9]*)`, no fraction, no exponent, no `-0`).
- 744 is the hours of a 31-day month, the inventory's own cap on `hoursPerMonth` (ADR 0025): a cap above
  it could never be reached, so it is refused rather than read as uncapped.

### Reader (`internal/adapter/target/snapshot`)

- New key constant `keyPlanBillingCapHours = "planBillingCapHours"`; `offeringKeys` gains
  `{Name: keyPlanBillingCapHours, Required: false}`. The comment naming `fixedFees` as the format's one
  optional key names both, and this ADR.
- The offering walk reads it with `integer` and passes it to `catalog.Offering.PlanBillingCapHours`.
- Refused with **`ErrDecode`** by the reader, with the path `offerings[N].planBillingCapHours`:
  `null`, a non-number (`"672"`, `true`, `{}`, `[]`), a fraction (`672.5`, `672.0`), an exponent
  (`6.72e2`), `-0`, a value that does not fit `int64`, and **`0`**. `0` is refused by the reader, as an
  empty `fixedFees` is, so "uncapped" has one spelling (leave the key out); the error says so, for
  example `offerings[1].planBillingCapHours at line 40, column 30 is 0, want 1 to 744 (leave the key
  out for no cap)`. A repeated or unknown key is refused as today.
- A negative integer (`-1`) or one above 744 (`745`) is read and refused by `catalog.New` (below), whose
  error `Parse` returns as is. So `snapshot.Parse` refuses zero, negative and over-744 caps; tests pin
  `errors.Is(err, snapshot.ErrDecode)` for `0` and the malformed values, and
  `errors.Is(err, catalog.ErrInvalidBillingCap)` for `-1` and `745`, with the path or the offering name
  and `planBillingCapHours` in the message.

### Domain (`internal/domain/catalog`)

`Offering` gains a field after `FixedFees`:

```go
// PlanBillingCapHours is the most hours a month the target bills for one
// instance of one of the offering's plans, whose monthly prices are the price
// of that many hours; 0 means uncapped, the prices standing for 730 hours.
// It applies to plan prices only, never to StoragePrices or FixedFees
// (ADR 0029). In a Catalog it is 0 or from 1 to MaxBillingCapHours.
PlanBillingCapHours int64
```

a constant and a sentinel:

```go
// MaxBillingCapHours is the largest PlanBillingCapHours: the hours of a
// 31-day month.
const MaxBillingCapHours = 744

// ErrInvalidBillingCap is returned for an offering whose PlanBillingCapHours
// is negative or above MaxBillingCapHours.
ErrInvalidBillingCap = errors.New("invalid plan billing cap")
```

`catalog.New` checks the cap after the fixed fees. The error is one line, wraps `ErrInvalidBillingCap`
and names the offering and the field, for example `offering "Cloud Servers": planBillingCapHours:
invalid plan billing cap: 745, want 0 to 744`. The exact text may vary. `0` is valid in the domain (it
is "uncapped"); only the reader refuses a written `0`. `clone` copies the field by value, and
`targettest`'s `equalOffering` compares it and `describe` shows it (`PlanBillingCapHours: 672`).

### Estimate (`internal/domain/estimate`)

No new exported function; the change is in the arithmetic of `estimate.For` for a `ByPlan` resource.
With `hours` the resource's `hoursPerMonth` read exactly (ADR 0027, 730 when absent), `cap` the chosen
plan's offering's `PlanBillingCapHours`, and `price` the plan price in the estimate region:

```
planShare = min(hours, cap) / cap     when cap > 0
planShare = hours / 730               when cap = 0     (unchanged)

Breakdown.Plan     = price × count × planShare
Breakdown.FixedFee = fee × hours / 730                 (unchanged, never capped)
Breakdown.Storage  = unchanged (never scaled by hours)
Monthly            = Plan + Storage + FixedFee         (unchanged)
```

All in `big.Rat`, through `estimate.Money`; `min` compares rationals exactly. The internal helper
`monthShare(hours)` becomes a plan share that takes the cap (for example `planShare(hours, cap
*big.Rat)`, the name is the implementer's), and the fee keeps `hours / 730`. The constant
`estimate.HoursPerMonth` (730) is unchanged. Plan choice (cheapest covering plan, ADR 0027) still
compares the monthly plan prices alone, not the shares: a plan's price is its price whichever share an
offering bills. That holds only while every candidate offering of a category in one target has the same
cap, which is true today: UpCloud's `compute.vm` has one offering, and both its `kubernetes.managed`
offerings cap at 672. A target that mixed capped and uncapped offerings in one category could pick a
plan that is cheaper by monthly price but dearer at partial hours (a capped plan at 336 hours bills 1/2
of its price, an uncapped one 336/730); plan choice is to be revisited when such a target arrives. Every
other rule of ADR 0027 is unchanged, `HoursAssumed` included.

Tests target `estimate.For(mapping.Result, region)` and read `Estimate.Breakdown.Plan`,
`Estimate.Breakdown.FixedFee` and `Estimate.Monthly` (`Money.Equal`, `estimate.Ratio`,
`estimate.FromAmount`), on a catalog built with `catalog.New` whose offering sets
`catalog.Offering.PlanBillingCapHours`. Worked cases, plan price 38.00, count 1, no storage, no fee:

| `hoursPerMonth` | cap 672 | uncapped |
|---|---|---|
| 336 | 19.00 (exactly 1/2) | 38 × 336 / 730 = 6384/365 (17.49) |
| 672 | 38.00 | 38 × 672 / 730 |
| 730, or absent | 38.00 | 38.00 |
| 744 | 38.00 (never above the price) | 38 × 744 / 730 |

With a fee of 60.8333 on a capped offering at 336 hours, `FixedFee` is 60.8333 × 336 / 730, as today.

Issue #218's acceptance follows: an UpCloud server at 336 hours is estimated at 50% of its monthly
price, at 730 hours at 100%, and every uncapped offering estimates as before.

### Data and `SOURCES.md`

In the same change as the code, UpCloud's snapshot sets `"planBillingCapHours": 672` on **Cloud
Servers**, **Managed Kubernetes** and **Managed Kubernetes Production** (their plans are server and
node plans, billed "maximum of 28 days per month"), and on no other offering: Managed PostgreSQL and
MySQL (single and multi-node), Block Storage, Block Storage Encrypted and Managed Object Storage are
charged by the hour with no cap. The Production control-plane fee (`60.8333`) stays uncapped. OVHcloud's
and Scaleway's snapshots do not change.

UpCloud's `SOURCES.md` records the cap in *Prices* and in each capped offering's section, with the
sentences above and `` `672` `` as a code span. `snapshot/sources_test.go` checks, for every offering
with a cap, that its `### <offering name>` section contains the cap as a code span (`` `672` ``), as it
checks price amounts.

## Alternatives considered

- **Schema version 3.** What ADR 0007's policy asks for, and ADR 0026 said its exception would not
  extend. Rejected for the reason ADR 0026 gave for its own key: every offering of every snapshot would
  be rewritten for a datum three offerings have, the snapshots are embedded, and no reader outside the
  binary meets them. The maintainer chose an optional key in version 2 (issue #218).
- **A required key, `0` or `null` for none.** Rejected: every snapshot changes, and `null` is never
  read as absent in a snapshot (ADR 0007).
- **`0` accepted as "uncapped".** Rejected: two spellings for one fact, as ADR 0026 refused an empty
  `fixedFees`.
- **A cap per plan or per price.** More general, but no target needs it: UpCloud's cap is the same for
  every plan of an offering, and plans billed differently belong in other offerings (ADR 0011).
- **A cap on the whole offering, fixed fees and storage included** (`billingCapHours`). Rejected: the
  pricing page charges the control plane, Block Storage and the databases "by the hour" with monthly
  figures for "a 30-day month", and gives the 28-day maximum only for server and node plans. The name
  says what it caps.
- **Storing prices as 730-hour prices** (page price × 730 / 672) and keeping one formula. Rejected: it
  overstates every UpCloud estimate for a full-month server by 8.6%, and is still wrong below 672
  hours.
- **A target-wide cap in the snapshot header.** Rejected: UpCloud's databases and storage are uncapped,
  so the cap differs by offering.
- **Modelling "the starting hour"** (rounding hours up per started hour). Rejected: `hoursPerMonth` is a
  monthly total, not a list of runs; the estimate takes it as given.

## Consequences

- UpCloud partial-month estimates on servers and Kubernetes nodes match the bill; goldens with UpCloud
  `hoursPerMonth` below 730 change, and nothing else does.
- The format has two optional keys. Any further change follows ADR 0007's policy unless an ADR records
  another exception.
- `catalog.Offering` grows by one integer; `catalog.New`, `targettest` and the `SOURCES.md` check cover
  it. The reports do not show the cap; an estimate's plan part simply follows it.
- ADR 0027's consequence that "no rule handles" UpCloud's 672-hour cap no longer holds.
