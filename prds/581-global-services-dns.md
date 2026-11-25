# PRD #581: Global services and public DNS zones

**Status:** Draft
**Issue:** #581

## Problem

PRD #552 ([prds/552-more-taxonomy-categories.md](552-more-taxonomy-categories.md)) left DNS zones and
CDNs out, and its decided question 2 asked for this follow-up: "a follow-up PRD on global services that
first decides how a snapshot records an offering with no region (and what `--region` does with it),
then adds public DNS zones (`network.dns`, OVHcloud and Scaleway). CDNs stay out even then."

Two things stand in the way today:

- **The catalog has no way to say "global".** Every offering lists at least one region
  (`catalog.ErrNoRegion`, `internal/domain/catalog`), and every offering region must be one of the
  snapshot's declared EU regions (ADR 0007, `ErrUndeclaredRegion`). A public DNS zone is served by a
  provider's set of authoritative name servers, often anycast across several countries, and is bound to
  no region the user picks. Listing it in every declared region would claim, region by region, a
  residency it does not have; leaving it out is what makes every DNS zone fit `none` today.
- **DNS zones are `unclassified`.** The inventory guide (`docs/guide/inventory.md`, *Everything else on
  AWS*) names "Route 53 zones" among the services that "should show up as `unclassified`". Route 53
  hosted zones reach the inventory only through AWS Config, Cloud DNS managed zones through Cloud Asset
  Inventory, and Azure DNS zones through Resource Graph, each as an `unclassified` row with no
  capability, public and private zones alike. A platform engineer moving a workload sees a blocker for
  every zone, although OVHcloud and Scaleway host a customer's zone, and the report cannot name the
  offering or what the move must change (a signed zone, routing policies).

A DNS zone is almost always part of a move: every public workload has one, and moving it is the step
that switches traffic. Now is the time for the same reason as PRD #552's: no live source adapter
exists, so a category costs one guide and the catalogs today, and every adapter and a format version
later (ADR 0025, *Consequences*). The region model is needed first, and it is the part later global
services would reuse.

## Outcome

A snapshot can record an offering as **global**: served from no region the user chooses, and never
listed as being in a declared region. The mapping treats a global offering as available in every
region, so `--region` never makes a global offering "not offered in <region>"; reports mark every global
offering they name as global, so the reader knows no region was claimed for it. A user who builds an
inventory with the guide gets a new taxonomy category, `network.dns`: their Route 53 public hosted
zones, Cloud DNS public managed zones and Azure DNS zones are classified, with the DNS features they rely
on as capabilities, and `shift2eu assess` maps them to OVHcloud's and (once its pages are read)
Scaleway's DNS zone offerings with fit `direct` or `partial`, and to fit `none` on UpCloud, which hosts
no zones. Zones are not estimated, with the reason "not priced". Private zones and resolvers stay
`unclassified`, CDNs stay out. The inventory format stays at version 2, the catalog snapshot format at
version 2 (with one optional key, as ADRs 0026 and 0029 added theirs), and the JSON report keeps its
`schemaVersion`s.

## Scope

| | In scope | Out of scope |
|---|---|---|
| Source providers | None live. The inventory file (schema versions 1 and 2) and the inventory guide's commands, jq programs and tables for **AWS**, **GCP** and **Azure** | Live discovery (`shift2eu discover`) |
| Target providers | **OVHcloud** and **Scaleway**: a global DNS zone offering in their embedded snapshots, with sources. **UpCloud**: its absence recorded in `SOURCES.md` | New targets. Live catalog or pricing APIs. Domain name registration |
| Region model | Global offerings in the snapshot (format version 2, one optional offering key), the catalog domain, the mapping, the estimates, `--region` and the Markdown, JSON and HTML reports | Global regions in the inventory beyond today's absent `region`. A region per name server or point of presence |
| Taxonomy categories | New: `network.dns` (public authoritative DNS zones). The existing categories keep their meaning | Private zones, resolvers and resolver rules, DNS firewalls, health checks and traffic policies as resources; CDNs; every other global service (*Later candidates*) |
| Capabilities | Two new ones proposed, `dnssec` and `dns-routing` (open questions 3 and 4) | Any other DNS capability (IPv6 transport, record types, zone transfers, query logging) |
| Inventory | Schema version 2; `network.dns` takes no size field | A schema version 3 |
| Assessment and output | The current fit rules; `network.dns` not estimated as "not priced"; the global mark in Markdown, JSON and the site's HTML report | A JSON report `schemaVersion` bump. Per-zone or per-query pricing |

### The region model

