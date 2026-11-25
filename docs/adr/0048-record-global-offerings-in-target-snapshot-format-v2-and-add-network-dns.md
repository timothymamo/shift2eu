# 0048. Record global offerings in target snapshot format version 2, add network.dns, and keep every format version

- Status: Accepted. Amends [0007](0007-shared-target-catalog-snapshot-format-v1.md) and
  [0024](0024-add-plans-prices-and-a-default-region-in-target-snapshot-format-v2.md): an offering may
  carry an optional `global` key within schema version 2, the third recorded exception to ADR 0007's
  version policy and "every key required" rule, after `fixedFees`
  ([0026](0026-add-an-optional-fixed-monthly-fee-per-offering-to-snapshot-format-v2.md)) and
  `planBillingCapHours` ([0029](0029-cap-the-billed-hours-of-an-offerings-plans-in-target-snapshot-format-v2.md));
  a global offering lists no region, against ADR 0007's "at least one declared region". Amends
  [0025](0025-json-inventory-file-format-v2.md) by one size-field row only (`network.dns` takes no
  field); the inventory's global resources need no change to it. Adds two optional keys to the version
  3 JSON report under [0018](0018-version-the-json-report-format-for-the-cli-and-the-api.md)'s rule
- Date: 2026-11-25

## Context

PRD #581 ([prds/581-global-services-dns.md](../../prds/581-global-services-dns.md)), the follow-up
PRD #552's decided question 2 asked for, adds public DNS zones. Two things stand in its way:

- **The catalog has no way to say "global".** Every offering lists at least one region
  (`catalog.ErrNoRegion`), and every offering region is one of the snapshot's declared EU regions
  ([ADR 0007](0007-shared-target-catalog-snapshot-format-v1.md), `ErrUndeclaredRegion`). A public DNS
  zone is served by a provider's authoritative name servers, often anycast across several countries,
  and bound to no region the user picks. Listing it in every declared region would claim, region by
  region, a residency it does not have; leaving it out makes every DNS zone fit `none`.
- **DNS zones are `unclassified`.** Route 53 hosted zones, Cloud DNS managed zones and Azure DNS zones
  reach the inventory as `unclassified` rows, public and private alike, so a zone is a blocker on
  every target although OVHcloud and Scaleway host one.

Today `--region <target>=<region>` must name one of `Catalog.Regions` (`analysis.ErrUnknownRegion`),
scopes availability through `mapping.MapIn`, which drops every offering not `AvailableIn` the region,
and is the estimate region ([ADR 0027](0027-match-plans-and-estimate-monthly-prices-in-a-domain-package.md)).
No report shows an offering's regions.

No live source adapter exists yet, so a category costs one guide and the catalogs today, and an adapter
and a format version later (ADR 0025, *Consequences*). The region model is the part later global
services would reuse. The maintainer decided the PRD's open questions as recommended: option A (1), the
JSON keys `bestOfferingGlobal` and `globalTies` (2), and the capabilities `dnssec` (3) and `dns-routing`
(4), with zones not priced (5).

Forces: ADR 0007's strictness and version policy, as ADRs 0026 and 0029 weighed them; one spelling per
fact (ADR 0026); no claim a snapshot's sources do not state (ADR 0011); and a report a reader checking
data residency can trust.

## Decision

A snapshot offering may be **global**: `"global": true` with `"regions": []`, served from the
provider's own locations and listed in no declared region. Only a category the taxonomy marks global
may have one (`network.dns` alone). A global offering is available in every region, so `--region` never
drops it, and every report marks it "(global)". The taxonomy gains `network.dns` (public authoritative
zones) and the capabilities `dnssec` and `dns-routing`. The snapshot format stays at version 2, the
inventory format at version 2, and the JSON report at `schemaVersion` 3.

### The region model: option A

| Option | Snapshot | Mapping and `--region` | Residency claim | Verdict |
|---|---|---|---|---|
| **A. Explicit marker**: `"global": true`, `"regions": []` | One optional offering key, as `fixedFees` and `planBillingCapHours` | `AvailableIn` is true in every region: in scope whatever `--region` says | None: in no declared region, and the report says global | **Chosen** |
| B. Empty `regions` means global | No new key | As A | None | An offering whose author forgot its regions is silently global; `ErrNoRegion` can no longer catch it |
| C. A pseudo-region `global` | A declared region named `global` | Only with `--region <target>=global`; any real region drops the zone | Misleading: `global` listed among the EU regions, on the site's region choice, possibly as `defaultRegion` | A special case wherever a region is read |
| D. Every declared region | No change | Works with no code change | Claims, for each EU region, a service that is not there; drifts when a region is added | The snapshot states what its sources do not |
| E. A separate list of global offerings | A new top-level key | Its own path through mapping | None | Two offering shapes, two validation paths, two places the reports look |

