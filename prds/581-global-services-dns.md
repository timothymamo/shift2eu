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
- **Catalog.** `Offering.Global` (or an equivalent the milestone chooses); `Offering.AvailableIn(region)`
  is true for every region of the target when the offering is global; `Catalog.Regions` stays the
  declared regions the regional offerings and the default region use, so a global offering adds no
  region, and `--region` keeps accepting exactly today's regions. `defaultRegion` stays a real region.
- **Mapping and `--region`.** A global offering is a candidate in every region, so it is never dropped by
  `MapIn` and never gives "not offered in <region>". The alternative, listing global offerings
  separately from the region's, would show the same zone as served and not served at once.
- **Estimates.** A global offering's prices apply in the estimate region, whatever it is. (`network.dns`
  is not priced in this PRD, so no amount depends on it yet; the rule is for a later priced global
  category.)
- **Reports.** Wherever a report names an offering (the best offering and its ties), a global one is
  marked: Markdown and the site's HTML report write "(global)" after its name; the JSON report version 3
  gives the resource an optional `bestOfferingGlobal: true` when its best offering is global (absent
  otherwise), which keeps `schemaVersion` 3 under ADR 0018's rule for an optional key, with the version 3
  schema files updated in the same change; the frozen version 1 and 2 files are untouched and those
  versions do not write it (open question 2). The report's region line is unchanged: it states the
  region chosen or estimated for, and a global offering does not change it.
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
| AWS | `AWS::Route53::HostedZone` with `Config.PrivateZone` false | the same type with `PrivateZone` true; `AWS::Route53::HealthCheck`, `AWS::Route53Resolver::*`, `AWS::Route53Profiles::*` (from AWS Config, as today) | `route53 list-hosted-zones` (`--query` keeping `Id`, `Name` and `Config.PrivateZone`); per public zone `route53 get-dnssec` (`--query Status.ServeSignature` only, never the key-signing keys); for `dns-routing`, per public zone `route53 list-resource-record-sets` with a `--query` that keeps only the routing keys (`SetIdentifier`, `Failover`, `GeoLocation`, `GeoProximityLocation`, `Region`, `Weight`, `MultiValueAnswer`, `CidrRoutingConfig`, `HealthCheckId`), never a record's name or values | `route53:ListHostedZones`, `route53:GetDNSSEC`, `route53:ListResourceRecordSets` |
| GCP | `dns.googleapis.com/ManagedZone` with `visibility` `public` | the same type with visibility `private`; `dns.googleapis.com/Policy`, `dns.googleapis.com/ResponsePolicy` (from the asset search, as today) | `gcloud dns managed-zones list` (`--format` keeping `name`, `dnsName`, `visibility` and `dnssecConfig.state`); for `dns-routing`, per public zone `gcloud dns record-sets list` with a `--format` that keeps only whether `routingPolicy` is set, never `rrdatas` or names | `roles/dns.reader` (read-only, "Provides read-only access to all Cloud DNS resources", <https://docs.cloud.google.com/dns/docs/access-control>); it includes `dns.resourceRecordSets.list`, so the guide says that without `dns-routing` `dns.managedZones.list` alone is needed (a custom role) |
| Azure | `Microsoft.Network/dnszones` | `Microsoft.Network/privateDnsZones`, `Microsoft.Network/dnsResolvers` and their children, `Microsoft.Network/trafficManagerProfiles` (from Resource Graph, as today) | Resource Graph, as today, with the zone's `properties.zoneType` projected; for `dnssec`, the zone's DNSSEC configuration (`az network dns dnssec-config show`, or the `dnssecConfigs` child if Resource Graph lists it; milestone 4 checks) | `Reader`, as today (milestone 4 checks it covers the DNSSEC read) |

Checked on 2026-11-25:

- **AWS Config records `AWS::Route53::HostedZone`** (and `AWS::Route53::DNSSEC` and
  `AWS::Route53::HealthCheck`; no record set type), per the
  [AWS Config resource type list](https://docs.aws.amazon.com/config/latest/developerguide/resource-config-reference.html).
  But the guide's `select-resource-config` selects `resourceId, resourceName, resourceType, awsRegion`,
  no configuration, so it cannot tell a public zone from a private one; and Config lists only what the
  recorder records. So the guide needs the direct `list-hosted-zones` call, and `aws.jq` drops Config's
  `AWS::Route53::HostedZone` rows (they join `covered_types`), as it drops the types its service calls
  list. `ListHostedZones` "Retrieves a list of the public and private hosted zones" with `PrivateZone`
  per zone, at most 100 per response (the CLI pages through them)
  (<https://docs.aws.amazon.com/Route53/latest/APIReference/API_ListHostedZones.html>); `GetDNSSEC`
  returns `Status.ServeSignature` beside the key-signing keys
  (<https://docs.aws.amazon.com/Route53/latest/APIReference/API_GetDNSSEC.html>).
- **Cloud Asset Inventory lists `dns.googleapis.com/ManagedZone`** for search
  (<https://docs.cloud.google.com/asset-inventory/docs/asset-types>), with no visibility in the search
  result the guide reads; `gcloud dns managed-zones list` "displays the list of your managed-zones", the
  global Cloud DNS service by default (<https://docs.cloud.google.com/sdk/gcloud/reference/dns/managed-zones/list>).
  So the guide makes the list call and `gcp.jq` drops the asset search's `ManagedZone` rows.
- **Azure DNS zones sign with DNSSEC** ("How to sign your Azure Public DNS zone with DNSSEC",
  <https://learn.microsoft.com/en-us/azure/dns/dnssec>); whether the feature is generally available and
  where its state is read is milestone 4's check.

Capabilities the programs derive (only if open questions 3 and 4 add them):

| Capability | When |
|---|---|
| `dnssec` | Route 53 `ServeSignature` `SIGNING`; Cloud DNS `dnssecConfig.state` `on` (or `transfer`); an Azure zone with a DNSSEC configuration that is signed |
| `dns-routing` | Route 53: a record set with any routing key above (weighted, latency, failover, geolocation, geoproximity, IP-based or multivalue answer) or a health check; Cloud DNS: a record set with a `routingPolicy` (weighted round robin, geolocation, failover); Azure DNS: none (routing is Traffic Manager's, a separate resource; an alias record to it is to be checked in milestone 4) |

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
  of a global tie; the JSON report version 3 writes `"bestOfferingGlobal": true` on such a resource and
  omits the key otherwise; versions 1 and 2 never write it; every golden document validates against its
  schema.

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
  included (the existing check in `test/examples`).

**Mapping and estimates**

- [ ] On UpCloud a `network.dns` resource has fit `none`, the reason "no offering in the UpCloud catalog
  for `network.dns`", and is a blocker; on Scaleway the same until milestone 6 lands.
- [ ] On OVHcloud a `network.dns` resource with no capability has fit `direct` and the DNS zone as best
  offering; one listing a capability the offering does not list is `partial` naming it (for example
  `dns-routing`, if added and if milestone 5 confirms OVHcloud has no routing policies).
- [ ] A `network.dns` resource with a fit is not estimated, "not priced" in the Markdown and HTML reports
  and `not-priced` in the JSON report, and is not counted as 0 in a total.

**Inventory guide**

- [ ] Run as the guide runs them, against recorded, sanitised fixtures (zones under `example.com`), each
  source's jq program writes a public zone as `network.dns` with no `region`, with the capabilities the
  table gives, and a private zone as `unclassified` with its `rawType`; AWS Config's and the GCP asset
  search's rows for the same zones are dropped, so each zone is listed once.
- [ ] A Route 53 zone whose fixture record sets include a weighted or failover record gets `dns-routing`
  (if added); one with only simple records does not. A zone with `ServeSignature` `SIGNING` gets `dnssec`
  (if added).
- [ ] No fixture and no program output holds a record's name or values, or a DNSSEC key (a test checks
  the fixtures for the keys the calls' `--query` and `--format` leave out).
- [ ] Every new command in the guide only lists, describes or gets; the AWS policy, GCP roles and Azure
  role in the guide cover it (*review*, and the existing `cloud-read-only` checks where they apply).
- [ ] The site's inventory page shows the guide's new commands (`test/site`).

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
They touch the files PRD #552's and PRD #571's open milestones change (`docs/guide/inventory.md`, the
`.jq` programs, the fixtures and e2e goldens, `catalog.json`, the parsed-catalog goldens, the
`targettest` category lists, `SOURCES.md`): each starts after the milestones of both PRDs that touch the
same source or target have merged, so none conflicts (open question 6). Until a target's catalog
milestone lands, its report shows `network.dns` as fit `none` with "no offering in the <target>
catalog", never worse than `unclassified`.

1. [ ] **Region model and the `network.dns` category.** The `global` offering key in the snapshot reader
   (`internal/adapter/target/snapshot`) and `catalog.New` (global means no regions, every-region prices
   and fees, global categories only); `Offering.AvailableIn` for a global offering; the taxonomy's set of
   global categories; `network.dns` with its definition, its row in the size-field table (no field), and
   in the never-priced categories; the capabilities open questions 3 and 4 decide; the "(global)" mark in
   the Markdown and HTML reports and `bestOfferingGlobal` in the version 3 JSON schemas and reports;
   a `targettest` check that global offerings serve only global categories; `examples/` gains one
   `network.dns` resource, its goldens change; the guide's lists of categories and capabilities name the
   new ones. One ADR records the region model (options A to E, why A), the snapshot exception (amending
   ADR 0007 and ADR 0024, as ADRs 0026 and 0029 did), the inventory's global resources (no ADR 0025
   change), the category and its boundaries, the capabilities, and why no format version changes. No
   catalog gains an offering here: the domain and reader tests use their own snapshots. Depends on PRD
   #571 milestone 1 (both add categories to `internal/domain/taxonomy` and samples to `examples/`).
2. [ ] **AWS classification in the guide.** `route53 list-hosted-zones`, `get-dnssec` and (if
   `dns-routing`) `list-resource-record-sets`, with their `--query` keeping only the fields read; the
   three IAM actions in the guide's policy; `aws.jq` classifies public zones, keeps private ones
   `unclassified` and drops Config's `AWS::Route53::HostedZone` rows; the *Everything else on AWS* text
   no longer names Route 53 zones as `unclassified`; synthetic sanitised fixtures, the no-excluded-key
   test, the e2e golden and the AWS entries of the e2e category list. Depends on 1, PRD #552 milestone 2
   and PRD #571 milestone 2.
3. [ ] **GCP classification in the guide.** `gcloud dns managed-zones list` and (if `dns-routing`)
   `record-sets list`, with `--format` keeping only the fields read; `roles/dns.reader` in the guide's
   roles, with what it covers; `gcp.jq` classifies public zones and drops the asset search's duplicates;
   fixtures, golden and the GCP entries of the e2e category list. Depends on 1, PRD #552 milestone 3 and
   PRD #571 milestone 3.
4. [ ] **Azure classification in the guide.** `Microsoft.Network/dnszones` from Resource Graph with
   `zoneType` projected, the DNSSEC read if milestone 4's check confirms where it is; private zones and
   resolvers stay `unclassified`; fixtures (all-zero GUIDs), golden and the Azure entries of the e2e
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
   describes offering regions. Moves this PRD to `prds/done/` and closes #581. Depends on 2 to 6.

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
  hosts, verification tokens); the calls' `--query` and `--format` keep only the routing keys, as PRD
  #571's *Only the fields the program reads* keeps the guide to what it uses.

**Open questions for the maintainer** (each with a recommendation)

1. **The region model.** Options A to E under *The region model*. *Recommendation:* A, an explicit
   `"global": true` with empty `regions`, limited to categories the taxonomy marks global; milestone 1's
   ADR records it. B hides a forgotten region list; C lists `global` among the EU regions and drops zones
   under any real `--region`; D claims a residency the sources do not state; E doubles the offering shape.
2. **How the JSON report marks a global offering.** (a) an optional `bestOfferingGlobal: true` on the
   resource, version 3 only; (b) nothing: a consumer reads the category; (c) a new `schemaVersion` 4
   with an offering object instead of a name. *Recommendation:* (a). It keeps `schemaVersion` 3 (ADR
   0018: an optional key), says what Markdown says, and leaves ties' names as they are; (c) is a breaking
   change for one flag.
3. **`dnssec` as a capability.** A signed source zone moved to a provider that cannot sign it must be
   unsigned first (the DS record removed at the registrar), or validating resolvers fail it. Every source
   signs; OVHcloud signs an external zone through its API; Scaleway is to be read. *Recommendation:* add
   `dnssec`, "the provider signs the zone with DNSSEC and publishes the keys the parent's DS record
   points at", derived read-only from each source's DNSSEC state. It costs one call per zone and names
   a real dependency; if both targets sign, it still keeps a signed zone from mapping `direct` to a later
   target that does not. The alternative is no capability and a note in the guide.
4. **`dns-routing` as a capability.** Route 53 weighted, latency, failover, geolocation and
   multivalue records, and Cloud DNS routing policies, answer differently by client or by health; a target
   with plain records would serve them `direct` and hide that failover stops working. OVHcloud documents
   no such answers (milestone 5 checks), Scaleway's dynamic records are to be read. *Recommendation:*
   add `dns-routing`, "the zone answers depend on the client's location, on weights or on the health of
   the endpoints", derived from record sets read with a query that keeps only the routing keys. The
   cost is one more call per zone and a record-set permission (`route53:ListResourceRecordSets`, and
   `roles/dns.reader`, which also reads record values); the alternative is no capability and a guide
   note telling the user to add it by hand.
5. **Pricing zones.** *Recommendation:* not priced (`not-priced`) in this PRD: a zone costs a few euros
   a year on OVHcloud, is included with a domain registered there, and Scaleway's billing is unknown;
   pricing it needs a per-resource fee method no category has. A follow-up issue can add one if wanted.
6. **Order against PRDs #552 and #571.** All three change the same guide sections, jq programs,
   fixtures, catalogs and goldens. *Recommendation:* this PRD queues behind both, file area by file
   area: milestone 1 after PRD #571 milestone 1; each guide milestone after both PRDs' milestones of
   the same source (PRD #552 milestones 2 to 4, PRD #571 milestones 2 to 4); OVHcloud (5) after PRD #571
   milestone 5 (PRD #552 milestone 5 has merged); Scaleway (6) after PRD #552 milestone 6 and PRD #571
   milestone 7. Scaleway's saved pages for this PRD can be saved in the same session as theirs.
7. **Private zones.** *Recommendation:* keep them `unclassified` (*What does not, and why*). The
   alternative, a `network.privatedns` category with no offering on any target, says only what
   `unclassified` already says, and adds a category to every adapter.

**To be checked** (each in the milestone named; a fact not confirmed stays out of the snapshot)

- Milestone 1: that no place but `Offering.AvailableIn`, `catalog.New`, the snapshot reader and the
  three reports reads an offering's regions (the site builder writes `Catalog.Regions`, unchanged).
- Milestone 2: whether AWS Config records a hosted zone with `awsRegion` `global` (so the dropped rows
  match); the CLI's `get-dnssec` output on a zone that was never signed (`NOT_SIGNING`) and on a private
  zone (an error the program must tolerate, or the call made only for public zones); that the routing
  keys named are every key that marks a non-simple record set, and whether an alias to a resource of
  another zone with `EvaluateTargetHealth` counts.
- Milestone 3: that `gcloud dns managed-zones list` returns `visibility` and `dnssecConfig.state` in
  every project, and whether forwarding and peering zones are `private`; the `--format` that keeps
  `routingPolicy` presence only; whether a narrower predefined role than `roles/dns.reader` lists zones.
- Milestone 4: whether Azure DNS DNSSEC signing is generally available, where its state is read
  (Resource Graph `dnszones/dnssecConfigs`, or `az network dns dnssec-config show`), and that `Reader`
  covers it; whether an alias record set pointing at Traffic Manager should give `dns-routing`.
- Milestone 5: OVHcloud's name server locations for a zone without Anycast; whether any OVHcloud page
  offers geolocation, weighted or failover answers; that the DNSSEC API covers a standalone zone; that
  a standalone zone is a generally available product (an order tile, not a beta).
- Milestone 6: from the saved pages, Scaleway's external domain hosting, its DNSSEC for an external
  domain, its dynamic records and their kinds, and whether any of it is in beta.