What a global offering is, and how each layer treats it. Today: an offering has `regions`, at least one,
each a declared region (ADR 0007); `Catalog.Regions` lists every offering's regions and the default
region; `--region <target>=<region>` must name one of those regions (`analysis.ErrUnknownRegion`) and
scopes availability with `mapping.MapIn`, which drops every offering not `AvailableIn` the region, and
is the estimate region (PRD #190, ADR 0027); the Markdown, JSON and HTML reports show the region chosen
or the estimate region, never an offering's regions.

The options for recording a global offering:

| Option | Snapshot | Mapping and `--region` | Residency claim | Cost |
|---|---|---|---|---|
| **A. Explicit marker** (recommended): `"global": true`, `"regions": []` | One optional offering key, as `fixedFees` (ADR 0026) and `planBillingCapHours` (ADR 0029) were added within format version 2 | `AvailableIn` is true in every region: in scope whatever `--region` says | None: the offering is in no declared region, and the report says global | One key, one rule in `catalog.New`, one in `AvailableIn`, one mark in each report |
| B. Empty `regions` means global | No new key; `regions` may be empty | As A | None | An offering whose author forgot its regions is silently global; `ErrNoRegion` can no longer catch it |
| C. A pseudo-region `global`, declared in `regions` | No new key; a declared region named `global` | Only with `--region <target>=global`; any real region drops the zone ("not offered in `fr-par`") | Misleading: `global` is listed among the target's EU regions, in `Catalog.Regions`, on the site's region choice, and could be a `defaultRegion` | A special case in region checks everywhere a region is read |
| D. Every declared region | No change | Works with no code change | Claims, for each EU region, a service that is not there; drifts whenever a region is added | Silent; the snapshot states something its sources do not |
| E. A separate list of global offerings | A new top-level key | Its own path through mapping | None | Two offering shapes, two validation paths, two places the reports look |

**Recommended: A.** The decisions, which milestone 1's ADR records:

- **Snapshot.** An offering may carry `"global": true`. A global offering has `"regions": []` (the key
  stays required; a non-empty list beside `global: true` is refused), and every price and fixed fee of
  it applies to every region (`"regions": []` in each); `global: false` is refused as meaningless, as
  an absent key is the regional default. A regional offering keeps at least one declared region. The
  format stays at version 2: the key is optional, every existing snapshot reads unchanged, and the
  snapshots are embedded in the binary that reads them, so no older reader meets one. This is the third
  recorded exception to ADR 0007's version policy, after `fixedFees` and `planBillingCapHours`.
- **Only global categories may be global.** The taxonomy marks the categories whose offerings may be
  global (`network.dns` alone in this PRD), and `catalog.New` refuses a global offering of any other
  category, as it refuses a plan without vCPUs outside `cache.redis` (ADR 0046). So a regional product
  cannot be recorded as global to dodge the region rule. A category so marked may still have regional
  offerings.
- **Catalog.** `Offering.Global` (or an equivalent the milestone chooses). On a global offering,
  `Offering.AvailableIn(r)` is true for every non-empty `r`, and `AvailableIn("")` stays false, as for a
  regional offering (no region asked, no availability claimed). That is safe because `--region` is
  checked against `Catalog.Regions` before any mapping (`analysis.ErrUnknownRegion`), so the only regions
  `AvailableIn` meets are the target's declared ones and its estimate region. `Catalog.Regions` stays the
  declared regions the regional offerings and the default region use, so a global offering adds no
  region, and `--region` keeps accepting exactly today's regions. `defaultRegion` stays a real region.
  Every reader of an offering's regions follows the same rule: `AvailableIn`, `Offering.priceIn`
  (`internal/domain/catalog/lookup.go`, under `PlanPrice` and `StoragePrice`), `Offering.FixedFee`
  (`internal/domain/catalog/catalog.go`, used by the estimate), which today checks
  `slices.Contains(o.Regions, region)` itself and so would give a global offering no fee in any region,
  the estimate's `offeredIn` and `plansOfferedIn` (`internal/domain/estimate`), which go through
  `AvailableIn`, and `targettest`'s offering equality and dump (`equalOffering`, `describe`), which must
  carry the global flag so the parsed-catalog goldens show it; the doc comments of `AvailableIn`,
  `PlanPrice`, `StoragePrice` and `FixedFee` say what a global offering does. The snapshot doc's rule
  that fixed fees cover every region (`internal/adapter/target/snapshot/doc.go`) gains the global case.
- **Mapping and `--region`.** A global offering is a candidate in every region, so it is never dropped by
  `MapIn` and never gives "not offered in <region>". The alternative, listing global offerings
  separately from the region's, would show the same zone as served and not served at once.
- **Estimates.** A global offering's prices apply in the estimate region, whatever it is. (`network.dns`
  is not priced in this PRD, so no amount depends on it yet; the rule is for a later priced global
  category.)
- **Reports.** Wherever a report names an offering, a global one is marked, and the three formats mark
  the same offerings: the best offering and each of its ties. "(global)" means, for a reader checking
  data residency, "not bound to a region you choose": the provider serves it from its own locations,
  which the target's `SOURCES.md` names as its pages state them. Milestone 1's ADR records that
  definition, and each report says it once, in a note under the first table that shows a global
  offering (Markdown and HTML) or in the schema's description of the key (JSON), pointing to
  `SOURCES.md`. The places, all changed in milestone 1:
  - Markdown, single target: the best-offering cell with its ties (`renderBestOffering`,
    `internal/adapter/report/markdown/report.go`) and the required-changes list's "(not offered by X)"
    (`report.go`, the `not offered by` line).
  - Markdown, comparison: the per-target best-offering cell of the comparison table
    (`internal/adapter/report/markdown/comparison.go`, the `v.BestOffering` cell), which names no ties
    today and gains none; the per-target sections reuse the single-target renderers.
  - The site's HTML report: `renderBestOffering` (`internal/adapter/report/htmlreport/report.go`), used by
    the single-target report and by each target's section of a comparison, and the required-changes
    list's "(not offered by X)" (`report.go`).
  - JSON, version 3 only, assessment and comparison alike (`assessment-v3.schema.json`,
    `comparison-v3.schema.json`, beside their `bestOffering` and `ties`): an optional
    `bestOfferingGlobal: true` when the best offering is global, and an optional `globalTies`, the
    names of the ties that are global in the order of `ties`; each key is absent when it would be false
    or empty. That keeps `schemaVersion` 3 under ADR 0018's rule for an optional key, with the version 3
    schema files updated in the same change (open question 2). Version 2 never writes either key;
    the frozen version 1 and 2 schema files are unchanged.
  - Exceptions, which name no offering: the blockers' "no offering in the <target> catalog for
    <category>" reason names a target and a category; an estimate names a plan, whose offering is the
    marked best offering of the same resource. Milestone 1 checks that no other place names an offering.

  The report's region line is unchanged: it states the region chosen or estimated for, and a global
  offering does not change it.