A is the only option that claims no residency, keeps `ErrNoRegion` catching a forgotten region list,
and keeps one offering shape.

### The snapshot (amends ADR 0007 and ADR 0024)

```json
{
  "name": "DNS zone",
  "categories": ["network.dns"],
  "capabilities": ["dnssec"],
  "regions": [],
  "global": true,
  "plans": [],
  "storagePrices": []
}
```

| Field | JSON type | Required | Meaning |
|---|---|---|---|
| `global` | boolean, `true` only | **no** | The offering is served from the provider's own locations, bound to no region the user chooses |

- **Absent**: regional, as every offering before this ADR. Every existing snapshot reads unchanged and
  gives the same `catalog.Catalog` (`Global` false on every offering), and its parsed-catalog golden
  does not change.
- **`true`**: `regions` stays required and is `[]`; a non-empty list is refused
  (`catalog.ErrGlobalRegion`). Every price and fixed fee of the offering has `"regions": []` and so
  applies in every region; a price or fee that names a region is refused (`catalog.ErrPriceRegion`, as
  a region outside a regional offering's is). A global offering's fixed fees are one fee with
  `"regions": []`.
- **`false`** is refused by the reader (`snapshot.ErrDecode`), as a written `0` cap and an empty
  `fixedFees` are: "regional" has one spelling, the key left out. The reader refuses it **after the
  offering object is read**, so the error names the offering and the key's path and position, and says
  to leave the key out for a regional offering (`offering "DNS zone": "offerings[0].global" at ... is
  false, want true (leave the key out for a regional offering)`; the exact text may vary). `null` is refused as a value of the wrong type, as everywhere.
- A regional offering keeps at least one declared region: `"regions": []` without `global` is still
  `ErrNoRegion`.
- The schema version stays **2**. The key is optional, every existing snapshot reads unchanged, and
  the snapshots are embedded in the binary that reads them, so no older reader meets one. This is the
  third recorded exception to ADR 0007's version policy, after `fixedFees` (ADR 0026) and
  `planBillingCapHours` (ADR 0029).

### Only global categories may be global

`taxonomy.IsGlobal(c)` reports whether offerings of `c` may be global; `network.dns` is the only such
category, and any other value, one outside the vocabulary included, is not. `catalog.New` refuses a
global offering that serves any other category with `catalog.ErrGlobalCategory`, as it refuses a plan
without vCPUs outside `cache.redis` ([ADR 0046](0046-add-seven-taxonomy-categories-and-match-cache-plans-on-memory.md)),
so a regional product cannot be recorded as global to dodge the region rule. A global category may
still have regional offerings. `targettest`'s `checkGlobalCategories` holds every adapter's snapshot to
the same rule, and `equalOffering` and `describe` carry the flag, so the parsed-catalog goldens show it.

`catalog.New` checks `ErrGlobalCategory` **before** `ErrGlobalRegion`: an offering that is wrong on
both counts is reported for the category, the more fundamental mistake, since moving its regions
would not make it valid.

### The catalog and the mapping

- `catalog.Offering.Global bool`, false the regional default. On a global offering
  `AvailableIn(r)` is true for every non-empty `r`, and `AvailableIn("")` stays false: no region
  asked, no availability claimed, as for a regional offering. That is safe because `--region` is
  checked against `Catalog.Regions` before any mapping, so the only regions `AvailableIn` meets are
  the target's declared ones and its estimate region.
- `PlanPrice`, `StoragePrice` and `FixedFee` of a global offering apply in every non-empty region.
  `FixedFee` now asks `AvailableIn` instead of comparing the offering's regions itself, which would have
  given a global offering no fee anywhere. The estimate's `offeredIn` and `plansOfferedIn` go through
  `AvailableIn` already.
- `Catalog.Regions` is unchanged: the regional offerings' regions and the default region. A global
  offering adds no region, so `--region` accepts exactly today's regions, `--region ovhcloud=global`
  is refused as any unknown region is (exit 2), the site's region choice and the error listing a
  target's regions gain no entry, and `defaultRegion` stays a real region.
- `mapping.MapIn` never drops a global offering, so a global offering never gives "not offered in
  <region>". Listing global offerings apart from the region's would show the same zone as served and
  not served at once.
- An estimate prices a global offering in the estimate region, whatever it is. `network.dns` is not
  priced, so no amount depends on this yet; the rule is for a later priced global category.

### The inventory's global resources

The inventory already carries a global resource: `resources[].region` is absent for one, the domain
allows an empty region, and the guide's jq programs already turn a global location into no region.
Mapping never compares a resource's region with an offering's. So a DNS zone is written with no
`region`, inventory schema version 2 suffices, and ADR 0025's fields and versions do not change for
it.

### What "(global)" means, and where the reports mark it

For a reader checking data residency, **"(global)" means "not bound to a region you choose: the
provider serves it from its own locations"**, which the target's `SOURCES.md` names as its pages state
them. Each report says so once, in a note under the first table that shows a global offering (Markdown
and HTML) or in the schema's description of the keys (JSON), pointing to `SOURCES.md`. The report's
region line is unchanged: it states the region chosen or estimated for.

Every report marks the same offerings, the best offering and each of its ties:

- **Markdown, single target**: the best-offering cell and its ties, and the required-changes list's
  "(not offered by X)" when X is global.
- **Markdown, comparison**: a target's best-offering cell in the comparison table (which names no ties);
  the per-target sections reuse the single-target renderers.
- **The site's HTML report**: the best offering and its ties, in the single-target report and in each
  target's section of a comparison, and the required-changes list's "(not offered by X)".
- **JSON, version 3**, assessment and comparison: an optional `bestOfferingGlobal: true` beside
  `bestOffering` when it is global, and an optional `globalTies`, the names of the global ties in the
  order of `ties`; each key is absent when it would be false or empty.

The blockers' "no offering in the <target> catalog for <category>" names no offering, and an estimate
names a plan whose offering is the marked best offering of the same resource, so neither is marked.

### The category `network.dns`

"A public DNS zone the provider's authoritative name servers serve on the internet: the records of one
domain or subdomain, delegated to the provider by NS records at the parent." One resource per zone:
Route 53 public hosted zones, Cloud DNS managed zones of visibility `public`, Azure DNS zones
(`Microsoft.Network/dnszones`). Named after the kind of product, as `network.loadbalancer`, in the
`network` family ADR 0046 started.

Out, and why:

- **Private zones** (Route 53 private hosted zones, Cloud DNS private, forwarding and peering zones,
  `Microsoft.Network/privateDnsZones`) stay `unclassified`: a private zone answers only inside the
  networks it is attached to and moves with the network design, and no target sells a user-managed
  private zone. A category with no offering anywhere would say nothing `unclassified` does not.
- **Resolvers and resolver rules** stay `unclassified`: recursive resolution inside a network, not
  hosting a zone; every target network resolves on its own.
- **Health checks, traffic policies and Azure Traffic Manager** stay `unclassified` resources; what a
  zone's records depend on them for is `dns-routing`.
- **Domain registrations** are not zones and are not assessed: a registrar transfer is not a catalog
  offering.

`network.dns` takes **no size field** and no `usage` (refused with `inventory.ErrFieldNotForCategory`,
named by its path, as on `network.loadbalancer`), and is **never priced**: it joins
`network.loadbalancer` and `registry.container` behind `estimate.NeverPriced`, "not priced" in Markdown
and HTML and `not-priced` in JSON. A zone costs a few euros a year and is billed per zone and per query,
which no inventory field records; pricing it would need a per-resource fee no category has. ADR 0025's
size-field table gains the row:

| Category | `vcpus` | `memoryGiB` | `storageGiB` | `count` | `usage.hoursPerMonth` |
|---|---|---|---|---|---|
| `network.dns` | no | no | no | no | no |

### The capabilities `dnssec` and `dns-routing`

- **`dnssec`**: "the provider signs the zone with DNSSEC and publishes the keys the parent's DS record
  points at". A signed zone moved to a provider that cannot sign it must be unsigned first (the DS
  record removed at the registrar), or validating resolvers fail it. Derived read-only from each
  source's signed state, never its keys.
