# 0026. Add an optional fixed monthly fee per offering to target snapshot format version 2

- Status: Accepted. Amends [0024](0024-add-plans-prices-and-a-default-region-in-target-snapshot-format-v2.md):
  an offering may carry an optional `fixedFees` key, within schema version 2. This is a recorded
  exception to the schema version policy of [0007](0007-shared-target-catalog-snapshot-format-v1.md)
  ("any change to the format, even a new optional field, is a new version") and to its "every key
  required" rule, for this one key only. Amended by
  [0029](0029-cap-the-billed-hours-of-an-offerings-plans-in-target-snapshot-format-v2.md): a second optional key,
  `planBillingCapHours`, is recorded as the same kind of exception; fixed fees are never capped and stay
  scaled by hours / 730
- Date: 2026-11-13

## Context

Snapshot format version 2 (ADR 0024) prices an offering per plan, multiplied by a resource's `count`,
and per GiB of storage. It cannot hold a **fixed price per instance of an offering**: one charge per
cluster, however many nodes it has. Managed Kubernetes control planes are such a charge (issue #205):

- OVHcloud's Managed Kubernetes Standard plan charges €0.09 an hour per cluster (€65.70 a month at 730
  hours) in `RBX`, `PAR` and `MIL`. PRD #190 milestone 3 (PR #203) left it out of the snapshot and
  listed it in `internal/adapter/target/ovhcloud/SOURCES.md`, because a plan price would be multiplied
  by the node count.
- Scaleway Kapsule and UpCloud Managed Kubernetes have paid control-plane tiers too. PRD #190 milestones
  4 and 5 are told to leave them out and list them.

Without the fee, every estimate of a cluster on those targets is too low by the fee, and nothing in the
report says so. PRD #190 milestone 6 (plan matching and estimates) has not started, so the format can
change before anything reads prices.

Forces:

- **ADR 0007's strictness and version policy.** Every key required, no `null`, any change to the format
  a new version. But every snapshot already in the repository has no fee to state, and the issue asks
  that snapshots without one load unchanged.
- **ADR 0024's price rules.** Exact decimals read through `Walker.Raw`, never a float; `EUR` only;
  monthly prices, an hourly price × 730 rounded half up to 4 fractional digits; the reader refuses
  malformed numbers (`ErrDecode`), `catalog.New` refuses out-of-range values.