- **Source side.** The inventory already carries a global resource: `resources[].region` is "absent for
  a global resource" (`docs/guide/inventory.md`, *The inventory format in brief*), the domain allows an
  empty region (`internal/domain/inventory`), and the jq programs already turn a global location into no
  region (`aws.jq`: AWS Config's `awsRegion` `global`; `gcp.jq`: `location_region`; `azure.jq`: `location`
  `global`). Mapping never compares a resource's region with an offering's ("where a resource runs today
  says nothing about which target region it must move to", `internal/domain/mapping`). So a Route 53 zone,
  a Cloud DNS zone and an Azure DNS zone (location `global`) are written with no `region`, and inventory
  schema version 2 suffices: no field, no version, no ADR 0025 amendment.

### The new category

The definition is what the taxonomy's doc comment will say (`internal/domain/taxonomy/category.go`).

| Category | Definition | OVHcloud | Scaleway | UpCloud |
|---|---|---|---|---|
| `network.dns` | A public DNS zone the provider's authoritative name servers serve on the internet: the records of one domain or subdomain, delegated to the provider by NS records at the parent | DNS zone (Web Cloud "Domains", standalone or with a domain), global | Domains and DNS, external or Scaleway-registered domain, global (to be checked from saved pages) | none |

**What counts.** Route 53 public hosted zones (`Config.PrivateZone` false), Cloud DNS managed zones of
visibility `public`, and Azure DNS zones (`Microsoft.Network/dnszones`; zone type `Public`, the only one
Azure creates since private zones became `Microsoft.Network/privateDnsZones`). One resource per zone.

**What does not, and why.**

- **Private zones** (Route 53 private hosted zones, Cloud DNS `private` zones, including forwarding and
  peering zones, `Microsoft.Network/privateDnsZones`) stay `unclassified`: a private zone answers only
  inside the source's networks it is attached to, so it moves with the network design (a target VPC's
  internal DNS, or a resolver the user runs), and no target sells a user-managed private zone as a
  product (to be checked in milestones 5 and 6: Scaleway's VPC DNS resolves its own resources only).
  Mapping one to a public zone would be wrong; giving it a category with no offering anywhere would say
  nothing `unclassified` does not.
- **Resolvers and resolver rules** (Route 53 Resolver endpoints and rules, Cloud DNS server policies and
  response policies, Azure DNS Private Resolver) stay `unclassified`: recursive resolution inside a
  network, not hosting a zone; every target network resolves on its own (UpCloud: "UpCloud network
  includes a Domain Name Service resolver at each location").
- **Route 53 health checks and traffic policies, Azure Traffic Manager** stay `unclassified` resources;
  what a zone's records depend on them for is the `dns-routing` capability (open question 4).
- **Domain registrations** (Route 53 Domains, Cloud Domains, App Service Domains) are not zones and are
  not assessed: moving a registration is a registrar transfer, which no target catalog here records.

### The target offerings

Found on 2026-11-25 (milestones 5 and 6 re-read each page and cite it in `SOURCES.md`):

- **OVHcloud: DNS zone.** A zone can be created on its own, "Standalone DNS zone", for a domain not
  registered with OVHcloud: the zone is activated by pointing the domain's NS records at OVHcloud's name
  servers at the registrar ("you can only have one active DNS zone for your domain name").
  <https://docs.ovhcloud.com/en/guides/web-cloud/domains/dns-zone-create>. Record types include "A, AAAA,
  CNAME, DNAME, NS" and the text, email and extended families.
  <https://docs.ovhcloud.com/en/guides/web-cloud/domains/dns-zone-general-information>. DNSSEC: three
  cases, the third "not registered with OVHcloud, using OVHcloud DNS", enabled through the API
  (`POST /domain/zone/{zoneName}/dnssec`) and the keys passed to the registrar.
  <https://docs.ovhcloud.com/en/guides/web-cloud/domains/dns-dnssec>. Anycast DNS is an option, "€1.09
  ex. VAT/year/Domain", "in four points of presence around the world"; without it, records are "usually
  stored in a single location". <https://www.ovhcloud.com/en-ie/domains/options/dns-anycast/>. So the
  offering is global either way: the name servers are OVHcloud's, outside the region model of its Public
  Cloud (no `GRA`, `SBG` or `DE1` is chosen), and the anycast servers are "spread across multiple
  countries". No page found offers geolocation, weighted or failover answers (milestone 5 checks), so no
  `dns-routing`.
- **Scaleway: Domains and DNS.** PRD #552 recorded that Scaleway hosts external zones by API. On
  2026-11-25 every documentation page tried answered HTTP 403 to an automated client,
  <https://www.scaleway.com/en/docs/domains-and-dns/faq/> and
  <https://www.scaleway.com/en/docs/domains-and-dns/concepts/> among them, so nothing about it is
  confirmed here: milestone 6 is gated on the maintainer's saved pages (*Pages the maintainer must save
  by hand*), which must state external domains, DNSSEC, and dynamic records (geolocation, weighted or
  health-checked answers) if any.
- **UpCloud: none.** "We do not currently offer a user-configurable authoritative DNS service for
  hosting your domain names." Its network has resolvers ("a Domain Name Service resolver at each
  location") and reverse DNS per IP address, neither of which hosts a zone.
  <https://upcloud.com/docs/products/networking/dns/>. UpCloud's `SOURCES.md` *Left out on purpose*
  records it.

### How the sources classify

The `rawType` keeps the guide's conventions: the CloudFormation type on AWS, the Cloud Asset Inventory
asset type on GCP, the Resource Manager type on Azure. Every zone is written with no `region`.

| Source | `network.dns` | Stays `unclassified` | Calls (read-only) | Permissions |
|---|---|---|---|---|
| AWS | `AWS::Route53::HostedZone` with `Config.PrivateZone` false | the same type with `PrivateZone` true; `AWS::Route53::HealthCheck`, `AWS::Route53Resolver::*`, `AWS::Route53Profiles::*` (from AWS Config, as today) | `route53 list-hosted-zones` (`--query` keeping `Id`, `Name` and `Config.PrivateZone`); per public zone, by its ID (`--hosted-zone-id "$id"`, never its name), `route53 get-dnssec` (`--query Status.ServeSignature` only, never the key-signing keys); for `dns-routing`, per public zone `route53 list-resource-record-sets --hosted-zone-id "$id"` reduced to a count of routed record sets, `--query 'length(ResourceRecordSets[?SetIdentifier \|\| HealthCheckId \|\| MultiValueAnswer])'` (milestone 2 confirms the predicate), so no record's name, values, set identifier, health check ID or routing settings are written | `route53:ListHostedZones`, `route53:GetDNSSEC`; `route53:ListResourceRecordSets` optional, only for `dns-routing` |
| GCP | `dns.googleapis.com/ManagedZone` with `visibility` `public` | the same type with visibility `private`; `dns.googleapis.com/Policy`, `dns.googleapis.com/ResponsePolicy` (from the asset search, as today) | The asset search's `ManagedZone` row stays the resource, as every GCP resource is (`gcp.jq`'s `details(...)` pattern); per project `gcloud dns managed-zones list --project="$project"` adds its details, `--format='json(kind,name,visibility,dnssecConfig.state)'` with the project added to each entry by the guide's `jq` filter (the list output carries no project; milestone 3 confirms), joined to the asset row on project and zone name; a zone missing from the list output stays `unclassified`. For `dns-routing`, per public zone `gcloud dns record-sets list --project="$project" --zone="$zone" --filter='routingPolicy:*' --format='value(type)'` reduced to a count of routed record sets, written with the project and zone name, never `rrdatas`, names or the policy | `roles/dns.reader` (read-only, "Provides read-only access to all Cloud DNS resources", <https://docs.cloud.google.com/dns/docs/access-control>); it includes `dns.resourceRecordSets.list`, which lets its holder read every record, so the guide offers a custom role with `dns.managedZones.list` alone for a user who does not want `dns-routing` |
| Azure | `Microsoft.Network/dnszones` | `Microsoft.Network/privateDnsZones`, `Microsoft.Network/dnsResolvers` and their children, `Microsoft.Network/trafficManagerProfiles` (from Resource Graph, as today) | Resource Graph, as today, with the zone's `properties.zoneType` projected; for `dnssec`, the zone's DNSSEC configuration read with only its signed-state field kept: `az network dns dnssec-config show --resource-group "$rg" --zone-name "$zone"` with a `--query` keeping that one field, or, if Resource Graph lists the `dnssecConfigs` child, a `project` of the zone's ID and that field alone; never `signingKeys`, `publicKey` or `delegationSignerInfo` (milestone 4 checks which read and which field) | `Reader`, as today (milestone 4 checks it covers the DNSSEC read) |

