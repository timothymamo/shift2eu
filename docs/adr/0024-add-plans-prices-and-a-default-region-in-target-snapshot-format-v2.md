# 0024. Add plans, monthly EUR prices and a default region in target snapshot format version 2

- Status: Accepted. Amends [0007](0007-shared-target-catalog-snapshot-format-v1.md): the snapshot
  format moves to schema version 2, and the shared reader reads version 2 only. Amended by
  [0026](0026-add-an-optional-fixed-monthly-fee-per-offering-to-snapshot-format-v2.md): an offering may
  carry an optional `fixedFees` key, a fixed monthly fee per instance, within schema version 2. Amended by
  [0029](0029-cap-the-billed-hours-of-an-offerings-plans-in-target-snapshot-format-v2.md): an offering
  may carry an optional `planBillingCapHours` key, the most hours a month its plans are billed, within
  schema version 2
- Date: 2026-11-13

## Context

PRD #190 (issue #190) adds plan matching and monthly cost estimates to the assessment. Milestone 2,
"Catalog plans and prices", gives each target's catalog what an estimate needs, before any target
lists a real price (milestones 3 to 5) and before anything estimates (milestone 6):

- per offering, the **plans** a priced category runs on: a name, vCPU, memory and included storage,
  each with a monthly list price in EUR excluding VAT, by region where prices differ;