- **Never zero for unknown** (PRD #190). A region in which a fee is unknown must not read as free.
- **Tests come first, from another model family** (ADR 0003). This ADR is the specification the tests
  are written against, so it fixes every key, type, field, method, sentinel and error by name.

## Decision

An offering in a version 2 snapshot may have **one optional key, `fixedFees`**: a non-empty array of
ADR 0024 price objects, the offering's fixed monthly fee per instance (one Kubernetes cluster), on top
of its plans and storage. The schema version stays **2**. When the key is present its fees cover
**every region of the offering exactly once**. When it is absent the offering charges no fixed fee.
`catalog.Offering` gains `FixedFees []catalog.Price` and the method `FixedFee`.

Every other rule of ADR 0007, ADR 0009 and ADR 0024 holds unchanged.

### The document

```json
{
  "name": "Example Kubernetes",
  "categories": ["kubernetes.managed"],
  "capabilities": [],
  "regions": ["fr-par", "nl-ams"],
  "plans": [
    {
      "name": "EK-2-4",
      "vcpus": 2,
      "memoryGiB": 4,
      "storageGiB": 0,
      "prices": [{ "amount": 12.5, "currency": "EUR", "regions": [] }]
    }
  ],
  "storagePrices": [],
  "fixedFees": [
    { "amount": 65.7, "currency": "EUR", "regions": ["fr-par"] },
    { "amount": 0, "currency": "EUR", "regions": ["nl-ams"] }
  ]
}
```

Each offering: the fields of ADR 0007 and ADR 0024, and one more:

| Field | JSON type | Required | Meaning |
|---|---|---|---|
| `fixedFees` | array of price objects (ADR 0024), at least one | **no** | The monthly list price, in EUR excluding VAT, charged once per instance of the offering (one cluster), on top of its plans and storage prices. The fees cover every region of the offering exactly once. A fee of `0` is a fee: it is how "Included" or "Free" is written |

Each element is a price object exactly as ADR 0024 defines it, with every key required: `amount`
(exact decimal, at least 0, at most 4 fractional digits), `currency` (exactly `EUR`) and `regions`
(array of strings; empty means every region of the offering). The key is spelt `fixedFees`, character
for character.

Absent and present:

- **Absent**: the offering charges no fixed fee. Every snapshot written before this ADR is read
  unchanged and gives the same `catalog.Catalog` as before (with `FixedFees` nil on every offering).
- **Present**: a non-empty array. An **empty array is refused**, so "no fee" has one spelling: leave
  the key out. `null` is refused as for every other key (ADR 0007): it is never read as absent.
- A repeated `fixedFees` key and an unknown key are refused as today (ADR 0007, `jsondoc.Walker.Object`).

Hourly fees are converted as ADR 0024's prices are: × 730, rounded half up to 4 fractional digits.
OVHcloud's €0.09 an hour is `0.09 × 730 = 65.7`, stored as `65.7` (`Amount(657000)`, shown as `65.70`).

A target that sells a free and a paid control plane in the same region records the fee of the tier the
offering stands for (ADR 0011: one offering per tier; a tier that adds no capability gets no offering
and the offering stands for the product's default tier in that region). One offering has one fee per
region.

### Reader (`internal/adapter/target/snapshot`)

- New key constant `keyFixedFees = "fixedFees"`. `offeringKeys` gains
  `{Name: keyFixedFees, Required: false}`, the only key of the format with `Required: false`. The
  comment on the key lists ("every one of them required: the format has no optional field") changes to
  name this exception and this ADR.
- The offering walk reads `fixedFees` with the price list reader (`prices`, the same function as
  `storagePrices` and plan prices), so every price rule and number rule of ADR 0024 applies, with paths
  such as `offerings[2].fixedFees[1].amount`.
- An absent key leaves the offering's fees nil. A present key that reads as an empty array is refused
  with **`ErrDecode`**, placed by line and column and named by its path as other walk errors are, for
  example: `not a valid catalog document: offerings[2].fixedFees at line 31, column 20 is an empty
  array, want at least one fee (leave the key out for none)`. The exact text may vary; the tests pin
  `errors.Is(err, snapshot.ErrDecode)` and the presence of `offerings[2].fixedFees` and `empty`.
- Refused with `ErrDecode` by the reader, as ADR 0024's other prices are: `fixedFees` given as `null`,
  as anything but an array (`{}`, `"65.7"`, `65.7`, `true`), an element that is not a price object
  (`null` included), a price object with a missing, unknown or repeated key, and an `amount` with an
  exponent (`6.57e1`), more than 4 fractional digits (`65.70000`), a negative zero in any spelling
  (`-0`, `-0.00`), a value that does not fit `Amount`, or a non-number (`"65.7"`, `null`).
- A negative `amount` (`-0.01`), a currency other than `EUR`, a region that is not a region of the
  offering, overlapping fees and a region left without a fee are read and refused by `catalog.New`
  (below), whose error `Parse` returns as it is, so its sentinels stay reachable through `errors.Is`.
  A fee region is not checked against the declared regions: the offering's regions are, and
  `catalog.New` checks fee regions against those (ADR 0024, *Reader checks*, step 9).
- `snapshot.Parse` still reads schema version 2 only; `snapshot.Document` is unchanged.

### Domain (`internal/domain/catalog`)

`Offering` gains a field after `StoragePrices`:

```go
// FixedFees lists the offering's fixed monthly fee per instance (one
// Kubernetes cluster), charged on top of its plans and storage, by region.
// When the offering has fees, they cover every region of the offering exactly
// once. In a Catalog they are sorted by their first region, the every-region
// fee first, and nil when the offering charges no fixed fee.
FixedFees []Price
```

and one method:

```go
// FixedFee returns the fixed monthly fee the offering charges per instance in
// region, and true. It returns 0 and false when the offering charges no fixed
// fee (FixedFees is nil) or region is not one of its regions. region is
// compared exactly with the offering's regions and the fees' regions, as plan
// prices are: no case folding, no trimming. A fee whose Regions is nil applies
// to every region of the offering.
func (o Offering) FixedFee(region string) (Amount, bool)
```

For an offering in a `Catalog` with fees, `FixedFee(r)` returns `(fee, true)` for every `r` in
`o.Regions`, `fee` possibly `0`; `(0, false)` for any other `r`, including `""`. For an offering with no
fees it returns `(0, false)` for every region. A caller tells "no fee" (`false`) from "a fee of 0"
(`0, true`) by the boolean.

`catalog.New` checks an offering's fixed fees **after its storage prices** (ADR 0024's order is
otherwise unchanged), with the price list check ADR 0024 defines for `storagePrices`, under the list
name `fixedFees`: each fee in input order (amount, currency, regions, overlap), then the coverage check
below. A nil and an empty `FixedFees` are the same: no fixed fee, no error, nil in the catalog (the
reader never passes an empty list, since it refuses one).

Sentinels: the existing ones of ADR 0024, and **one new one**:

| Sentinel | Returned for a fixed fee |
|---|---|
| `ErrInvalidPrice` (existing) | An amount below 0 or above `MaxAmount`, a currency not exactly `EUR`, or a region that is blank or not valid text |
| `ErrPriceRegion` (existing) | A region the offering is not available in |
| `ErrOverlappingPrices` (existing) | Two fees of one offering that apply to the same region; a fee with no regions applies to every region, so it overlaps any other fee. A region listed twice in one fee is one region, not an overlap (ADR 0024) |
| **`ErrMissingFee`** (new) | An offering with at least one fee and a region that no fee applies to |

```go
// ErrMissingFee is returned for an offering whose fixed fees leave one of its
// regions without a fee: a region with no fee would read as free.
ErrMissingFee = errors.New("offering region has no fixed fee")
```

The fee errors reuse the price sentinels because a fee is a price and is checked by the same code; the
list name `fixedFees` in the error says which list failed. `ErrMissingFee` is new because no price list
of ADR 0024 has a coverage rule (a plan may be priced in only some regions), so no existing sentinel
means it. Coverage is checked once every fee has passed the other checks; when several regions lack a
fee, the error names the first in sorted order (`strings.Compare`), so it is the same on every run.

Error wording, in the form of today's errors (`offering %s: ` + list + index + wrapped sentinel):

- `offering "Example Kubernetes": fixedFees[0]: invalid price: amount -0.01, want at least 0`
  (`ErrInvalidPrice`)
- `offering "Example Kubernetes": fixedFees[1]: invalid price: currency "eur", want "EUR"`
  (`ErrInvalidPrice`)
- `offering "Example Kubernetes": fixedFees[0]: price region is not a region of the offering: region
  "de-fra" is not a region of the offering` (`ErrPriceRegion`)
- `offering "Example Kubernetes": fixedFees[1]: overlapping prices: applies to region "fr-par", as
  fixedFees[0] does` (`ErrOverlappingPrices`; the every-region forms as ADR 0024 gives them)
- `offering "Example Kubernetes": fixedFees: offering region has no fixed fee: region "nl-ams"`
  (`ErrMissingFee`)

The exact text may vary; each error is one line, wraps exactly one of the sentinels above, names the
offering, and contains `fixedFees` (with the index for the first four). Values appear only through
`text.Quote` or `Amount.String`. Tests pin the sentinel with `errors.Is` and the presence of the
offering name, `fixedFees` and, for `ErrMissingFee`, the quoted region.

The catalog holds its own copy, as for storage prices: each fee's regions sorted, de-duplicated and nil
when empty, the fees sorted by first region with the every-region fee first, `FixedFees` nil when none.
`clone` copies them, so `Offerings` and `OfferingsFor` stay deep copies: changing a returned fee or its
regions does not change the catalog.

### `targettest` and `SOURCES.md`

- `targettest`: `equalOffering` also compares `FixedFees` (with `equalPrice`), and `describe` shows them
  (`FixedFees: ...`, through `describePrices`), so the `repeat` check covers them. No new check: the
  domain already guarantees the currency, the range, the regions and the coverage, so such a check could
  never be violated. `Expectations` is unchanged.
- `SOURCES.md` (`internal/adapter/target/snapshot/sources_test.go`): the rule that every price amount of
  an offering appears as a code span in its `### <offering name>` section (ADR 0024) covers its fixed
  fees too, as `Amount.String()` gives them (`` `65.70` ``, `` `0.00` ``). For each fee converted from an
  hourly price the section gives the hourly figure, the multiplication and the result, as for plans.

### Estimates (PRD #190 milestone 6)

This ADR adds no estimate arithmetic. Milestone 6 must follow this rule: for a resource estimated on an
offering in the estimate region `r`, when `FixedFee(r)` returns `(fee, true)`, the estimate adds `fee`
**once per resource** (one cluster), never multiplied by the resource's `count` (which for
`kubernetes.managed` is its nodes, ADR 0025), and scaled by `hoursPerMonth / 730` as the plan price is,
since the fees are hourly charges converted to a month. When it returns `(0, false)` nothing is added.
The fee alone never makes an estimate: a resource without a plan price for its region stays "not
estimated", as ADR 0024 and PRD #190 say. Arithmetic is exact and rounded once for display (ADR 0024).