**The per-zone loops** (milestones 2 to 4) read the zone list the first call wrote with
`while IFS= read -r id; do ...; done`, pass a zone's ID wherever the API accepts one (Route 53's
`--hosted-zone-id`; Cloud DNS and Azure take the zone name, with the project or resource group), quote
every variable (`"$id"`, `"$zone"`, `"$project"`, `"$rg"`), and never use `eval`. The `test/site`
check of the guide's commands asserts that no command holds an unquoted `$zone` or `$id`.

**The record-set permissions.** `route53:ListResourceRecordSets`, and `dns.resourceRecordSets.list` in
`roles/dns.reader`, return every record of a zone to the caller; `--query` and `--format` filter on the
client, after the records were read. So milestones 2 and 3 mark the permission optional in the guide,
needed only for `dns-routing`, and say that it lets its holder read every record; milestone 3 offers the
custom role with `dns.managedZones.list` alone. Both update the guide's paragraph on the collected
directory (`docs/guide/inventory.md`, the paragraph that starts "The collected directory is the
providers' raw output") and its *Privacy* section: zone names are your domains, and the record-set
output is a count per zone only.

Checked on 2026-11-25:

- **AWS Config records `AWS::Route53::HostedZone`** (and `AWS::Route53::DNSSEC` and
  `AWS::Route53::HealthCheck`; no record set type), per the
  [AWS Config resource type list](https://docs.aws.amazon.com/config/latest/developerguide/resource-config-reference.html).
  But the guide's `select-resource-config` selects `resourceId, resourceName, resourceType, awsRegion`,
  no configuration, so it cannot tell a public zone from a private one; and Config lists only what the
  recorder records. So the guide needs the direct `list-hosted-zones` call, and `aws.jq` drops Config's
  `AWS::Route53::HostedZone` and `AWS::Route53::DNSSEC` rows (both join `covered_types`), as it drops
  the types its service calls list: `get-dnssec` already reads a zone's signed state into `dnssec`, so a
  signed zone is listed once, never again as an `unclassified` DNSSEC row. `ListHostedZones` "Retrieves a list of the public and private hosted zones" with `PrivateZone`
  per zone, at most 100 per response (the CLI pages through them)
  (<https://docs.aws.amazon.com/Route53/latest/APIReference/API_ListHostedZones.html>); `GetDNSSEC`
  returns `Status.ServeSignature` beside the key-signing keys
  (<https://docs.aws.amazon.com/Route53/latest/APIReference/API_GetDNSSEC.html>).
- **Cloud Asset Inventory lists `dns.googleapis.com/ManagedZone`** for search
  (<https://docs.cloud.google.com/asset-inventory/docs/asset-types>), with no visibility in the search
  result the guide reads; `gcloud dns managed-zones list` "displays the list of your managed-zones", the
  global Cloud DNS service by default (<https://docs.cloud.google.com/sdk/gcloud/reference/dns/managed-zones/list>).
  So the guide makes the list call and `gcp.jq` joins its output to the asset search's `ManagedZone`
  rows by project and zone name, as it joins `$sql` and the other details today: each zone stays one
  asset row, classified from its details, and `unclassified` when they are missing.
- **Azure DNS zones sign with DNSSEC** ("How to sign your Azure Public DNS zone with DNSSEC",
  <https://learn.microsoft.com/en-us/azure/dns/dnssec>); whether the feature is generally available and
  where its state is read is milestone 4's check.

Capabilities the programs derive (only if open questions 3 and 4 add them):

| Capability | When |
|---|---|
| `dnssec` | Route 53 `ServeSignature` `SIGNING`; Cloud DNS `dnssecConfig.state` `on` (or `transfer`); an Azure zone with a DNSSEC configuration that is signed |
| `dns-routing` | Route 53: a count of routed record sets above 0 (weighted, latency, failover, geolocation, geoproximity, IP-based or multivalue answer, all of which carry a `SetIdentifier`, or a health check); Cloud DNS: a count above 0 of record sets with a `routingPolicy` (weighted round robin, geolocation, failover); Azure DNS: none (routing is Traffic Manager's, a separate resource; an alias record to it is to be checked in milestone 4) |

A simple record set (one name, one type, one set of values) is what every target serves, and an alias
record to a resource of the same source (Route 53 alias to a load balancer, Azure alias record) becomes,
on any target, a plain record or a CNAME: that is a required change the guide names, not a capability.

### Estimates

| Category | Size fields (inventory version 2) | Priced by | Why |
|---|---|---|---|
| `network.dns` | none | not estimated: not priced (`not-priced`, `estimate.NeverPriced`, ADR 0046) | OVHcloud bills a zone yearly (a standalone zone; the zone of a domain registered with it comes with the domain) with Anycast as a €1.09-a-year option, Scaleway's billing is to be read from saved pages, and the sources bill per zone and per query, which no inventory field records. A per-zone price is a few euros a year, and pricing it would need a per-resource fixed-fee method no category has (ADR 0026's fee is on top of a plan); open question 5 |

`usage` is refused on `network.dns`, as on every category it is not for. A size field on a
`network.dns` resource is refused with exit 1, naming its path, as for `network.loadbalancer`.

### Left out

| Candidate | Why left out |
|---|---|
| CDNs (CloudFront, Cloud CDN, Front Door, Azure CDN) | As PRD #552 decided: too vague to assess. Scaleway Edge Services fronts only Scaleway origins, UpCloud CDN is a closed beta with no API, OVHcloud's CDN is a Network product sold by traffic quota, Cloud CDN is a flag on a backend and not a resource, and CloudFront's features (edge functions, WAF) have no vocabulary. The region model here would let a later PRD record one as global; this PRD does not |
| Private DNS zones, resolvers, DNS firewalls | *What does not, and why* above |
| Domain registrations | Not a zone; a registrar transfer is not a catalog offering |
| `ipv6` on `network.dns` | Every source zone is served over IPv6 and holds AAAA records without the user choosing it, so a derived `ipv6` would be on every zone and mark a target `partial` for something no user decided; OVHcloud lists AAAA records, and Scaleway's is to be read. Not proposed |
| Global offerings of other categories | `catalog.New` refuses them (*The region model*); a regional product stays regional |

**Later candidates** for the region model, each its own PRD if wanted: a global load balancer front
(already mapped onto regional target load balancers, PRD #552), a CDN once a target sells a general one,
and transactional email (SES, Azure Communication Services) if a target sells it as a product.

## Acceptance criteria

All criteria are pinned by tests that run offline under `make test` or `make test-e2e`, unless marked
*review*.

**Region model**

- [ ] A version 2 snapshot whose offering has `"global": true` and `"regions": []`, of category
  `network.dns`, is read. One with `"global": true` and a non-empty `regions`, with `"global": false`,
  or with `"global": true` on an offering of any category not marked global (`compute.vm`, say), is
  refused with an error naming the offering; a regional offering with `"regions": []` is still refused
  as available in no region.
- [ ] A global offering's price or fixed fee that names a region is refused.
- [ ] Every existing snapshot reads as before, and its parsed-catalog golden does not change.
- [ ] `shift2eu assess --target ovhcloud --region ovhcloud=<a declared region>` on an inventory with a
  `network.dns` resource gives that resource the global offering as its best offering, never "not
  offered in `<region>`"; `--region ovhcloud=global` is refused with exit 2 and the usual unknown-region
  message, as for any region the catalog does not list.
- [ ] The target's region list (`Catalog.Regions`, the site's region choice, the error that lists a
  target's regions) contains no `global` and no new entry.
- [ ] Markdown and the site's HTML report write "(global)" after the name of a global best offering and
  of a global tie, and in the required-changes list's "(not offered by X)" when X is global, with the
  note saying "not bound to a region you choose" and pointing to the target's `SOURCES.md` (goldens).
- [ ] The Markdown comparison report writes "(global)" in a target's best-offering cell when that
  target's best offering is global, and the HTML comparison report in each target's section (goldens).
- [ ] The JSON report version 3, assessment and comparison, writes `"bestOfferingGlobal": true` on a
  resource whose best offering is global and `globalTies` with the global ties' names, and omits each
  key otherwise; `assessment-v3.schema.json` and `comparison-v3.schema.json` declare both, optional;
  a comparison golden with a global best offering exists, and every golden document validates against
  its schema. Version 2 never writes either key; the frozen version 1 and 2 schemas are unchanged.
- [ ] `Offering.AvailableIn("")` is false on a global offering and `AvailableIn(r)` true for every
  non-empty `r`; `PlanPrice`, `StoragePrice` and `FixedFee` of a global offering apply in any non-empty
  region;
  `targettest`'s offering equality and dump compare and print the global flag.

**Taxonomy and inventory**

- [ ] An inventory (schema version 1 or 2) with a `network.dns` resource with no `region` is read, and
  `shift2eu assess` exits 0. An unknown value such as `network.cdn` or `network.privatedns` is still
  refused with exit 1 and "unknown taxonomy category".
- [ ] A version 2 `network.dns` resource with any size or usage field is refused with exit 1 and a
  message naming the field by its path.
- [ ] If open questions 3 and 4 add them: an inventory listing `dnssec` or `dns-routing` is read, and an
  offering that does not list it gives fit `partial` naming it as missing.
- [ ] Inventories without `network.dns` keep their reports: in milestones 1 to 4 no existing golden file
  of such an inventory changes (the `examples/` goldens change in milestone 1 only because a sample gains
  a `network.dns` resource); in milestones 5 and 6 such a golden may change only in its catalog line's or
  `snapshot` object's source and retrieved date, as PRD #552's same criterion says.
- [ ] The sample inventories under `examples/` together use every taxonomy category, `network.dns`
  included, and every capability, `dnssec` and `dns-routing` included if added: milestone 1's
  `network.dns` resource (or resources) in `examples/` list both (the existing checks in
  `test/examples`).

**Mapping and estimates**

- [ ] On UpCloud a `network.dns` resource has fit `none`, the NoOffering reason as the reports write it,
  "no offering in the `UpCloud` catalog for `network.dns`", and is a blocker; on Scaleway the same until milestone 6 lands.
- [ ] On OVHcloud a `network.dns` resource with no capability has fit `direct` and the DNS zone as best
  offering; one listing a capability the offering does not list is `partial` naming it (for example
  `dns-routing`, if added and if milestone 5 confirms OVHcloud has no routing policies).
- [ ] A `network.dns` resource with a fit is not estimated, "not priced" in the Markdown and HTML reports
  and `not-priced` in the JSON report, and is not counted as 0 in a total.

**Inventory guide**

- [ ] Run as the guide runs them, against synthetic fixtures (zones under `example.com`, account
  `123456789012`, all-zero GUIDs), each source's jq program writes a public zone as `network.dns` with
  no `region`, with the capabilities the table gives, and a private zone as `unclassified` with its
  `rawType`; AWS Config's `AWS::Route53::HostedZone` and `AWS::Route53::DNSSEC` rows for the same zones
  are dropped, and on GCP each zone is the asset
  search's row joined to its `managed-zones list` details (`unclassified` when they are missing), so
  each zone is listed once.
- [ ] A Route 53 zone whose fixture count of routed record sets is above 0 gets `dns-routing` (if
  added); one whose count is 0 does not. A zone with `ServeSignature` `SIGNING` gets `dnssec` (if added).
- [ ] No fixture, no collected sample and no program output holds record data or a DNSSEC key: a test
  scans them recursively and finds none of the keys `rrdatas`, `signatureRrdatas`,
  `healthCheckedTargets`, `ResourceRecords`, `AliasTarget`, `KeySigningKeys`, `SetIdentifier`,
  `signingKeys`, `publicKey` or `delegationSignerInfo`. (`name` is not banned: every GCP asset row and
  zone has one.) The record-set output is the count per zone, or `value(type)` lines only, and its
  fixtures hold no key and no line but the record type. The test also checks that no record name of
  the synthetic zones (for example `www.example.com.`) appears as a value anywhere in the fixtures, the
  collected samples or the program output. Every call's `--query`, `--format` or Resource Graph projection keeps only the fields
  the programs read.
- [ ] Every new command in the guide only lists, describes or gets; the AWS policy, GCP roles and Azure
  role in the guide cover it (*review*, and the existing `cloud-read-only` checks where they apply).
- [ ] The site's inventory page shows the guide's new commands, and no command holds an unquoted loop
  variable, `$zone`, `$id`, `$project` or `$rg` (`test/site`).
- [ ] The guide marks the record-set permission optional, for `dns-routing` only, says it lets its
  holder read every record, and its collected-directory paragraph and *Privacy* section say that zone
  names are the user's domains and the record-set output is a count only (*review*).

**Catalogs**

- [ ] OVHcloud's catalog serves `network.dns` with one global offering (`targettest` *category* check);
  the *regions* check holds (no undeclared region), and a new `targettest` check holds that every global
  offering serves only global categories.
- [ ] The offering has its own section in `SOURCES.md` naming the pages, the fact behind each
  capability and the statement that it is global, with where its name servers are as the pages say.
- [ ] Scaleway's catalog, after milestone 6, does the same, or its `SOURCES.md` *Left out on purpose*
  records why not, with the pages checked.
- [ ] UpCloud's `SOURCES.md` *Left out on purpose* names `network.dns` with the page above.
- [ ] *Review:* no fact in a snapshot comes from a page that was neither fetched from public
  documentation nor saved by the maintainer; no saved page is committed (`TestNoTrackedSavedPages`).

**Architecture and formats**

- [ ] `make check` passes, including depguard and `test/architecture` for every release target.
- [ ] *Review:* the inventory format stays at version 2, the snapshot format at version 2 with the one
  optional key, and the JSON report at its current versions; milestone 1's ADR records why.
- [ ] *Review:* no direct source-service-to-target-service table exists: the guide maps `rawType` to a
  category, the catalogs map offerings to categories, and only the taxonomy joins them.

## Milestones

Each milestone ships as one PR. All depend on this PRD being merged, and every other milestone on
milestone 1. Milestones 2 to 4 (the guide) and 5 and 6 (the catalogs) are independent of one another.
They touch the files the open milestones of PRD #552 and of PRD #571 (merged as a PRD, PR #580) change (`docs/guide/inventory.md`, the
`.jq` programs, the fixtures and e2e goldens, `catalog.json`, the parsed-catalog goldens, the
`targettest` category lists, `SOURCES.md`): each starts after the milestones of both PRDs that touch the
same source or target have merged, so none conflicts (open question 6). Until a target's catalog
milestone lands, its report shows `network.dns` as fit `none` with "no offering in the <target>
catalog", never worse than `unclassified`.

1. [ ] **Region model and the `network.dns` category.** The `global` offering key in the snapshot reader
   (`internal/adapter/target/snapshot`) and `catalog.New` (global means no regions, every-region prices
   and fees, global categories only); `Offering.AvailableIn` for a global offering; the taxonomy's set of
   global categories; `network.dns` with its definition, its row in the size-field table (no field), and
   in the never-priced categories; the capabilities open questions 3 and 4 decide; `Offering.FixedFee`
   for a global offering; the doc comments of `AvailableIn`, `PlanPrice`, `StoragePrice` and
   `FixedFee`, and the snapshot doc's fixed-fee rule; `targettest`'s offering equality and dump carrying
   the global flag; the "(global)" mark and its note in every place *The region model* lists (Markdown
   single-target and comparison, the site's HTML report, required changes included), and
   `bestOfferingGlobal` and `globalTies` in the version 3 JSON schemas (assessment and comparison) and
   reports, with a comparison golden that shows them; a `targettest` check that global offerings serve
   only global categories; `examples/` gains a `network.dns` resource (or two) listing `dnssec` and
   `dns-routing` if added, its goldens change; the guide's lists of categories and capabilities name the
   new ones. One ADR records the region model (options A to E, why A), the snapshot exception (amending
   ADR 0007 and ADR 0024, as ADRs 0026 and 0029 did), the inventory's global resources (no ADR 0025
   change), what "(global)" means for a data-residency reader ("not bound to a region you choose",
   where the name servers are being in `SOURCES.md`), the category and its boundaries, the
   capabilities, and why no format version changes. No
   catalog gains an offering here: the domain and reader tests use their own snapshots. Depends on PRD
   #571 milestone 1 (both add categories to `internal/domain/taxonomy` and samples to `examples/`).
2. [ ] **AWS classification in the guide.** `route53 list-hosted-zones`, `get-dnssec` and (if
   `dns-routing`) `list-resource-record-sets` reduced to a count per zone, with their `--query` keeping
   only the fields read, in the per-zone loop *The per-zone loops* describes (zone IDs, quoted
   variables, `while IFS= read -r`, no `eval`); the IAM actions in the guide's policy,
   `route53:ListResourceRecordSets` marked optional with what it lets its holder read; `aws.jq`
   classifies public zones, keeps private ones `unclassified` and drops Config's
   `AWS::Route53::HostedZone` and `AWS::Route53::DNSSEC` rows (both join `covered_types`; `get-dnssec`
   reads the signed state); `list-hosted-zones` returns `Id` as `/hostedzone/Z...`, so the
   milestone confirms that `get-dnssec` and `list-resource-record-sets` accept that form, or the loop
   strips the prefix; the *Everything else on AWS* text no longer names Route 53 zones as
   `unclassified`; the collected-directory paragraph and *Privacy*; synthetic fixtures (zones under
   `example.com`, account `123456789012`), the recursive no-record-data test, the e2e golden and the
   AWS entries of the e2e category list. Depends on 1 and PRD #571 milestone 2 (PRD #552 milestone 2
   has merged, PR #569).
3. [ ] **GCP classification in the guide.** `gcloud dns managed-zones list` per project, its
   `--format` and the project added for the join, and (if `dns-routing`) `record-sets list` reduced to a
   count per zone, in the per-zone loop *The per-zone loops* describes; `roles/dns.reader` in the
   guide's roles, with what it covers (every record), and the custom role with `dns.managedZones.list`
   alone for a user without `dns-routing`; `gcp.jq` keeps the asset search's `ManagedZone` row and
   joins the list output on project and zone name, `unclassified` when it is missing; the
   collected-directory paragraph and *Privacy*; synthetic fixtures, the recursive no-record-data test,
   golden and the GCP entries of the e2e category list. Depends on 1, PRD #552 milestone 3 and PRD #571
   milestone 3.
4. [ ] **Azure classification in the guide.** `Microsoft.Network/dnszones` from Resource Graph with
   `zoneType` projected, the DNSSEC read with only its signed-state field kept (a `--query`, or a
   Resource Graph `project` of that field) if milestone 4's check confirms where it is, in the per-zone
   loop *The per-zone loops* describes; private zones and resolvers stay `unclassified`; synthetic
   fixtures (all-zero GUIDs), the recursive no-record-data test, golden and the Azure entries of the e2e
   category list. Depends on 1, PRD #552 milestone 4 and PRD #571 milestone 4.
5. [ ] **OVHcloud catalog.** One global `network.dns` offering, *DNS zone*, with the capabilities its
   pages state (`dnssec` from the DNSSEC guide's case 3, if added; `dns-routing` only if a page states
   geolocation, weighted or failover answers), no plans (not priced); `SOURCES.md` (the pages above,
   where its name servers are, the Anycast option as a fact, not an offering), golden, `targettest`
   categories. Pages: the documentation above and the Anycast page, fetchable (`www.ovhcloud.com`
   answered an automated client on 2026-11-25); saved by the maintainer only if they then refuse one.
   Depends on 1 and PRD #571 milestone 5.
6. [ ] **Scaleway catalog, or its record of absence.** **Gated on the maintainer's saved pages** (below).
   If they confirm that a zone for an external domain is hosted and generally available: one global
   `network.dns` offering, *Domains and DNS*, with `dnssec` and `dns-routing` where the pages state them;
   otherwise `SOURCES.md` *Left out on purpose* records it with the pages checked. Depends on 1, PRD #552
   milestone 6 and PRD #571 milestone 7.
7. [ ] **UpCloud's record, comparison, docs and close.** UpCloud's `SOURCES.md` *Left out on purpose*
   names `network.dns` with <https://upcloud.com/docs/products/networking/dns/>; an e2e fixture inventory
   with a public zone, a signed zone and a routed zone, assessed against all three targets, golden for
   Markdown and JSON; the README and the `CLAUDE.md` status line updated; `docs/architecture.md` if it
   describes offering regions. Moves this PRD to `prds/done/` and closes #581. Depends on 2 to 6, PRD
   #552 milestones 7 (UpCloud catalog) and 8 (comparison, docs and close), and PRD #571 milestones 6
   (UpCloud catalog) and 8 (comparison, docs and close): all of them change UpCloud's `SOURCES.md`, the
   three-target e2e comparison fixtures and goldens, the README and the `CLAUDE.md` status line.

**Pages the maintainer must save by hand** (from a logged-out browser, into a scratch directory outside
the repository, never committed, as `internal/adapter/target/upcloud/SOURCES.md` describes), for
milestone 6; each answered HTTP 403 to an automated client on 2026-11-25 or sits on `www.scaleway.com`:

- <https://www.scaleway.com/en/docs/domains-and-dns/faq/>
- <https://www.scaleway.com/en/docs/domains-and-dns/concepts/>
- the Domains and DNS how-to that adds an **external domain** (a domain registered elsewhere) to
  Scaleway's DNS, from the documentation's menu;
- the Domains and DNS how-to or reference page on **DNSSEC**;
- the page on **dynamic records** (geolocation, weighted, health-checked or view answers), if the menu
  has one;
- the Domains and DNS pricing, from <https://www.scaleway.com/en/pricing/>, only if open question 5
  decides to price zones.

No OVHcloud or UpCloud page needs saving: their documentation answered an automated client.

## Decisions and open questions

**Proposed decisions** (all pending the maintainer's approval of this PRD; those an open question names
also wait on its answer)

- **Region model first, option A** (*The region model*): an explicit `global` key, only for categories
  the taxonomy marks global, available in every region, marked in every report, no format version.
- **One category, `network.dns`, public zones only.** A zone is what the user moves and a target sells;
  its records and their routing are what differs between providers. Named after the kind of product, as
  `network.loadbalancer` is, in the `network` family PRD #552 started.
- **The inventory is unchanged.** A global source resource has no `region`, as today; mapping never
  reads it.
- **Not priced.** `network.dns` joins `network.loadbalancer` and `registry.container` as never priced
  (open question 5).
- **No inventory format version, no JSON report `schemaVersion` bump, no snapshot format version.** As
  ADR 0023, 0046: `taxonomy.ParseCategory` accepts a new value in a version 1 or 2 file, and the field
  table gains a row for the new category only. ADR 0018 keeps the version for an optional key; ADR 0026
  and 0029 set the precedent for an optional snapshot key within version 2. An older binary refuses an
  inventory with `network.dns` with "unknown taxonomy category".
- **GA only, and the target rule.** As in PRDs #11, #77 and #552: a capability is listed only where the
  documentation states it (ADR 0011), and a product whose availability is not stated is not an offering.
- **The guide can ship before the catalogs**, as in PRD #552.
- **The guide reads no zone contents.** Record names and values are the user's data (they name internal
  hosts, verification tokens); the record-set calls' `--query` and `--format` reduce them to a count
  of routed record sets per zone, and the DNSSEC reads to the signed state, as PRD
  #571's *Only the fields the program reads* keeps the guide to what it uses.

**Open questions for the maintainer** (each with a recommendation)

1. **The region model.** Options A to E under *The region model*. *Recommendation:* A, an explicit
   `"global": true` with empty `regions`, limited to categories the taxonomy marks global; milestone 1's
   ADR records it. B hides a forgotten region list; C lists `global` among the EU regions and drops zones
   under any real `--region`; D claims a residency the sources do not state; E doubles the offering shape.
2. **How the JSON report marks a global offering.** A global category may have regional offerings too,
   so a resource's ties can mix global and regional ones. (a) an optional `bestOfferingGlobal: true` and
   an optional `globalTies` (the names of the global ties) on the resource, version 3 only, assessment
   and comparison; (a') `bestOfferingGlobal` alone, with Markdown and HTML then also marking only the
   best offering, never a tie; (b) nothing: a consumer reads the category; (c) a new `schemaVersion` 4
   with an offering object instead of a name. *Recommendation:* (a). It keeps `schemaVersion` 3 (ADR
   0018: optional keys), says exactly what Markdown and HTML say, ties included, and leaves `ties` as it
   is; (a') hides a global tie from every reader; (c) is a breaking change for one flag.
3. **`dnssec` as a capability.** A signed source zone moved to a provider that cannot sign it must be
   unsigned first (the DS record removed at the registrar), or validating resolvers fail it. Route 53
   and Cloud DNS sign, and milestone 4 checks Azure DNS signing; OVHcloud signs an external zone through its API; Scaleway is to be read. *Recommendation:* add
   `dnssec`, "the provider signs the zone with DNSSEC and publishes the keys the parent's DS record
   points at", derived read-only from each source's DNSSEC state. It costs one call per zone and names
   a real dependency; if both targets sign, it still keeps a signed zone from mapping `direct` to a later
   target that does not. The alternative is no capability and a note in the guide.
4. **`dns-routing` as a capability.** Route 53 weighted, latency, failover, geolocation and
   multivalue records, and Cloud DNS routing policies, answer differently by client or by health; a target
   with plain records would serve them `direct` and hide that failover stops working. OVHcloud documents
   no such answers (milestone 5 checks), Scaleway's dynamic records are to be read. *Recommendation:*
   add `dns-routing`, "the zone answers depend on the client's location, on weights or on the health of
   the endpoints", derived from a count of routed record sets per zone, with no record data written.
   The cost is one more call per zone and a record-set permission (`route53:ListResourceRecordSets`,
   and `dns.resourceRecordSets.list` in `roles/dns.reader`), which lets its holder read every record:
   the guide marks it optional, for `dns-routing` only; the alternative is no capability and a guide
   note telling the user to add it by hand.
5. **Pricing zones.** *Recommendation:* not priced (`not-priced`) in this PRD: a zone costs a few euros
   a year on OVHcloud, is included with a domain registered there, and Scaleway's billing is unknown;
   pricing it needs a per-resource fee method no category has. A follow-up issue can add one if wanted.
6. **Order against PRDs #552 and #571.** All three change the same guide sections, jq programs,
   fixtures, catalogs and goldens (PRD #571 is merged as a PRD, PR #580, with all eight milestones
   open; PRD #552 has milestones 1, 2 and 5 merged, milestone 2 in PR #569). *Recommendation:* this
   PRD queues behind both, file area by file area: milestone 1 after PRD #571 milestone 1; each guide
   milestone after both PRDs' open milestones of the same source (AWS: PRD #571 milestone 2; GCP: PRD
   #552 milestone 3 and PRD #571 milestone 3; Azure: PRD #552 milestone 4 and PRD #571 milestone 4);
   OVHcloud (5) after PRD #571 milestone 5; Scaleway (6) after PRD #552 milestone 6 and PRD #571
   milestone 7; UpCloud and close (7) after PRD #552 milestones 7 and 8 and PRD #571 milestones 6 and
   8. Scaleway's saved pages for this PRD can be saved in the same session as theirs.
7. **Private zones.** *Recommendation:* keep them `unclassified` (*What does not, and why*). The
   alternative, a `network.privatedns` category with no offering on any target, says only what
   `unclassified` already says, and adds a category to every adapter.

**To be checked** (each in the milestone named; a fact not confirmed stays out of the snapshot)

- Milestone 1: that no place reads an offering's regions but `catalog.New`, the snapshot reader,
  `Offering.AvailableIn`, `Offering.priceIn` (`PlanPrice`, `StoragePrice`), `Offering.FixedFee`
  (`catalog.go`, used by the estimate), the estimate's `offeredIn`
  and `plansOfferedIn` (through `AvailableIn`) and `targettest`'s `equalOffering` and `describe` (the
  site builder writes `Catalog.Regions`, unchanged); that no report place names an offering beyond the
  ones *The region model* lists.
- Milestone 2: the CLI's `get-dnssec` output on a zone that was never signed (`NOT_SIGNING`) and on a private
  zone (an error the program must tolerate, or the call made only for public zones); that the count's
  predicate (`SetIdentifier || HealthCheckId || MultiValueAnswer`) catches every non-simple record set,
  and whether an alias to a resource of another zone with `EvaluateTargetHealth` counts. (Whether AWS
  Config records a hosted zone with `awsRegion` `global` does not matter: `aws.jq` drops its rows, and
  the `AWS::Route53::DNSSEC` rows, by `resourceType`, `covered_types`.)
- Milestone 3: that `gcloud dns managed-zones list` returns `visibility` and `dnssecConfig.state` in
  every project, whether its output carries the project (or the guide's `jq` filter adds it), and
  whether forwarding and peering zones are `private`; that `--filter='routingPolicy:*'` with
  `--format='value(type)'` counts exactly the routed record sets; whether a narrower predefined role than
  `roles/dns.reader` lists zones.
- Milestone 4: whether Azure DNS DNSSEC signing is generally available, where its state is read
  (Resource Graph `dnszones/dnssecConfigs`, or `az network dns dnssec-config show`), and that `Reader`
  covers it; whether an alias record set pointing at Traffic Manager should give `dns-routing`.
- Milestone 5: OVHcloud's name server locations for a zone without Anycast; whether any OVHcloud page
  offers geolocation, weighted or failover answers; that the DNSSEC API covers a standalone zone; that
  a standalone zone is a generally available product (an order tile, not a beta).
- Milestone 6: from the saved pages, Scaleway's external domain hosting, its DNSSEC for an external
  domain, its dynamic records and their kinds, and whether any of it is in beta.