- per offering, a **per-GiB monthly price** for its storage: block, object or database storage;
- per snapshot, a **default region**: the region estimates use when the user names none (decided by
  the maintainer on 2026-11-13: one region per target, `--region` or else the snapshot's default).

Forces:

- **ADR 0007's schema version policy.** Any change to the snapshot format is a new version and a new
  ADR. The reader is shared, so the new version reaches every target at once. The snapshots are
  embedded, so they can all be rewritten in the same change (ADR 0011).
- **ADR 0007's strictness.** Every key required, no `null`, exact keys, no repeated key, no lone
  surrogate, a size cap, one-line errors that name values by path. A price that is silently dropped,
  defaulted or rounded changes an estimate without a word, as a dropped capability changes a fit.
- **Money is never a float.** `encoding/json` reads a number as `float64`, in which `0.1 + 0.2` is not
  `0.3`. A price list read through a float can disagree with its own source in the last cent, and a
  total of many such prices drifts further. The walker (`jsondoc`, ADR 0009) can return a number's
  JSON text as written (`Walker.Raw`), so the reader can parse it exactly.
- **Never zero for unknown** (PRD #190). A plan without a price must not read as free. An offering
  that publishes no price for a region must say so, not borrow another region's price.
- **Monthly prices only** (decided 2026-11-13). Some targets publish hourly prices; the conversion has
  to happen once, visibly, with a fixed rounding.
- **The domain decides which values are valid** (ADR 0007). `catalog.New` holds the rules for plans and
  prices; the reader only turns JSON text into domain values and checks what only the document can tell
  (the declared regions).
- **Tests come first, from another model family** (ADR 0003). This ADR is the specification the tests
  are written against, so it fixes every key, type, field, sentinel and check label by name.

## Decision

The target snapshot format moves to **schema version 2**. It is version 1 (ADR 0007, as amended by
ADR 0009 and ADR 0011) plus one top-level key, `defaultRegion`, and two offering keys, `plans` and
`storagePrices`. Every other rule of ADR 0007 and ADR 0009 holds unchanged: every key required, no
`null`, exact keys at most once per object, the surrogate rule, `MaxDocumentBytes` (1 MiB), the
declared region code shape, and the shape and wording of errors.

`snapshot.Parse` reads **version 2 only**. A version 1 document is refused with `ErrSchemaVersion`
(`schemaVersion is 1, want the integer 2`). The three embedded snapshots (OVHcloud, Scaleway,
UpCloud) are rewritten to version 2 in the same change, with `"plans": []` and `"storagePrices": []` on
every offering and a provisional default region (see *Rollout*).

### The document

```json
{
  "schemaVersion": 2,
  "target": "Example Cloud",
  "sourceURL": "https://www.example.com/pricing/",
  "retrievedOn": "2026-11-13",
  "regions": ["fr-par", "nl-ams"],
  "defaultRegion": "fr-par",
  "offerings": [
    {
      "name": "Example Compute",
      "categories": ["compute.vm"],
      "capabilities": ["ipv6"],
      "regions": ["fr-par", "nl-ams"],
      "plans": [
        {
          "name": "EC-2-4",
          "vcpus": 2,
          "memoryGiB": 4,
          "storageGiB": 20,
          "prices": [
            { "amount": 12.5, "currency": "EUR", "regions": ["fr-par"] },
            { "amount": 13.0125, "currency": "EUR", "regions": ["nl-ams"] }
          ]
        },
        {
          "name": "EC-1-0.5",
          "vcpus": 1,
          "memoryGiB": 0.5,
          "storageGiB": 0,
          "prices": [{ "amount": 3.65, "currency": "EUR", "regions": [] }]
        }
      ],
      "storagePrices": [{ "amount": 0.04, "currency": "EUR", "regions": [] }]
    }
  ]
}
```

Top level: the fields of ADR 0007, with `schemaVersion` now `2`, and one more:

| Field | JSON type | Meaning |
|---|---|---|
| `defaultRegion` | string | The region estimates use when the user names none. One of the declared `regions`, spelt as declared |

Each offering: the fields of ADR 0007, and two more:

| Field | JSON type | Meaning |
|---|---|---|
| `plans` | array of plan objects; may be empty | The plans the offering sells, in any order |
| `storagePrices` | array of price objects; may be empty | The monthly price of one GiB of the offering's storage: block, object or database storage, depending on the categories it serves. For an offering whose plans include storage, the price of a GiB beyond what a plan includes |

Each plan, every key required:

| Field | JSON type | Meaning |
|---|---|---|
| `name` | string | The target's name for the plan. Non-blank valid text, unique within the offering |
| `vcpus` | integer | Virtual (or, for bare metal, physical) CPU threads. At least 1 |
| `memoryGiB` | number, exact decimal | Memory in GiB, greater than 0, at most 3 fractional digits (`0.5`, `1.875`) |
| `storageGiB` | integer | Storage the plan includes, in GiB. At least 0 |
| `prices` | array of price objects | The plan's monthly prices, at least one |

Each price, every key required:

| Field | JSON type | Meaning |
|---|---|---|
| `amount` | number, exact decimal | The monthly list price excluding VAT, at least 0, at most 4 fractional digits |
| `currency` | string | Exactly `EUR` |
| `regions` | array of strings; may be empty | The offering regions the price applies to. Empty means every region of the offering |

An empty `plans` or `storagePrices` list is a value, not an absence: the offering sells no plan, or
publishes no storage price, that the snapshot records. An estimate for it is "not estimated", never
zero (milestone 6).

### Numbers

JSON numbers in a snapshot are read from their JSON text (`Walker.Raw`), never through `float64`.

**Integers** (`vcpus`, `storageGiB`) match `-?(0|[1-9][0-9]*)` and fit an `int64`. A fraction or an
exponent (`2.0`, `2e0`, `2E+0`) is refused even when its value is whole, as `schemaVersion` refuses
`1.0`. A minus sign is read, so a negative value reaches `catalog.New` and is refused there by rule,
not by grammar. Refused by the reader with `ErrDecode`: a fraction, an exponent, `-0`, a value outside
`int64`, and any non-number (`"2"`, `null`, `true`).

**Exact decimals** (`memoryGiB`, `amount`) match `-?(0|[1-9][0-9]*)(\.[0-9]+)?` with at most 3
fractional digits for `memoryGiB` and at most 4 for `amount`. Trailing zeros count towards the limit
(`1.00000` is refused for `amount`: five fractional digits). The value is stored as an integer count
of the smallest unit (below), so `12.5`, `12.50` and `12.5000` are the same amount. Refused by the
reader with `ErrDecode`: an exponent (`1e2`, `1.5E-1`), more fractional digits than allowed, a zero
with a minus sign in any spelling (`-0`, `-0.0`, `-0.0000`), a value whose integer count of the
smallest unit does not fit an `int64`, and any non-number. A negative value other than zero is read and
refused by `catalog.New` (`ErrInvalidPlan`, `ErrInvalidPrice`), so the rule lives once, in the
domain.

`-0` is refused because it is never what a person compiling a price list means, and an exact reader
that read it as `0` would drop the sign silently.

A refused number is placed by line and column and named by its path, as other walk errors are
(ADR 0007): `offerings[1].plans[0].prices[0].amount` or `offerings[0].plans[2].memoryGiB`. It is shown
through `jsondoc.Describe` (`text.Quote` of the number as written).

### Domain types

`internal/domain/catalog` gains:

```go
// Amount is an exact amount of money in ten-thousandths of the currency unit:
// Amount(125000) is 12.50 EUR. It is never a float.
type Amount int64

// AmountScale is the number of Amount units in one currency unit.
const AmountScale = 10_000

// MaxAmount is the largest Amount a Price may hold: one million currency units.
const MaxAmount Amount = 1_000_000 * AmountScale

// EUR is the only currency a Price may hold.
const EUR = "EUR"

// String formats a as a plain decimal: the integer part, a point, and the
// fraction with trailing zeros dropped but at least two digits kept, with a
// leading minus sign when negative. Amount(125000) is "12.50",
// Amount(130125) is "13.0125", Amount(400) is "0.04", Amount(0) is "0.00".
func (a Amount) String() string

// Price is a monthly list price, excluding VAT.
type Price struct {
	Amount   Amount   // monthly, at least 0, at most MaxAmount
	Currency string   // exactly EUR
	Regions  []string // offering regions it applies to; nil means every one
}

// Plan is one size an offering sells, with its prices.
type Plan struct {
	Name               string
	VCPUs              int64   // at least 1, at most MaxPlanVCPUs
	MemoryMilliGiB     int64   // thousandths of a GiB, at least 1, at most MaxPlanMemoryMilliGiB
	IncludedStorageGiB int64   // at least 0, at most MaxIncludedStorageGiB
	Prices             []Price // at least one
}

const (
	MaxPlanVCPUs          = 1 << 16             // 65,536
	MaxPlanMemoryMilliGiB = (1 << 24) * 1_000   // 16,777,216 GiB
	MaxIncludedStorageGiB = 1 << 30             // 1,073,741,824 GiB
)
```

`Offering` gains `Plans []Plan` and `StoragePrices []Price`. `Snapshot` gains `DefaultRegion string`.

The reader converts `amount` to `Amount` (`12.5` is `Amount(125000)`) and `memoryGiB` to
`MemoryMilliGiB` (`0.5` is `500`, `4` is `4000`). The caps are far above any real plan; they bound the
arithmetic of milestone 6, which must still multiply with an overflow check.

### Validation in `catalog.New`

New sentinels, each in `internal/domain/catalog`:

| Sentinel | Returned for |
|---|---|
| `ErrInvalidPlan` | A plan whose name is blank or not valid text (`text.Validate`), `VCPUs` below 1 or above `MaxPlanVCPUs`, `MemoryMilliGiB` below 1 or above its cap, or `IncludedStorageGiB` below 0 or above its cap |
| `ErrDuplicatePlan` | Two plans of one offering whose names are equal with every run of white space treated as one space, as offering names are compared |
| `ErrUnpricedPlan` | A plan with no price |
| `ErrInvalidPrice` | A price whose amount is below 0 or above `MaxAmount`, whose currency is not exactly `EUR` (no case folding, no trimming), or that lists a region that is blank or not valid text |
| `ErrPriceRegion` | A price that lists a region the offering is not available in (compared exactly, after the offering's regions are validated) |
| `ErrOverlappingPrices` | Two prices of one plan, or two storage prices of one offering, that apply to the same region. A price with no regions applies to every region, so it overlaps any other price of the same list |

`ErrInvalidCatalog` is also returned, as today, for a `Snapshot.DefaultRegion` that is blank or not
valid text. `catalog.New` does not check that the default region is a region of any offering: the
declared regions are not in the domain (ADR 0009), and a catalog with no offerings still has a default
region. The reader checks it against the declared regions.

A plan need not have a price in every region of its offering. "No price for its region" is a valid
"not estimated" reason (milestone 6), and a price list that covers only some regions is the provider's
fact, not an error.

Checks run in this order, per offering, after its name, categories, capabilities and regions (today's
order is unchanged): each plan in input order (name, duplicate, numbers, then its prices in input
order: amount, currency, regions, overlap; then `ErrUnpricedPlan` if it had none), then each storage
price in the same way. A price's regions are de-duplicated before the overlap check: `["fr-par",
"fr-par"]` is one region, as repeated offering regions are (ADR 0007).

Error wording, following today's `offerings[%d]: ...` and `offering %s: ...`:

- `offering "Example Compute": plans[1]: plan has an empty name` (wrapping `ErrInvalidPlan`) and
  `offering "Example Compute": plans[1]: duplicate plan name "EC-2-4"` (`ErrDuplicatePlan`): a plan
  without a usable name is named by its index;
- `offering "Example Compute": plan "EC-2-4": vcpus 0, want at least 1` (`ErrInvalidPlan`) and
  `offering "Example Compute": plan "EC-2-4": plan has no price` (`ErrUnpricedPlan`);
- `offering "Example Compute": plan "EC-2-4": prices[1]: currency "eur", want "EUR"`
  (`ErrInvalidPrice`), and `... prices[1]: region "de-fra" is not a region of the offering`
  (`ErrPriceRegion`), and `... prices[1]: applies to region "fr-par", as prices[0] does`
  (`ErrOverlappingPrices`; with an empty list: `applies to every region, as prices[0] does` or
  `prices[0] applies to every region`);
- `offering "Example Compute": storagePrices[0]: amount -0.01, want at least 0` (`ErrInvalidPrice`).

The exact text may vary; each error is one line, wraps exactly one of the sentinels above, names the
offering and the plan or the list index, and shows values only through `text.Quote` or
`Amount.String`. The tests pin the sentinel with `errors.Is` and the presence of the offering name,
the plan name or list index, and the field name (`vcpus`, `memoryGiB`, `storageGiB`, `amount`,
`currency`, `regions`).

The catalog holds its own copy: each offering's plans sorted by name (`strings.Compare`), each price's
regions sorted, de-duplicated and nil when empty, each plan's prices and each offering's storage prices
sorted by their first region with the every-region price (nil `Regions`) first (the lists are disjoint
once overlaps are refused, so this order is total), and `Plans` and `StoragePrices` nil when empty.
`Offerings` and `OfferingsFor` return deep copies: changing a returned plan, price or regions slice
does not change the catalog. `Snapshot()` returns `DefaultRegion` as given.

### Reader checks

`Parse`'s order (ADR 0007) gains one step and walks the new keys:

1. to 5. As ADR 0007 and ADR 0009, with `schemaVersion` exactly `2`, the new keys in the walk, and the
   number grammar above checked as each number is read.
6. `retrievedOn` is a `YYYY-MM-DD` date.
7. Every declared region is non-blank, valid text, a region code, and declared once.
8. **New:** `defaultRegion` is a declared region, spelt as declared. Otherwise
   `ErrUndeclaredDefaultRegion` (new sentinel in `snapshot`):
   `default region "fr-paris" is not one of the declared regions`. A blank or invalid value fails this
   check too, since no declared region is blank or invalid. A snapshot that declares no region
   therefore cannot be read: it has no region to estimate in.
9. Every offering region is a declared region (`ErrUndeclaredRegion`), as today. A price region is
   not checked against the declared regions: `catalog.New` checks it against the offering's regions,
   which are declared. It holds those regions in a set, so the check stays linear however long or
   repetitive the two lists are: a document under the size cap whose storage price and offering each
   repeat a region about 120 000 times took 19.7 s to parse with a scan of the offering's regions
   per price region, and takes 30 ms with the set (`BenchmarkParseRepeatedPriceRegion`).
10. `catalog.New` accepts the catalog, and its error is returned as it is, so the sentinels above stay
    reachable through `errors.Is`.

`ErrUndeclaredDefaultRegion` is a new sentinel rather than `ErrUndeclaredRegion`, whose meaning is
"offering in an undeclared region" and whose callers may rely on that.

`snapshot.Document` is unchanged: `Catalog` carries the default region in `Catalog.Snapshot().DefaultRegion`.

### Monthly prices and rounding

- A snapshot holds **monthly** list prices only. Where a target publishes an hourly price, the person
  compiling the snapshot multiplies it by **730** hours and rounds the product **half up to 4
  fractional digits** (all prices are non-negative, so half up and half away from zero agree). Example:
  `0.0137` EUR/hour × 730 = `10.001` → `10.001`; `0.00001234` × 730 = `0.0090082` → `0.0090`.
  `SOURCES.md` gives, for each such price, the hourly figure, the multiplication and the stored result.
  The reader cannot tell a converted price from a published one; review and `SOURCES.md` hold the rule.
- **Estimates** (milestone 6 and later) compute at the full stored precision and with exact integer or
  rational arithmetic, never `float64`, and round **once**, to the cent, **half away from zero**, only
  for display. Half away from zero is the rounding people expect on an invoice and the one the targets'
  own price pages use when they show cents; banker's rounding (half to even) would make a displayed
  price differ from the provider's for no reason a reader can see. Rounding each line before summing is
  not allowed: totals are summed at full precision and rounded once.

### `SOURCES.md`

ADR 0007's rules hold, and two more, pinned by `TestEveryOfferingHasASourcesSection` (or a sibling
test in the same file over the same glob):

- In each offering's `### <offering name>` subsection, every plan name and every price amount of the
  offering (plan prices and storage prices) appears as a Markdown code span: the plan name exactly as
  in the catalog, the amount as `Amount.String()` (`` `12.50` ``, `` `13.0125` ``, `` `0.04` ``). An
  offering with no plans and no storage prices needs nothing more.
- A non-blank `## Default region` section names the snapshot's default region as a code span and says
  why it is the default.

### `targettest`

`Expectations` gains `DefaultRegion string`, required like the other fields: an empty one is an
`expectations` violation (`expectations: DefaultRegion is empty, want the region estimates use by
default`). `Check` adds these checks, each violation starting with its label:

- `default-region`: `Catalog.Snapshot().DefaultRegion` equals `want.DefaultRegion`, and is one of
  `want.Regions` (skipped when `want.Regions` is empty).

The contract has no check that every price is in `EUR` at an amount of at least 0, that every plan
has a price, or that every price region is a region of its offering. The domain already guarantees
all three: a `catalog.Catalog` can only be built by `catalog.New`, which refuses each case
(`ErrInvalidPrice`, `ErrUnpricedPlan`, `ErrPriceRegion`), so such a check could never be violated
and no test could pin it. (An earlier draft of this ADR named them `prices` and `price-regions`;
they were dropped while implementing milestone 2.) The `repeat` check's comparison (`diff`,
`equalOffering`) also compares plans, prices and storage prices, and `snapshot` also compares
`DefaultRegion`.

### Rollout

Milestone 2 ships, in one change: the domain types and checks, the version 2 reader (version 1
refused), the `targettest` checks, the `SOURCES.md` checks, and the three snapshots at version 2 with
empty `plans` and `storagePrices` and a **provisional** default region: `GRA` for OVHcloud, `fr-par`
for Scaleway, `de-fra1` for UpCloud, each the first region its snapshot declares. Each
`SOURCES.md` gets its `## Default region` section saying the choice is provisional until its pricing
milestone. Milestones 3 to 5 add the plans and prices and confirm or change the default region with
their reason.

Out of scope for this ADR and milestone 2: plan matching, estimates, the "not estimated" reasons,
`--region` and `--no-estimate`, report and JSON report changes, the CLI and the HTTP API. No golden
file changes: no report shows plans or prices yet.

## Alternatives considered

- **`float64` for prices and memory.** Rejected: binary floating point cannot hold `0.1` exactly, so a
  price read from a file may differ from its source, and sums drift. Every comparison with `SOURCES.md`
  would need a tolerance.
- **Prices as JSON strings (`"12.50"`).** Exact without custom number parsing, and common in payment
  APIs. Rejected: the walker can already return a number's JSON text, so a number is just as exact, and
  a string invites `"12,50"`, `"€12.50"` and other spellings the grammar would have to refuse one by
  one. A number also keeps the snapshot readable.
- **Integer cents in the file (`"amountCents": 1250`).** Exact and simple, but object storage and some
  hourly-converted prices need sub-cent precision (`0.0090` EUR per GiB-month), and a person compiling
  a snapshot copies a price as the provider shows it. Four fractional digits cover both. The domain
  still holds integers.
- **`math/big.Rat` or a decimal library in the domain.** Exact at any precision, but `big.Rat` is a
  pointer type that makes `Price` mutable through a shared pointer, and a library is a third-party
  dependency (CLAUDE.md rule 6). An `int64` of ten-thousandths is exact for every price a target
  publishes, comparable with `==` and cheap to clone.
- **More fractional digits (6 or 8).** Some providers publish hourly prices to 5 or 6 digits. Converted
  to monthly prices, 4 digits keep an error below 0.00005 EUR per unit per month, far below the cent an
  estimate shows. More digits would lower the overflow headroom for milestone 6's arithmetic.
- **Memory in MiB.** `0.5` GiB is 512 MiB, but `0.1` GiB is not a whole number of MiB, and plans are
  published in GiB. Thousandths of a GiB hold every decimal a provider publishes to 3 digits.
- **Accept versions 1 and 2 for a while.** ADR 0007 allows it. Rejected: every snapshot is embedded and
  rewritten in the same change, so no version 1 document is left to read, and two versions would be two
  code paths to test.
- **Optional `plans` and `storagePrices`, absent meaning none.** Rejected by ADR 0007's reasoning: a
  forgotten key would read as "no plans", and an estimate would say "not estimated" for a reason the
  snapshot never stated.
- **A plan priced in every offering region.** Rejected: providers do not sell every plan in every
  region, and refusing a partial price list would force invented prices. Milestone 6 reports "no price
  for its region".
- **The default region checked against offering regions too.** A default region no offering is
  available in makes every estimate "not estimated". But a snapshot with no offerings, as in tests,
  would need an invented offering. The declared check catches typos; review catches a useless default.
- **Default region in `catalog.Catalog` as a method, separate from `Snapshot`.** It describes the
  snapshot's evidence, like the source URL and the date, so it sits with them.
- **Reusing `ErrUndeclaredRegion` for the default region.** Its documented meaning is an offering in an
  undeclared region; a new sentinel keeps both meanings exact.
- **Refusing negative numbers in the reader's grammar.** It would give a negative price `ErrDecode`
  from the reader but `ErrInvalidPrice` from `catalog.New` called directly. Reading the sign and leaving
  the rule to the domain keeps one rule and one sentinel. Only `-0`, which the domain cannot see, is the
  reader's to refuse.

## Consequences

- Every target's snapshot changes in milestone 2, and every later price change is a snapshot change
  with its `SOURCES.md`, reviewed like a capability change.
- Prices stay exact from the file to the estimate. A price in `SOURCES.md` and in the catalog can be
  compared as text through `Amount.String()`.
- `catalog.Offering` is bigger, and `clone` copies plans and prices. `Offerings` and `OfferingsFor` stay
  deep copies.
- An empty snapshot needs at least one declared region and a default region; `TestParseEmptyCatalog`
  and the snapshot test fixtures move to version 2.
- `targettest` callers (the three target adapters) pass `DefaultRegion` in their `Expectations`.
- Milestone 6 computes estimates on `Amount` and `MemoryMilliGiB` with overflow checks, and compares
  an inventory resource's `memoryGiB` (PRD #190 milestone 1) in the same unit, thousandths of a GiB.
- The inventory's `monthlyCost` (milestone 1) is a separate format with its own ADR; it may reuse the
  number grammar here, but nothing in this ADR requires it.