Issue #205's acceptance then holds: one cluster on OVHcloud *Managed Kubernetes Service* in `RBX`
includes `65.70` EUR for the control plane; an offering without `fixedFees` estimates exactly as before.

### Data

In the same change as the domain and reader, OVHcloud's snapshot records the Standard plan's control
plane, €0.09 an hour × 730 = `65.7`:

- *Managed Kubernetes Service* (`GRA`, `LIM`, `RBX`, `SBG`, `WAW`):
  `"fixedFees": [{ "amount": 0, "currency": "EUR", "regions": ["GRA", "LIM", "SBG", "WAW"] },
  { "amount": 65.7, "currency": "EUR", "regions": ["RBX"] }]`. The Free plan's control plane is
  "Included" in `GRA`, `LIM`, `SBG` and `WAW`; `RBX` (EU-WEST-RBX) offers the Standard plan only.
- *Managed Kubernetes Service 3-AZ* (`MIL`, `PAR`), the Standard plan only:
  `"fixedFees": [{ "amount": 65.7, "currency": "EUR", "regions": [] }]`.

Its `SOURCES.md` moves the control-plane fee from *Left out on purpose* into both offerings' *Prices*
with `` `65.70` `` and `` `0.00` `` as code spans. Every other offering of every snapshot is unchanged and
has no `fixedFees` key. Scaleway's and UpCloud's fees are recorded by their own price milestones (PRD
#190 milestones 4 and 5), which have not merged; each lists its control-plane fees under this ADR.

## Alternatives considered

- **Schema version 3 with `fixedFees` required on every offering.** What ADR 0007's policy asks for.
  Rejected: it rewrites every offering of every snapshot to add `"fixedFees": []` (or a fee list), and
  changes the reader's version policy (read 3 only, or 2 and 3), for one datum that only Kubernetes
  offerings have. The snapshots are embedded and no snapshot outside the repository exists (ADR 0009's
  reasoning for its own exception), so no reader can meet a document it misreads.
- **A required key with `[]` meaning no fee**, at version 2. Rejected: every snapshot changes, and an
  empty list would be a second spelling of "no fee" beside the coverage rule.
- **An empty array accepted as "no fee".** Rejected: two spellings for one fact. Absence is the one.
- **`null` read as absent** (as ADR 0025 does for inventories). Rejected: ADR 0007 never reads `null`
  as absent in a snapshot, and one exception is enough.
- **Fees for some regions only, a missing region read as no fee.** Rejected by "never zero for
  unknown": a region with no fee would be ambiguous between free and unknown. Requiring every region,
  with `0` for "Included", states the fact for each region.
- **A single fee, not per region.** Rejected: OVHcloud's *Managed Kubernetes Service* charges in `RBX`
  and not in the Free plan regions, so the fee differs by region within one offering.
- **The fee on a plan** (for example a plan with zero vCPU or a flag). Rejected: a plan is multiplied
  by `count` and matched by size; a control plane is neither, and PRD #190's matching would have to
  skip it everywhere.
- **A separate offering for the control plane.** Rejected: a cluster is one inventory resource matched
  to one offering through the taxonomy; a second offering would need a second match.
- **A new sentinel for every fee error (`ErrInvalidFee`).** Rejected: the fee is checked by the price
  code with the same rules, and the list name in the error already says it is a fee. Only coverage,
  which no other list has, gets a new sentinel.
- **Leave the fee out and have milestone 6 note it in the report** (issue #205's fallback). Rejected:
  the fee is known and published; a note would leave every estimate too low on purpose.

## Consequences

- Snapshots without fees load unchanged; an older binary cannot read a snapshot with fees, which
  matters to no one since snapshots are embedded in the binary that reads them.
- The format now has one optional key. The next format change follows ADR 0007's policy again: this
  exception does not extend to other keys.
- `catalog.Offering` grows by one slice; `clone`, `targettest`'s comparison and `SOURCES.md`'s price
  check cover it.
- OVHcloud Kubernetes estimates in `RBX`, `PAR` and `MIL` include the control plane once milestone 6
  lands; Scaleway and UpCloud must record theirs in milestones 4 and 5.
- Milestone 6 must add `FixedFee` once per estimated resource, scaled by hours, never by `count`.