- **`dns-routing`**: "the zone's answers depend on the client's location, on weights or on the health of
  the endpoints". Route 53 weighted, latency, failover, geolocation, IP-based and multivalue records and
  Cloud DNS routing policies answer differently by client or by health; a target with plain records
  would otherwise map `direct` and hide that failover stops working. Derived from a count of routed
  record sets per zone, with no record data written.

No other DNS capability: every source zone is served over IPv6, so a derived `ipv6` would be on every
zone and mark targets `partial` for something no user chose. An alias record to a resource of the same
source is a required change the guide names, not a capability.

### No format version changes

- **Snapshot: version 2**, with the optional `global` key (above).
- **Inventory: version 2.** `taxonomy.ParseCategory` and `ParseCapability` accept the new values in a
  version 1 or 2 file, as ADRs 0023, 0046 and 0047 added theirs; the field table gains a row for the new
  category only. An older binary refuses an inventory with `network.dns` with "unknown taxonomy
  category".
- **JSON report: `schemaVersion` 3**, with `bestOfferingGlobal` and `globalTies` as optional keys under
  ADR 0018's rule for an optional key; `assessment-v3.schema.json` and `comparison-v3.schema.json`
  declare both, optional, in the same change. Version 2 never writes either key, and the frozen version
  1 and 2 schema files are unchanged (`TestFrozenSchemaFilesAreUnchanged`). `category` and capability
  values are strings without an `enum`, so `network.dns`, `dnssec` and `dns-routing` need no version.

### Decisions taken while implementing

- `ErrGlobalCategory` is checked before `ErrGlobalRegion` (*Only global categories may be global*).
- `"global": false` is refused once the offering object is read, so the error names the offering, as
  `planBillingCapHours: 0` is.
- The GCP sample's existing Cloud DNS managed zone, `shop-zone`, is reclassified as `network.dns` with
  `dnssec` and `dns-routing` and no region, instead of adding a resource, so the samples together use
  every category and capability again and only the GCP sample's goldens change. No snapshot serves
  `network.dns` yet, so every target shows it as fit `none`, "no offering in the <target> catalog for
  `network.dns`", until milestones 5 and 6.

## Alternatives considered

- **Options B to E** for the region model, above.
- **Snapshot schema version 3.** What ADR 0007's policy asks for. Rejected for the reasons ADRs 0026 and
  0029 gave: every offering of every snapshot would be rewritten for a datum one offering per target
  has, and the snapshots are embedded, so no reader outside the binary meets them.
- **Accept `"global": false` as "regional".** Two spellings for one fact, as ADR 0026 refused an empty
  `fixedFees` and ADR 0029 a `0` cap.
- **Any category may be global.** Lets a regional product be recorded as global and escape the region
  rule; the taxonomy deciding which kinds of product are global keeps the claim checkable.
- **A global region in the inventory.** The inventory already says "global" by leaving `region` out, and
  mapping never reads it.
- **Mark only the best offering in JSON (`bestOfferingGlobal` alone).** Hides a global tie from every
  reader, and Markdown and HTML would have to drop their tie marks to match.
- **A JSON offering object instead of a name (`schemaVersion` 4).** A breaking change for one flag.
- **No capability, and a guide note for DNSSEC and routing.** A signed or routed zone would map `direct`
  to a target that cannot serve it, which is exactly what a capability exists to catch.
- **A `network.privatedns` category.** No target sells one; it would say only what `unclassified`
  says and cost every adapter a category.

## Consequences

- `internal/domain/taxonomy` gains `NetworkDNS`, `DNSSEC`, `DNSRouting` and `IsGlobal`;
  `internal/domain/catalog` gains `Offering.Global`, `ErrGlobalCategory` and `ErrGlobalRegion`, and
  `FixedFee` goes through `AvailableIn`; the snapshot reader accepts `global`; `targettest` checks global
  categories and prints the flag; the three reports mark global offerings, and the version 3 JSON
  schemas declare the two keys.
- A target can now serve `network.dns` with one global offering and claim no EU region for it. OVHcloud
  (milestone 5) and Scaleway (milestone 6, gated on saved pages) add theirs, each with a `SOURCES.md`
  section saying where its name servers are; UpCloud hosts no zones and records that.
- A later global category (a CDN, transactional email) joins by being added to `taxonomy.IsGlobal` and
  reuses every rule here, the residency note included. A priced global category would rely on the
  estimate rule above, which no amount exercises yet.
- The snapshot format now has three optional offering keys. Any further change follows ADR 0007's policy
  unless an ADR records another exception.
- The guide's per-source classification of zones (milestones 2 to 4) is still to come: until then a
  user sets a zone's category by hand.
