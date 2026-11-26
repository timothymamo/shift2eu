# Scaleway catalog snapshot: sources

`catalog.json` is a hand-compiled, dated snapshot of what Scaleway offers, in taxonomy terms. This file
records where each fact comes from, so the snapshot can be checked and refreshed.

## How it was compiled

- **Retrieved on:** 2026-11-13, the date the plans and prices were read (PRD #190 milestone 4, ADR 0024).
  The snapshot's `retrievedOn` and `sourceURL` now name the prices, the facts most likely to change. The
  regions and capabilities were compiled and verified on 2026-11-10 and re-verified on 2026-11-11 for
  PRD #14 (below); the prices changed no offering, capability or region. *Kubernetes Kapsule
  Dedicated*'s regions, and its plans' price regions, were last re-checked on 2026-11-19 for issue #211
  (below), which added `it-mil`. `retrievedOn` stays 2026-11-13, for the reason that bullet gives.
- **Compiled, then verified against the live pages.** The snapshot was first compiled from Scaleway's
  documentation source and Go SDK at the commits below, because `www.scaleway.com` then answered HTTP 403.
  On the same date every region and capability was checked against the live public pages cited below
  (issue #15), without credentials and without calling any Scaleway API. The check changed one fact:
  Serverless Functions gained the capability `ipv6`, and its section says why. It also found one
  conflict: the availability page lists Managed Database for PostgreSQL and MySQL in Milan, but the
  product's API does not declare Milan. The region stays out, as that offering's section explains.
- **Re-verified on 2026-11-11 for PRD #14 (capability granularity).** Every high-availability claim was
  checked again, and replaced by its scoped capability (`node-high-availability` or
  `zone-high-availability`) or dropped, under the target rule and the tier-variant rule (*Rules
  applied*). `www.scaleway.com` answered HTTP 403 to every fetch that day, for the documentation pages,
  their `.md` form and the product pages. The documentation source was read instead:
  `scaleway/docs-content` `main` was still at commit `e149770a6eacd436b33ef8c36ad40cdff90ebdca`, the
  commit cited below, which issue #15 found identical to the live pages. The URLs cited are the pages
  that source publishes. The availability page answered HTTP 403 to this re-verification too, at both
  <https://www.scaleway.com/en/availability/> and the address below. The reviewer of this change read
  it the same day at <https://www.scaleway.com/en/availability/> (HTTP 200, with the regions in the
  page's embedded JSON) and found that it agrees with every region the snapshot declares. It has one
  Kapsule row and no separate row for Dedicated control planes. So no declared region changed, and the
  new variant *Kubernetes Kapsule Dedicated* lists the regions its own section gives. This
  re-verification removed `ipv6` from Serverless Functions and removed Serverless SQL Database
  (*Left out on purpose*). Because it changed facts, `retrievedOn` in `catalog.json` was its date,
  2026-11-11, until the prices were added.
- **Prices read on 2026-11-13 for PRD #190 milestone 4.** The plans and prices, with the pages under
  *Prices*. No offering, capability or region changed.
- **Elastic Metal storage re-examined on 2026-11-14 for issue #219.** The *Elastic Metal* price page and
  the Elastic Metal FAQ were read again, from the maintainer's saved browser copies (`www.scaleway.com`
  answered automated clients with HTTP 403 that day), to find a price for storage beyond a server's
  disks. There is none, and *Elastic Metal* says why. The same read re-checked the eight recorded Elastic
  Metal plans: each server's hourly price, disks, cores, threads, memory and zones still match the page,
  so no plan price or included storage changed. No offering, capability, region or price changed, so
  `retrievedOn` stays 2026-11-13. The copies are not in the repository and were read as data; a refresh
  saves them the same way, from a logged-out browser session into a scratch directory outside the
  repository, and never commits them.
- **Kapsule Dedicated in Milan re-checked on 2026-11-19 for issue #211.** The *Containers* price page,
  the availability page with its CSV export, SDK `api/k8s/v1` and the Kubernetes documentation source
  were read again, from the maintainer's saved browser copies and public GitHub (`www.scaleway.com`
  refuses automated clients). The price page states, per region, which Dedicated control planes can be
  ordered, and it offers Dedicated-4 and Dedicated-8 in Milan, so *Kubernetes Kapsule Dedicated* gained
  the region `it-mil`; its section gives the sources. This re-check used the price page as the
  tier-variant regions rule's source for one offering. Its worker-node plans took the price regions of
  *Kubernetes Kapsule* (PLAY2 and PRO2 not priced in Milan), and its fixed fee and storage price hold in
  Milan unchanged. No other offering, capability, region or price changed. `retrievedOn` stays
  2026-11-13: it names the date every plan and price was read (above), and this re-check read the prices
  of one offering only.
- **Declared EU regions and the API check re-read on 2026-11-21 for issue #328.** Every region source
  on public GitHub was read again at the current heads of both repositories (`www.scaleway.com` refuses
  automated clients): `scaleway/scaleway-sdk-go` default branch `main` at commit
  `a251fd549cab893cd1fcdc7e9a2d382209f42531` (committed 2026-11-19), and `scaleway/docs-content`
  default branch `main` at commit `1ec45ae299fdea70effdd48e5a3c99f5d6dad8b1` (committed 2026-11-20).
  That covers the declared regions and zones (*Declared EU regions* gives the files read) and the API
  half of the regions rule for every offering (*API reference and SDK*, below, gives the files read).
  No region or zone changed, and every product API still serves exactly the regions its offering
  lists. **The availability page was not read again:** it is not part of `docs-content`, and
  `www.scaleway.com` is off limits to automated clients. It was last read in full on 2026-11-10 and
  2026-11-11 (above), and its Kapsule row alone on 2026-11-19 for issue #211, from the maintainer's
  saved copy. A full re-read waits for the maintainer's saved copy of the page. The API reference pages
  were not read again either. No offering, capability or price was read, so `retrievedOn` stays
  2026-11-13.
- **Eight offerings added on 2026-11-26 for PRD #552 milestone 6.** *Container Registry*, *Load
  Balancer*, *Managed Database for Redis*, *Managed Database for Redis Cluster*, *Managed MongoDB*,
  *Managed MongoDB Replica Set*, *Serverless Containers* and *Topics and Events*, for the categories
  `registry.container`, `network.loadbalancer`, `cache.redis`, `database.mongodb`,
  `serverless.container` and `messaging.topic`; each has its own section below. No `storage.file`
  offering (*Left out on purpose*, File Storage). The sources read that day:
  - three pages the maintainer saved from a logged-out browser (`www.scaleway.com` refuses automated
    clients), read as data from a scratch directory outside the repository and never committed: the
    saved copy of <https://www.scaleway.com/en/pricing/managed-databases/>, the saved copy of
    <https://www.scaleway.com/en/pricing/storage/> and the saved copy of
    <https://www.scaleway.com/en/product-availability-by-region/>, all read 2026-11-26. Each was read
    twice, as its visible tables (Paris selected) and as its embedded data (`__NEXT_DATA__`); no price
    or region disagreed between the two. The two price pages embed the same full price catalogue
    (every product key identical), so a price below cites the page whose visible table shows it. The
    availability page's embedded record is dated `updatedAt` 2026-10-30T10:47:34Z;
  - `scaleway/docs-content`, branch `main` at commit `13ef235ee5926d0a675e9b87cc421df5105762b1`
    (committed 2026-11-26), read through the GitHub API without credentials; a file
    `pages/<product>/<page>.mdx` is published at `https://www.scaleway.com/en/docs/<product>/<page>/`,
    the URL each section cites;
  - `scaleway/scaleway-sdk-go`, branch `main` at commit `d5b724367ac9d6a0e9b39f91a9aabf0b55216360`
    (2026-11-26), for the regions and zones each new product's API declares (each section names its
    file and the commit that last changed it), and `scaleway/scaleway-sdk-js` at commit
    `766d0b1d2d387731b12fd80c64ff7a2d110554a1`, as a cross-check.

  **What was re-read, and the date.** The two price pages hold the prices of *Managed Database for
  PostgreSQL and MySQL* (both offerings), their storage, *Block Storage* and *Object Storage*. Every
  one of those existing plans and prices was compared with the re-read pages and **none changed**
  (plans, vCPU, memory, hourly prices and regions), so no existing amount in `catalog.json` changed.
  The availability page was re-read **in full** for the first time since 2026-11-11, and every existing
  offering's regions still hold (Milan is still not declared by SDK `api/rdb/v1`, so the database
  offerings still leave it out); this closes the "a full re-read waits" note of 2026-11-21 below. The
  prices of *Instances*, *Elastic Metal* and the Kapsule control planes were **not** re-read from
  their own pages (they were not saved this time) and keep their 2026-11-13 reading.
  **`retrievedOn` stays 2026-11-13** (ADR 0007, one date per snapshot): it names the date every plan
  and price was read, and this re-read covered the prices of some pages only, as the Kapsule
  Dedicated re-check of 2026-11-19 did. The new offerings' prices were read on 2026-11-26, which each
  of their sections says.
- **Availability page:** <https://www.scaleway.com/en/product-availability-by-region/>, where every
  offering's regions come from, subject to the API check under *Rules applied*. It has one column per
  region (Paris, Amsterdam, Warsaw, Milan). For a zonal product it gives one mark per zone, and for a
  regional product it says "Available across the region".
  The legend has three marks: "Available", "Available soon" and "Not available". The page warns that
  "upcoming availability is based on our current roadmap and is subject to change". A plain HTTP client
  gets a JavaScript challenge instead of this page, so no copy of it was saved. It was read twice through
  a web-fetch tool, and both reads gave the same marks for every row cited below. A third read the same
  day was refused with HTTP 403. A reviewer should check the cells in a browser.
- **Documentation:** each page cited below was fetched live from `https://www.scaleway.com/en/docs/`, in
  the Markdown form the site publishes beside each page (`<page>.md`, linked from
  <https://www.scaleway.com/en/docs/llms.txt>). The documentation is built from
  [`scaleway/docs-content`](https://github.com/scaleway/docs-content). On the retrieval date its branch
  `main` was at commit `e149770a6eacd436b33ef8c36ad40cdff90ebdca`, the commit the snapshot was first
  compiled from, so the live pages and that source agree. A file `pages/<product>/<page>.mdx` is
  published at `https://www.scaleway.com/en/docs/<product>/<page>/`.
- **Changelog:** <https://www.scaleway.com/en/docs/changelog/>, read through its RSS feed
  <https://www.scaleway.com/en/docs/changelog/rss.xml>. The page draws its entries in the browser. The
  changelog gives the dates of launches and betas.
- **API reference and SDK, for checking regions:** the API reference pages under
  <https://www.scaleway.com/en/developers/api/> with the OpenAPI schemas they publish, and Scaleway's
  official Go SDK, [`scaleway/scaleway-sdk-go`](https://github.com/scaleway/scaleway-sdk-go), at commit
  `f408edc91a855d0b585a8fd0458f7f4761c4eefe`. That commit was still the head of `main` on the retrieval
  date. Each product API declares the regions or zones it serves (`Regions()` or `Zones()` in the SDK,
  the `region` or `zone` enum in the schema). They agree with the availability page except for Managed
  Database for PostgreSQL and MySQL in Milan. That offering's section also cites Scaleway's official
  JavaScript SDK, [`scaleway/scaleway-sdk-js`](https://github.com/scaleway/scaleway-sdk-js), at the
  commit given there.
  **Re-read on 2026-11-21 (issue #328)** at `scaleway-sdk-go` commit
  `a251fd549cab893cd1fcdc7e9a2d382209f42531`, one file per offering's product, with the commit that
  last changed it. Every one still serves exactly what the offering's section records, so no
  offering's regions changed:
  - `api/block/v1/block_sdk.go` (last changed in `b9189f296d`, 2026-10-13), *Block Storage*: `Zones()`
    is all twelve zones of the four regions.
  - `api/baremetal/v1/baremetal_sdk.go` (`7dcaff287d`, 2026-11-12), *Elastic Metal*: `API.Zones()` is
    fr-par-1, fr-par-2, nl-ams-1, nl-ams-2, pl-waw-2 and pl-waw-3, so no Milan zone.
  - `api/instance/v1/instance_sdk.go` (`58b0d2c460`, 2026-11-18), *Instances*: `Zones()` is the three
    zones of Paris, Amsterdam and Warsaw, and it-mil-1 only.
  - `api/k8s/v1/k8s_sdk.go` (`e521727b25`, 2026-10-08), *Kubernetes Kapsule* and *Kubernetes Kapsule
    Dedicated*: `Regions()` is fr-par, nl-ams, pl-waw and it-mil.
  - `api/rdb/v1/rdb_sdk.go` (`fad3cd62e2`, 2026-11-12), both *Managed Database for PostgreSQL and
    MySQL* offerings: `Regions()` is fr-par, nl-ams and pl-waw. It still does not declare it-mil, so
    Milan stays out (that offering's section).
  - `api/mnq/v1beta1/mnq_sdk.go` (`84e9d4d533`, 2025-11-04), *Queues*: `SqsAPI.Regions()` is fr-par
    and nl-ams.
  - `api/function/v1beta1/function_sdk.go` (`28202c2d81`, 2026-10-09), *Serverless Functions*:
    `Regions()` is fr-par, nl-ams and pl-waw.
  - *Object Storage*: the SDK's `api/` directory at that commit still has no Object Storage package.
    `docs-content` `pages/object-storage/concepts.mdx` at commit
    `1ec45ae299fdea70effdd48e5a3c99f5d6dad8b1` still lists an S3 endpoint for each of fr-par, nl-ams,
    pl-waw and it-mil.

  The API reference pages and their schemas were not read again (`www.scaleway.com`).
  **Read on 2026-11-26 (PRD #552 milestone 6)** at `scaleway-sdk-go` commit
  `d5b724367ac9d6a0e9b39f91a9aabf0b55216360`, for the new offerings, with the commit that last changed
  each file:
  - `api/registry/v1/registry_sdk.go` (`d9b5685d2d`, 2026-06-15), *Container Registry*: `Regions()` is
    fr-par, nl-ams and pl-waw, so no Milan.
  - `api/lb/v1/lb_sdk.go` (`f7f6cc2eea`, 2026-11-06), *Load Balancer*: `ZonedAPI.Zones()` is fr-par-1,
    fr-par-2, nl-ams-1 to nl-ams-3 and pl-waw-1 to pl-waw-3, and `API.Regions()` fr-par, nl-ams and
    pl-waw, so no Milan.
  - `api/redis/v1/redis_sdk.go` (`e521727b25`, 2026-10-08), both Redis offerings: `Zones()` is fr-par-1,
    fr-par-2, nl-ams-1, nl-ams-2, pl-waw-1 and pl-waw-2.
  - `api/mongodb/v1/mongodb_sdk.go` (`07ad86c4e6`, 2026-10-13), both MongoDB offerings: `Regions()` is
    fr-par.
  - `api/container/v1/container_sdk.go` (`2a42febf50`, 2026-11-06), *Serverless Containers*:
    `Regions()` is fr-par, nl-ams, pl-waw and it-mil (the deprecated `v1beta1` lacks it-mil).
  - `api/mnq/v1beta1/mnq_sdk.go` (`84e9d4d533`, 2025-11-04), *Topics and Events*: `SnsAPI.Regions()` is
    fr-par and nl-ams.
  - `api/file/v1alpha1/file_sdk.go` (`bd5114c353`, 2026-11-03), File Storage, not in the snapshot
    (*Left out on purpose*): `Regions()` is fr-par.
  - `api/rdb/v1/rdb_sdk.go` at the same commit still declares fr-par, nl-ams and pl-waw only.
- **`sourceURL`** is Scaleway's pricing page, <https://www.scaleway.com/en/pricing/>, the index of the
  per-product price pages every plan and storage price comes from (*Prices*). Until the prices were added
  it was the documentation root, <https://www.scaleway.com/en/docs/>, where the capabilities still come
  from. The regions come from the availability page, which each offering's section cites. The prices
  read on 2026-11-13 changed no offering's regions (*Prices*, *Which region*). The re-check of
  2026-11-19 used the *Containers* price page as the source that states a tier in a region, for the
  tier variant *Kubernetes Kapsule Dedicated* only (*Rules applied*, tier-variant regions).

## Prices

Every plan price, storage price and fixed fee in `catalog.json` is a monthly list price in EUR, excluding
VAT (ADR 0024, ADR 0026),
read on 2026-11-13 over HTTPS without credentials and without calling any Scaleway API. The price pages
are the per-product pages linked from <https://www.scaleway.com/en/pricing/>:

- *Instances*: <https://www.scaleway.com/en/pricing/virtual-instances/>
- *Elastic Metal*: <https://www.scaleway.com/en/pricing/elastic-metal/>
- *Managed Databases*: <https://www.scaleway.com/en/pricing/managed-databases/>
- *Storage* (Block Storage and Object Storage): <https://www.scaleway.com/en/pricing/storage/>
- *Containers* (Kubernetes Kapsule control planes): <https://www.scaleway.com/en/pricing/containers/>

The Redis and MongoDB plans and the MongoDB storage price (PRD #552 milestone 6) were read on
2026-11-26 from the maintainer's saved copy of the *Managed Databases* page, which also re-read the
existing database and storage prices (*How it was compiled*); the hourly × 730 rule below applies to
them unchanged.

- **How the pages were read.** `www.scaleway.com` has answered HTTP 403 to plain clients before (*How it
  was compiled*). On 2026-11-13 the pages answered HTTP 200 to a plain HTTPS client (curl, with no
  browser user agent); a web-fetch tool was refused with HTTP 403 the same day. Each page carries its
  tables twice: as server-rendered HTML for its default zone or region (Paris: `PAR-1` or "Paris" in the
  page's *Select Availability Zone* or *Select Region* control), and as the page's own embedded data
  (`__NEXT_DATA__`), which holds the same rows for every zone or region the selector offers. Every price
  below was read from the embedded data for each zone or region, and every row cited was checked against
  the server-rendered Paris table (`POP2-2C-8G`: "€0.0735 / HOUR", "~€53.65 / MONTH"). The copies were
  kept in a scratch directory outside the repository and treated as data.
- **Currency and tax.** The pages show euros ("Price (€/hour)", "Price per month (€)"), and each table's
  legal notice says "Prices before tax." (the Elastic Metal tables: "Price before tax").
- **Hourly to monthly.** Every price the snapshot records is published per hour. The snapshot converts the
  **hourly price × 730**, rounded **half up to 4 fractional digits** (ADR 0024). That is also the basis of
  the monthly figure the pages show beside an hourly one: the *Instances* table's "Approx. per month"
  (`POP2-2C-8G`: 0.0735 × 730 = 53.655, shown "~€53.65") and the storage tables' "Approx. per month"
  (Object Storage Standard Multi-AZ: €0.000022/GB/hour, shown €0.01606/GB/month). No plan price needed
  rounding: each hourly price × 730 has at most four fractional digits (`PLAY2-PICO`: 0.01428 × 730 =
  `10.4244`). The storage prices that did are worked out in their sections.
- **Monthly figures not used.** Elastic Metal tables show a monthly price far below hourly × 730
  (`EM-A116X-SSD`: €27.99 / MONTH against €0.077 / HOUR, × 730 = `56.21`). It is the monthly billing
  option, and the legal notice says "A commitment fee equivalent to one month of server rental is charged
  for each Elastic Metal server order with monthly billing. No commitment fee is charged for hourly
  billing." The snapshot holds no-commitment list prices, so it records hourly × 730; a user on monthly
  billing pays less than the estimate.
- **Which region, and zones.** Instances, Elastic Metal and the database nodes are priced per zone
  (`fr-par-1`, `fr-par-2`, ...); the snapshot does not model zones (*Rules applied*), so a price applies to
  a region. Where every zone of a region where the plan is offered has the same price, that is the
  region's price. Where they differ, which happens in one place, the region's price is the one of its
  **first zones**: in Paris, `fr-par-1` and `fr-par-2` agree, and `fr-par-3` charges about 1.5 times more
  for every PRO2 and POP2 Instance (`POP2-2C-8G`: €0.1103 an hour in `fr-par-3`, €0.0735 in the other
  Paris zones and every other region). The server-rendered Paris table shows the `fr-par-1` price, so a Paris
  estimate is the price of a server in Paris's first two zones; a workload placed in `fr-par-3` costs more
  than the estimate. The `fr-par-3` prices are listed in *Instances* and are not recorded. Where a plan's
  prices are the same in every region of its offering, the snapshot has one price with an empty `regions`
  list; where the plan is offered only in some regions, its price lists them, and an estimate elsewhere
  says there is no price (ADR 0024). No recorded plan price, storage price or fixed fee differs between
  regions, only between zones: of Paris for Instances, and, since PRD #552 milestone 6, the second zones
  of Amsterdam and Warsaw for Redis (*Managed Database for Redis*, the `nl-ams-2` and `pl-waw-2`
  prices, not recorded).
- **Units.** The pages give memory in "GB" and storage prices per "GB"; the snapshot records them as GiB,
  unconverted, as the OVHcloud snapshot does. The database page's storage size limits are binary
  (the embedded data gives a minimum volume of 10737418240 bytes, 10 GiB), but its prices say "GB".
- **Linux prices.** Instance rows are read without a Windows licence; the `-WIN` POP2 rows are not
  recorded.
- **Managed databases.** A plan is one node of a Database Instance, with vCPU and memory as the page
  lists them; the price is **per node**, so an estimate multiplies it by the resource's count, which for a
  database is its nodes (ADR 0025). The page's table has three price columns: "Main node", "Additional
  nodes*" and "Multi-AZ option**", with the notes "*Additional node could be another node from the same
  type, a Read Replica or a HA node." and "**Multi-AZ option is available on Read Replica only". The main
  node's price is the additional node's plus a management fee that the page's data carries separately
  (`DB-POP2-2C-8G`: main €0.1434 = node €0.076 + management €0.0674). So a **Standalone** instance (one
  node) costs the main node's price, and a **High Availability** instance (the main node and one hot
  standby, the "HA node") costs the main node's price plus one additional node's. The HA offering's plan
  price is that pair's price × 730, divided by its two nodes, so that the count of 2 an inventory gives
  for an HA database comes back to the pair's price exactly: no plan's half needed rounding.
  That per-node price is right only when the inventory gives **count 2** for an HA pair; an HA database
  recorded as one resource with count 1 is priced at half the pair. A **Standalone** instance with
  count 2 (a primary and a Read Replica) is priced as two main nodes, while Scaleway charges one main
  node and one additional node, so that estimate is high (by the management fee).
- **Database storage.** The page states its storage prices once per table, per GB and month: "Block
  Storage 5K: €0.0993/GB/month. Block Storage 15k: €0.1489/GB/month." Its data gives the hourly prices
  behind them (€0.000136 and €0.000204 per GB and hour). No page read says whether an HA instance's
  standby volume is billed too: the page gives one storage price per Database Instance with no "per node"
  wording, and the HA standby keeps a full copy of the data. The snapshot records the published per-GB
  price for both offerings, per GiB of the service's data; it is **not** doubled for High Availability,
  and an HA estimate may be low by up to that much if Scaleway bills the standby's volume.
- **Managed Kubernetes.** The FAQ (<https://www.scaleway.com/en/docs/kubernetes/faq/>) says "Nodes are
  billed at the same price as the underlying Compute Instances. The Kubernetes control plane is provided
  without additional costs." So a Kubernetes plan is a **worker node flavour at its *Instances* price**,
  and an estimate counts nodes. The control plane is a fee per cluster, which a plan cannot hold (a
  plan is multiplied by the node count), so it is recorded as the offering's fixed fee per cluster
  (`fixedFees`, ADR 0026). The containers price page lists "Kapsule Mutualized" at €0 an hour, 0 × 730 =
  `0.00`, recorded for *Kubernetes Kapsule*. The Dedicated control planes cost Dedicated-4 €0.11, Dedicated-8
  €0.18 and Dedicated-16 €0.35 an hour; *Kubernetes Kapsule Dedicated* records the smallest,
  Dedicated-4, 0.11 × 730 = `80.30` a month, as a lower bound (that offering's section).
- **Plans chosen.** A fully cited subset of the general-purpose sizes a workload is matched against:
  Instances and Kubernetes nodes, the current Development range PLAY2 and the general-purpose ranges PRO2
  and POP2 (Linux); Elastic Metal, eight common servers that have one price in all their zones; the
  databases, the PLAY2, PRO2 and POP2 node types (the Cost Optimized and Production Optimized tables).
  The rest is under *Left out on purpose*.

## Licences of the sources

- **Pricing pages** (`www.scaleway.com/en/pricing/` and the per-product pages above): under Scaleway's
  copyright, with no open licence stated. Only facts are taken from them: plan names, sizes and prices,
  recorded as numbers, and brief quotations in quotation marks.
- **Documentation and changelog** (`www.scaleway.com/en/docs/`): built from `scaleway/docs-content`, which
  is licensed CC BY-NC-SA 4.0 (Creative Commons Attribution-NonCommercial-ShareAlike 4.0 International),
  as its `LICENSE.md` states at the commit above, and still states at commit
  `13ef235ee5926d0a675e9b87cc421df5105762b1`, read for PRD #552 milestone 6 on 2026-11-26. GitHub's
  licence detection reports it only as "Other".
  The site's footer reads "© 2023-2026 – Scaleway".
- **Availability page and API reference** (`www.scaleway.com/en/product-availability-by-region/`,
  `www.scaleway.com/en/developers/api/`): under Scaleway's copyright, with no open licence stated. The
  availability page's footer reads "© 1999-2026 - Scaleway SAS.", and the API reference reads "© 1999 -
  2026 Scaleway. All rights reserved." Only facts are taken from these pages: where each product is
  available, and which regions each API declares.
- **OpenAPI schemas** published with the API reference: they fall under the API reference's terms, so
  all rights reserved. Only facts are taken from them: the regions and zones each schema declares.
- `scaleway/scaleway-sdk-go` and `scaleway/scaleway-sdk-js` are licensed Apache-2.0, as the `LICENSE`
  file of each states at the commit cited (GitHub's licence API reports `Apache-2.0` for both at the
  commits read on 2026-11-26). Only facts are taken from them: the regions and zones each
  API declares.
- **Rule for this file and `catalog.json`:** take facts only (which product exists, in which regions, with
  which feature) and brief quotations, each in quotation marks and attributed to the page it comes from.
  Paraphrase everything else. Never copy documentation or web text in bulk, and keep it out of
  `catalog.json`, which holds only names, taxonomy terms, region codes, and plan sizes and prices
  recorded as numbers (facts), with no copied prose. A refresh follows the same rule.

## Rules applied

- **Regions:** an offering lists a region only when both of these hold:
  - the availability page marks the product "Available" there, either across the region or in at least
    one of its zones ("Available soon" does not count);
  - the product's API serves the region: the SDK declares it (or at least one zone in it), or the API
    reference's schema declares it. Object Storage has no API in the SDK (it is S3-compatible), so for it
    the documentation must name an S3 endpoint in the region.

  Where the two disagree, the region is left out until they agree, and the offering's section says so.
  Zones are not modelled. Regions are within the declared list below.
- **Capabilities:** only from the taxonomy vocabulary, and only when the documentation describes the
  feature as generally available. Features in public or private beta are left out and listed under
  *Left out on purpose*.
- **Target rule** (ADR 0011): an offering lists a capability only where the documentation states it
  **in full**. An offering that meets only part of a capability's definition does not list it, and
  *Left out on purpose* records the omission with its reason. A product that does not fully serve a
  taxonomy category is not in the snapshot for that category.
- **Tier-variant rule** (ADR 0011): all of an offering's capabilities hold in **one** tier. The base
  offering is the product's default tier, or where none is stated, its lowest tier: the tier that adds
  no capability. A tier that adds a capability is a variant named `<base offering> <tier>`, with its
  own section below. A tier that only lacks capabilities gets no offering. Here the base tiers are
  Standard Multi-AZ (Object Storage, its stated default), and Standalone (Managed Database) and
  Mutualized (Kubernetes Kapsule), their products' lowest tiers, since no page names a default for
  either. The variants are *Managed Database for PostgreSQL and MySQL High Availability* and
  *Kubernetes Kapsule Dedicated*. Since PRD #552 milestone 6 the same reading gives Standalone (Managed
  Database for Redis) and the 1-node replica set (Managed MongoDB) as base tiers, and the variants
  *Managed Database for Redis Cluster* and *Managed MongoDB Replica Set*. A **Needs:** line names the tier its offering is named after, or the
  base tier.
- **Tier-variant regions:** a tier variant lists only the regions where a source states that tier, or
  where the tier's general availability predates the region's opening; the product's availability row
  alone is not enough.
- **Scope:** high availability is listed at the level the documentation states in full:
  `node-high-availability` when the offering survives the loss of a node with no manual failover,
  `zone-high-availability` when it keeps serving through the loss of a whole zone. A **Scope:** line on
  the capability says which level, and why the other is not claimed. The catalog adds node level to
  every offering that lists zone level (ADR 0011).
- **Regions and capabilities** (PRD #11's rule): a capability that holds in only some of an offering's
  regions makes a separate offering in those regions. No Scaleway capability needs one today; see
  Object Storage for the Milan check.

## Declared EU regions

`fr-par` (Paris, France), `it-mil` (Milan, Italy), `nl-ams` (Amsterdam, the Netherlands), `pl-waw` (Warsaw,
Poland).

- The availability page has exactly these four regions, and labels each of them "3 AZ". SDK
  `scw/locality.go` defines the same four regions, each with three zones.
- `it-mil` is recent. The changelog entry *New region opening - MIL-1 is now available!* (2026-03-10)
  opens Milan with one zone, for "Instances, Kapsule, Block Storage, Object Storage, VPC, Databases, KMS,
  Audit Trail, and more". Serverless Containers and Container Registry arrived there on 2026-08-31. The
  entry *A full 3-AZ Region in Milan!* (2026-10-12) adds zones MIL-2 and MIL-3, naming Instances, Block
  Storage and the Object Storage Standard Multi-AZ class. Some pages still name only Paris, Amsterdam and
  Warsaw: the Instances FAQ, the Block Storage concepts page, and the introductions of the Kubernetes and
  Managed Database API references.
- **Re-read on 2026-11-21 (issue #328): no region or zone changed.** Still `fr-par`, `it-mil`, `nl-ams`
  and `pl-waw`, each with three zones. The files read:
  - SDK `scaleway/scaleway-sdk-go`, branch `main` (its default; `master` is stale) at commit
    `a251fd549cab893cd1fcdc7e9a2d382209f42531` (2026-11-19), file `scw/locality.go`: `AllRegions` is
    fr-par, nl-ams, pl-waw, it-mil, and `AllZones` is `<region>-1` to `<region>-3` for each. The file
    last changed in commit `5cd6bec617` (2026-10-08), which added it-mil-2 and it-mil-3.
  - `scaleway/docs-content`, branch `main` at commit `1ec45ae299fdea70effdd48e5a3c99f5d6dad8b1`
    (2026-11-20): `changelog/march2026/2026-03-09-global-added-test-global-changelog-entry.mdx` (the
    entry *New region opening - MIL-1 is now available!*, dated 2026-03-10 in its front matter),
    `changelog/august2026/2026-08-31-containers-added-serverless-containers-available-in-mil.mdx`,
    `changelog/august2026/2026-08-31-container-registry-added-container-registry-available-i.mdx` and
    `changelog/october2026/2026-10-08-partner-space-added-a-full-3-az-region-in-milan.mdx` (the entry
    *A full 3-AZ Region in Milan!*, dated 2026-10-12 in its front matter). Their text is unchanged. No
    changelog entry up to that commit opens another region or zone. `macros/concepts/region.mdx` still
    names only Paris, Amsterdam and Warsaw, like the pages listed above.
  - The availability page is not part of `docs-content` and `www.scaleway.com` is off limits to
    automated clients, so it was not read again. It was last read in full on 2026-11-10 and 2026-11-11,
    and its Kapsule row alone on 2026-11-19 (issue #211); a full re-read waits for the maintainer's
    saved copy of the page.
  - The per-product API check: *API reference and SDK, for checking regions* (above).

## Default region

`fr-par` (Paris, France) is the snapshot's `defaultRegion`: the region a cost estimate uses when the user names
none (PRD #190, ADR 0024). Chosen provisionally in milestone 2, it was **confirmed in milestone 4**, when the
prices were added, because it hosts the most offerings: all 10 of the snapshot. Since PRD #552 milestone 6
it hosts all 18. `nl-ams` hosts 16 (not the two MongoDB offerings), `pl-waw` 14 (no Queues, Topics and
Events or MongoDB) and `it-mil` 6 (Block Storage, Instances, Kubernetes Kapsule, Kubernetes Kapsule
Dedicated, since issue #211, Object Storage and, since milestone 6, Serverless Containers).
`fr-par` and `nl-ams` tie on prices for every offering both host: each plan and storage price they share
applies in both at the same amount (*Prices*). `fr-par` was kept over `nl-ams` because it was already the
default and is the first of the declared codes, kept in code order; since milestone 6 it is also the only
region with MongoDB, so it is the better default for an estate with a MongoDB database.

The choice says nothing about which region is cheapest or best. A Paris estimate prices a server in the
`fr-par-1` and `fr-par-2` zones; `fr-par-3` charges more for PRO2 and POP2 Instances (*Prices*, *Which
region, and zones*).

## Offerings

### Block Storage

Category `storage.block`. Pages: <https://www.scaleway.com/en/docs/block-storage/concepts/>,
<https://www.scaleway.com/en/docs/block-storage/faq/>.

- `snapshots`: the concepts page describes volumes that "can be snapshotted", and the FAQ covers snapshot
  export and import.
- Regions `fr-par`, `it-mil`, `nl-ams`, `pl-waw`: the availability page marks Block Storage 5k and Block
  Storage 15k available in all three zones of each of the four regions. SDK `api/block/v1` serves all
  twelve zones.

- **Storage price** (*Storage* price page, *Block Storage*, Paris, read 2026-11-13): the "Block Storage 5K"
  row, €0.000130/GB per hour, × 730 = 0.0949, so `0.0949` per GiB a month (no rounding needed), in every
  region; the page's data gives the same price in every zone of the four regions. The offering also covers
  Block Storage 15K (€0.000177, × 730 = 0.12921, would be 0.1292); an offering has one storage price per
  region, and 5K, the lower-performance class, is the one recorded. The data's hourly figure for 5K is
  0.000129999, which × 730 = 0.09489927 also rounds half up to `0.0949`.

### Container Registry

Category `registry.container` (PRD #552 milestone 6, read 2026-11-26). Pages:
<https://www.scaleway.com/en/docs/container-registry/faq/>; the changelog entry *Container Registry is
now available in the Milan region* (2026-08-31,
`changelog/august2026/2026-08-31-container-registry-added-container-registry-available-i.mdx`), all from
`scaleway/docs-content` at commit `13ef235ee5926d0a675e9b87cc421df5105762b1`.

- Category fit: private OCI image storage. The FAQ: "You can store any docker container image on the
  Namespace and it is possible to set the visibility of each image towards your needs. It can either be
  private or public."
- No capability. No Container Registry page mentions encryption (none in any of its pages at that
  commit), so `encryption-at-rest` is not listed (target rule), and nothing states a zone-level
  continuity, so no `zone-high-availability`. Every ECR, Artifact Registry and ACR resource the guide
  writes lists `encryption-at-rest`, so **every source registry is `partial` on Scaleway**, missing
  `encryption-at-rest` (*Left out on purpose*).
- Regions `fr-par`, `nl-ams`, `pl-waw`: the availability page (saved copy of
  <https://www.scaleway.com/en/product-availability-by-region/>, read 2026-11-26) marks Container
  Registry "Available across the region" in all four regions, and its embedded data agrees. SDK
  `api/registry/v1` declares fr-par, nl-ams and pl-waw (*API reference and SDK*), and the JavaScript SDK's
  `registry/src/v1` agrees. **Milan is left out under the regions rule**: the availability page and the
  changelog entry ("Container Registry is now available in the Milan region (`it-mil`).") say yes, but
  no SDK declares it, and the FAQ's traffic note names only "AMS ↔ PAR, WAW ↔ PAR, or AMS ↔ WAW". Add
  `it-mil` once the SDK or the API reference's schema declares it.
- **Not priced** (decided question 6 of PRD #552: a registry is not estimated, reason `not-priced`): no
  plan, storage price or fixed fee. For the record, the FAQ gives "Private images | €0.027/GB/month";
  it is not recorded.

### Elastic Metal

Category `compute.baremetal`: dedicated physical servers running a general purpose operating system, with
no hypervisor. Until issue #187 (ADR 0023) it served `compute.vm`, and tied with Instances for a virtual
machine workload, so a group of VMs could be pointed at bare-metal servers. Moving it changed no source
fact. Pages:
<https://www.scaleway.com/en/docs/elastic-metal/faq/>,
<https://www.scaleway.com/en/docs/elastic-metal/reference-content/elastic-metal-networking/>,
<https://www.scaleway.com/en/docs/ipam/reference-content/ipv6/>.

- `ipv6`: the FAQ says servers are "delivered with an IPv6 address (/128) pre-configured", and flexible
  IPv6 /64 subnets can be ordered. The page *IPv6 and the Scaleway ecosystem* lists Elastic Metal among
  the products that support IPv6.
- Regions `fr-par`, `nl-ams`, `pl-waw`: the availability page marks the Aluminium, Beryllium, Iridium and
  Lithium ranges available in two zones each of Paris, Amsterdam and Warsaw. It marks no Elastic Metal
  range available in Milan. SDK `api/baremetal/v1` serves fr-par-1, fr-par-2, nl-ams-1, nl-ams-2,
  pl-waw-2 and pl-waw-3.

- **Prices** (*Elastic Metal* price page, read 2026-11-13): eight servers of the Aluminium, Beryllium and
  Iridium ranges, each with one hourly price in every zone it is offered in, and offered in at least one
  zone of each of the offering's three regions, so each has one price with an empty `regions` list. A
  plan's vCPU count is the server's hardware threads ("6C / 12T" is 12, ADR 0024: physical CPU threads for
  bare metal); its included storage is the total of its disks as published, in GB recorded as GiB ("2 ×
  1.02 TB" is 2040, "2 × 960 GB" is 1920), with no RAID assumed. The price is the hourly price × 730 (no
  commitment); the monthly-billing price is shown for reference only (*Prices*).

| Plan | Cores / threads | Memory | Disks on the page | EUR/hour | EUR/month (× 730) | Monthly-billing price, not recorded | Price regions |
|---|---|---|---|---|---|---|---|
| `EM-A116X-SSD` | 4C / 4T | 32 GB | 2 × 1.02 TB SSD (`storageGiB` 2040) | 0.077 | `56.21` | €27.99 | all |
| `EM-A410X-SSD` | 6C / 12T | 64 GB | 2 × 1.02 TB SSD (`storageGiB` 2040) | 0.153 | `111.69` | €54.99 | all |
| `EM-A610R-NVMe` | 6C / 12T | 32 GB | 2 × 1.02 TB NVMe (`storageGiB` 2040) | 0.11 | `80.30` | €39.99 | all |
| `EM-B220E-NVMe` | 8C / 16T | 64 GB | 2 × 1.02 TB NVMe (`storageGiB` 2040) | 0.333 | `243.09` | €119.99 | all |
| `EM-I120E-NVMe` | 8C / 16T | 64 GB | 2 × 960 GB NVMe (`storageGiB` 1920) | 0.37 | `270.10` | €134.99 | all |
| `EM-I220E-NVMe` | 16C / 32T | 128 GB | 2 × 960 GB NVMe (`storageGiB` 1920) | 0.63 | `459.90` | €229.99 | all |
| `EM-I320E-NVMe` | 24C / 48T | 192 GB | 2 × 1.92 TB NVMe (`storageGiB` 3840) | 0.959 | `700.07` | €349.99 | all |
| `EM-I420E-NVMe` | 32C / 64T | 256 GB | 2 × 1.92 TB NVMe (`storageGiB` 3840) | 1.233 | `900.09` | €449.99 | all |

- **No storage price.** Re-examined on 2026-11-14 (issue #219): an Elastic Metal server's storage is the
  fixed local disks of its plan, and Scaleway sells no volume to extend it. The FAQ (saved copy of
  <https://www.scaleway.com/en/docs/elastic-metal/faq/>, read 2026-11-14) says "Elastic Metal is not
  currently compatible with Block Storage, but this functionality is planned for the future." The price
  page (saved copy of <https://www.scaleway.com/en/pricing/elastic-metal/>, read 2026-11-14) lists only
  Network and Windows Licences add-ons, with no storage, disk or volume row, and every server in its
  embedded data has `fixedLocal` storage only. A resource needing more than its plan's included storage
  is therefore "not estimated" on the storage, rather than priced at a Block Storage volume it cannot
  attach. When the FAQ's planned Block Storage support ships, this offering may take the *Block Storage*
  price.

### Instances

Category `compute.vm` (virtual machines; Elastic Metal is `compute.baremetal`). Pages:
<https://www.scaleway.com/en/docs/instances/concepts/>,
<https://www.scaleway.com/en/docs/instances/how-to/use-flexips/>,
<https://www.scaleway.com/en/docs/ipam/reference-content/ipv6/>.

- `ipv6`: flexible routed IPv6 gives each Instance a public /64 prefix (use-flexips page). The page *IPv6
  and the Scaleway ecosystem* lists Instances among the products that support IPv6.
- `snapshots`: the concepts page defines snapshots and images built from them; the Elastic Metal FAQ
  contrasts Elastic Metal with "Instances, which have the snapshot feature".
- Regions `fr-par`, `it-mil`, `nl-ams`, `pl-waw`: the availability page marks the PRO2 range, among
  others, available in all three zones of Paris, Amsterdam and Warsaw. It marks the BASIC3-X,
  STANDARD3-X, COMPUTE3-X and MEMORY3-X ranges available in all three Milan zones, as well as in Paris
  and Amsterdam. SDK
  `api/instance/v1` serves every Paris, Amsterdam and Warsaw zone but only it-mil-1; both Milan changelog
  entries name Instances.

- **Prices** (*Instances* price page, *General Purpose* and *Development* tables, Linux, read 2026-11-13):
  the Development range PLAY2 and the General Purpose ranges PRO2 and POP2, at the price of their
  `fr-par-1` and `fr-par-2` zones in Paris (*Prices*, *Which region, and zones*). The page's data gives
  PLAY2 and PRO2 in Paris, Amsterdam and Warsaw only, so their prices list those three regions and an
  estimate in `it-mil` finds no price for them. POP2 has one price in every zone it is offered in,
  including `it-mil-1`, so one price with an empty `regions` list. The rows give storage only as Block
  Storage, priced apart (*Block Storage*), so no plan includes storage. The last column is the
  `fr-par-3` price, not recorded.
- **Storage price:** an Instance's root and data volumes are *Block Storage* volumes, billed separately
  from the Instance. The snapshot records the *Block Storage* price for them (*Storage* price page, the
  "Block Storage 5K" row, €0.000130/GB per hour, × 730 = `0.0949` per GiB a month, read 2026-11-13; see
  *Block Storage*). The page's data gives that price in every zone of the four regions, the regions of
  this offering, so one price with an empty `regions` list.

| Plan | vCPU | Memory | EUR/hour | EUR/month (× 730) | Price regions | `fr-par-3` EUR/hour, not recorded |
|---|---|---|---|---|---|---|
| `PLAY2-PICO` | 1 | 2 GB | 0.01428 | `10.4244` | `fr-par`, `nl-ams`, `pl-waw` | not offered |
| `PLAY2-NANO` | 2 | 4 GB | 0.02754 | `20.1042` | `fr-par`, `nl-ams`, `pl-waw` | not offered |
| `PLAY2-MICRO` | 4 | 8 GB | 0.05508 | `40.2084` | `fr-par`, `nl-ams`, `pl-waw` | not offered |
| `PRO2-XXS` | 2 | 8 GB | 0.0561 | `40.953` | `fr-par`, `nl-ams`, `pl-waw` | 0.08364 |
| `PRO2-XS` | 4 | 16 GB | 0.1122 | `81.906` | `fr-par`, `nl-ams`, `pl-waw` | 0.16728 |
| `PRO2-S` | 8 | 32 GB | 0.22338 | `163.0674` | `fr-par`, `nl-ams`, `pl-waw` | 0.33558 |
| `PRO2-M` | 16 | 64 GB | 0.44676 | `326.1348` | `fr-par`, `nl-ams`, `pl-waw` | 0.67116 |
| `PRO2-L` | 32 | 128 GB | 0.89454 | `653.0142` | `fr-par`, `nl-ams`, `pl-waw` | 1.3413 |
| `POP2-2C-8G` | 2 | 8 GB | 0.0735 | `53.655` | all | 0.1103 |
| `POP2-4C-16G` | 4 | 16 GB | 0.147 | `107.31` | all | 0.2205 |
| `POP2-6C-24G` | 6 | 24 GB | 0.2205 | `160.965` | all | 0.33075 |
| `POP2-8C-32G` | 8 | 32 GB | 0.29 | `211.70` | all | 0.435 |
| `POP2-12C-48G` | 12 | 48 GB | 0.435 | `317.55` | all | 0.6525 |
| `POP2-16C-64G` | 16 | 64 GB | 0.59 | `430.70` | all | 0.885 |
| `POP2-24C-96G` | 24 | 96 GB | 0.885 | `646.05` | all | 1.3275 |
| `POP2-32C-128G` | 32 | 128 GB | 1.18 | `861.40` | all | 1.77 |
| `POP2-48C-192G` | 48 | 192 GB | 1.77 | `1292.10` | all | 2.655 |
| `POP2-64C-256G` | 64 | 256 GB | 2.35 | `1715.50` | all | 3.525 |

### Kubernetes Kapsule

Category `kubernetes.managed`. Pages: <https://www.scaleway.com/en/docs/kubernetes/concepts/>,
<https://www.scaleway.com/en/docs/kubernetes/faq/>,
<https://www.scaleway.com/en/docs/kubernetes/reference-content/kubernetes-control-plane-offers/>.

The base offering is the lowest control-plane tier, **Mutualized**: a shared control plane that the
concepts page describes as "suitable for development, testing, staging environments or production
workloads with standard availability requirements". No page read names a default tier. Mutualized is
taken as the base because it is the tier that adds no capability; every Dedicated tier adds one. The
control-plane type is chosen when the cluster is created (FAQ).

- `autoscaling`: pools autoscale between a minimum and maximum node count (concepts *Autoscale*, FAQ).
  **Needs:** nothing beyond the base tier, Mutualized.
- No high availability: the Mutualized tier runs "1 resilient replica" of the API server and has no SLA
  (control-plane offers page). Its etcd keeps "3 replicas in multi-AZ", which protects the cluster's
  state, not the API server's availability. The Dedicated tiers add high availability, in the variant
  *Kubernetes Kapsule Dedicated*.
- Regions `fr-par`, `it-mil`, `nl-ams`, `pl-waw`: the availability page marks Kapsule "Available across
  the region" in all four regions, and the changelog entry opening MIL-1 (2026-03-10) names Kapsule. SDK
  `api/k8s/v1` and the schema of the Kubernetes API reference
  (<https://www.scaleway.com/en/developers/api/kubernetes/>) declare all four regions, although the API
  reference's introduction names only Paris, Amsterdam and Warsaw.

- **Prices** (*Instances* price page, read 2026-11-13; FAQ
  <https://www.scaleway.com/en/docs/kubernetes/faq/>): "Nodes are billed at the same price as the
  underlying Compute Instances. The Kubernetes control plane is provided without additional costs." So a
  plan is a **worker node flavour at its *Instances* price** (*Instances*), and the Mutualized control
  plane, listed at €0 an hour on the *Containers* price page
  (<https://www.scaleway.com/en/pricing/containers/>, read 2026-11-13, "Kapsule Mutualized"), adds
  nothing. **Fixed fee per cluster** (the control plane, ADR 0026): €0 an hour, 0 × 730 = `0.00`, with an empty `regions`
  list, since the page gives the Mutualized control plane one price, €0, in every region of this
  offering, `it-mil` included. The fee is charged once per cluster, never per node. The
  node flavours are those of *Instances*. Which Instance types may be Kapsule nodes is an **inference**:
  no page read lists them, but the *Instances* price page's data offers a "Kapsule" machine image for every
  PLAY2, PRO2 and POP2 type, in the same zones as the type itself. Prices and regions are those of
  *Instances*: PLAY2 and PRO2 in Paris, Amsterdam and Warsaw, POP2 in every region.
- **Storage price:** a Kapsule node's root volume and the cluster's persistent volumes are *Block
  Storage* volumes, billed separately from the node, as for *Instances*. The snapshot records the *Block
  Storage* price, `0.0949` per GiB a month (the "Block Storage 5K" row, €0.000130/GB per hour × 730, read
  2026-11-13; see *Block Storage*), the same in every zone of the four regions of this offering, so one
  price with an empty `regions` list.

| Plan | vCPU | Memory | EUR/hour | EUR/month (× 730) | Price regions |
|---|---|---|---|---|---|
| `PLAY2-PICO` | 1 | 2 GB | 0.01428 | `10.4244` | `fr-par`, `nl-ams`, `pl-waw` |
| `PLAY2-NANO` | 2 | 4 GB | 0.02754 | `20.1042` | `fr-par`, `nl-ams`, `pl-waw` |
| `PLAY2-MICRO` | 4 | 8 GB | 0.05508 | `40.2084` | `fr-par`, `nl-ams`, `pl-waw` |
| `PRO2-XXS` | 2 | 8 GB | 0.0561 | `40.953` | `fr-par`, `nl-ams`, `pl-waw` |
| `PRO2-XS` | 4 | 16 GB | 0.1122 | `81.906` | `fr-par`, `nl-ams`, `pl-waw` |
| `PRO2-S` | 8 | 32 GB | 0.22338 | `163.0674` | `fr-par`, `nl-ams`, `pl-waw` |
| `PRO2-M` | 16 | 64 GB | 0.44676 | `326.1348` | `fr-par`, `nl-ams`, `pl-waw` |
| `PRO2-L` | 32 | 128 GB | 0.89454 | `653.0142` | `fr-par`, `nl-ams`, `pl-waw` |
| `POP2-2C-8G` | 2 | 8 GB | 0.0735 | `53.655` | all |
| `POP2-4C-16G` | 4 | 16 GB | 0.147 | `107.31` | all |
| `POP2-6C-24G` | 6 | 24 GB | 0.2205 | `160.965` | all |
| `POP2-8C-32G` | 8 | 32 GB | 0.29 | `211.70` | all |
| `POP2-12C-48G` | 12 | 48 GB | 0.435 | `317.55` | all |
| `POP2-16C-64G` | 16 | 64 GB | 0.59 | `430.70` | all |
| `POP2-24C-96G` | 24 | 96 GB | 0.885 | `646.05` | all |
| `POP2-32C-128G` | 32 | 128 GB | 1.18 | `861.40` | all |
| `POP2-48C-192G` | 48 | 192 GB | 1.77 | `1292.10` | all |
| `POP2-64C-256G` | 64 | 256 GB | 2.35 | `1715.50` | all |

### Kubernetes Kapsule Dedicated

Category `kubernetes.managed`. A variant of *Kubernetes Kapsule* (tier-variant rule): a cluster with a
Dedicated control plane, Dedicated 4, 8 or 16, which share the same availability row. Pages:
<https://www.scaleway.com/en/docs/kubernetes/reference-content/kubernetes-control-plane-offers/>,
<https://www.scaleway.com/en/docs/kubernetes/concepts/>,
<https://www.scaleway.com/en/docs/kubernetes/reference-content/multi-az-clusters/>.

- `autoscaling`: as for the base offering; autoscaling has no tier restriction (concepts *Autoscale*).
  **Needs:** nothing beyond this variant's tier, a Dedicated control plane (autoscaling itself has no
  tier restriction).
- `node-high-availability`. **Needs:** a Dedicated control plane (Dedicated 4, 8 or 16). The
  control-plane offers page gives these tiers "2 replicas for HA" of the API server, etcd "3 replicas in
  multi-AZ", and a "99.5% uptime" SLA (the Mutualized tier: "1 resilient replica", SLA "N/A"). So the
  control plane survives the loss of a node with no manual failover. **Scope: node level only.** The
  concepts page says a regional cluster spreads control-plane replicas "across multiple zones", and is
  "only available with HA Dedicated control planes". But the Multi-AZ clusters page, *Limitations*,
  says the control plane is reached through a Load Balancer in the region's primary zone, and "If this
  zone fails globally, the control plane will be unreachable, even if the cluster spans multiple zones.
  This limitation also applies to HA Dedicated control planes." The cluster therefore does not keep
  serving through the loss of every zone, and `zone-high-availability` is not claimed (target rule).
- Regions `fr-par`, `it-mil`, `nl-ams`, `pl-waw` (tier-variant regions rule). The changelog entry
  *Dedicated control plane offers are now available!* (2023-09-06) announces general availability; it
  names no region and predates Warsaw and Milan. Warsaw has its own entry, *Dedicated control plane
  offers now available in Warsaw* (2023-12-15). `fr-par` and `nl-ams` are inferred from the GA date and
  the product's availability row, which marks Kapsule in both. No documentation page read names Paris or
  Amsterdam for the Dedicated tier.
  **Milan, added on 2026-11-19 (issue #211).** Until then Milan was left out, because nothing read
  stated Dedicated control planes there, Milan opened after the GA entry, the entry opening MIL-1
  (2026-03-10) names "Kapsule", not the tier, and the availability page has one Kapsule row. The
  re-check found a source that states the tier per region: the *Containers* price page
  (<https://www.scaleway.com/en/pricing/containers/>, read 2026-11-19 from a copy the maintainer saved
  in a browser). With Milan selected, its Kapsule table prices "Kapsule Dedicated-4" at "€0.11 /HOUR"
  and "Kapsule Dedicated-8" at "€0.18 /HOUR", and says of "Kapsule Dedicated-16": "Not available in this
  region". Its embedded data agrees: it holds the price SKUs
  `/k8s/control-plane/kapsule-dedicated-4/it-mil` and `/k8s/control-plane/kapsule-dedicated-8/it-mil`,
  and Dedicated-16 SKUs only for `fr-par`, `nl-ams` and `pl-waw`. A statement, per region, of which
  Dedicated tiers can be ordered is the source the tier-variant regions rule asks for. The two checks of
  the regions rule hold too, read the same day: the availability page
  (<https://www.scaleway.com/en/product-availability-by-region/>, the page and its CSV export, which
  agree) still has one Kapsule row, "Available across the region" in Paris, Amsterdam, Warsaw and Milan,
  with no separate row for Dedicated control planes; and SDK `api/k8s/v1` (`scaleway/scaleway-sdk-go`,
  default branch `main` at commit `58b0d2c460afae73448b2392176d508f37d42b71`, file
  `api/k8s/v1/k8s_sdk.go`) declares `Regions()` fr-par, nl-ams, pl-waw and it-mil. The documentation
  source, `scaleway/docs-content` at commit `e10b284732eceefcaf55a5bdebdb20641f370fb5` (the Kubernetes
  FAQ, the control-plane offers page and *Changing the control plane offer*), states no region for any
  tier: only an example command's output and a note about the CLI's default region name `fr-par`.
  **Dedicated-16 is not offered in Milan,** so in `it-mil` the largest Dedicated control plane is
  Dedicated-8 (€131.40 a month). Dedicated-16 is not recorded in any region (*Left out on purpose*), so
  nothing in the snapshot picks or prices it in Milan.

- **Prices** (*Instances* price page, read 2026-11-13): the worker nodes, as for *Kubernetes Kapsule*
  (the same inference about which Instance types are nodes), with the same price regions: PLAY2 and PRO2
  are priced in Paris, Amsterdam and Warsaw only, so their prices list those three regions and an
  estimate in `it-mil` finds no price for them; POP2 has one price in every region, so an empty
  `regions` list. The *Containers* price page read on 2026-11-19 confirms these price regions: its
  embedded node SKUs (`/instance/server/<type>/<zone>`) price PLAY2 and PRO2 only in zones of `fr-par`,
  `nl-ams` and `pl-waw`, and every recorded POP2 type in those and in `it-mil-1` too.
- **Fixed fee per cluster** (the control plane, ADR 0026; *Containers* price page
  <https://www.scaleway.com/en/pricing/containers/>, read 2026-11-13 and again on 2026-11-19 for
  `it-mil`): Dedicated-4 costs €0.11 an hour per cluster, × 730 = 80.30, recorded as `80.30` with an
  empty `regions` list, since the page gives it one price in all four regions of this offering, `it-mil`
  included. The fee is charged once per cluster, never per node.
  Dedicated-4 is the **smallest paid control plane**, and an estimate uses it as a **lower bound**: the
  larger tiers cost Dedicated-8 €0.18 and Dedicated-16 €0.35 an hour, × 730 = €131.40 and €255.50 a
  month (Dedicated-16 not in `it-mil`, above), and the Kubernetes FAQ says dedicated control planes have
  "a minimum commitment period of 30 calendar days". The larger tiers are not recorded (*Left out on
  purpose*). Choosing Dedicated-4 is a judgement the maintainer may overrule.
- **Storage price:** as for *Kubernetes Kapsule*: node and persistent volumes are *Block Storage*
  volumes, billed separately, at `0.0949` per GiB a month (the "Block Storage 5K" row, read 2026-11-13;
  see *Block Storage*), the same in every zone of the four regions of this offering, so one price with
  an empty `regions` list. The *Containers* price page read on 2026-11-19 gives the 5K volume the same
  hourly price in every zone of the four regions, `it-mil-1` to `it-mil-3` included.

| Plan | vCPU | Memory | EUR/hour | EUR/month (× 730) | Price regions |
|---|---|---|---|---|---|
| `PLAY2-PICO` | 1 | 2 GB | 0.01428 | `10.4244` | `fr-par`, `nl-ams`, `pl-waw` |
| `PLAY2-NANO` | 2 | 4 GB | 0.02754 | `20.1042` | `fr-par`, `nl-ams`, `pl-waw` |
| `PLAY2-MICRO` | 4 | 8 GB | 0.05508 | `40.2084` | `fr-par`, `nl-ams`, `pl-waw` |
| `PRO2-XXS` | 2 | 8 GB | 0.0561 | `40.953` | `fr-par`, `nl-ams`, `pl-waw` |
| `PRO2-XS` | 4 | 16 GB | 0.1122 | `81.906` | `fr-par`, `nl-ams`, `pl-waw` |
| `PRO2-S` | 8 | 32 GB | 0.22338 | `163.0674` | `fr-par`, `nl-ams`, `pl-waw` |
| `PRO2-M` | 16 | 64 GB | 0.44676 | `326.1348` | `fr-par`, `nl-ams`, `pl-waw` |
| `PRO2-L` | 32 | 128 GB | 0.89454 | `653.0142` | `fr-par`, `nl-ams`, `pl-waw` |
| `POP2-2C-8G` | 2 | 8 GB | 0.0735 | `53.655` | all |
| `POP2-4C-16G` | 4 | 16 GB | 0.147 | `107.31` | all |
| `POP2-6C-24G` | 6 | 24 GB | 0.2205 | `160.965` | all |
| `POP2-8C-32G` | 8 | 32 GB | 0.29 | `211.70` | all |
| `POP2-12C-48G` | 12 | 48 GB | 0.435 | `317.55` | all |
| `POP2-16C-64G` | 16 | 64 GB | 0.59 | `430.70` | all |
| `POP2-24C-96G` | 24 | 96 GB | 0.885 | `646.05` | all |
| `POP2-32C-128G` | 32 | 128 GB | 1.18 | `861.40` | all |
| `POP2-48C-192G` | 48 | 192 GB | 1.77 | `1292.10` | all |
| `POP2-64C-256G` | 64 | 256 GB | 2.35 | `1715.50` | all |

### Load Balancer

Category `network.loadbalancer` (PRD #552 milestone 6, read 2026-11-26). Pages:
<https://www.scaleway.com/en/docs/load-balancer/faq/>,
<https://www.scaleway.com/en/docs/load-balancer/reference-content/load-balancers-limitations/>,
<https://www.scaleway.com/en/docs/ipam/reference-content/ipv6/>, all from `scaleway/docs-content` at
commit `13ef235ee5926d0a675e9b87cc421df5105762b1`.

- `ipv6`. **Needs:** nothing (no tier). The FAQ, *Do Load Balancers support external IPv6
  connections?*: "Yes, Load Balancer supports both IPv4 and IPv6 addresses at the frontend. IPv6 can
  also be used to communicate between the Load Balancer and your backend servers." The limitations page:
  "IPv6 is also supported between your Load Balancer and backend servers." The page *IPv6 and the
  Scaleway ecosystem* lists Load Balancer ("Scaleway Load Balancers are compatible with IPv6"). Both
  directions are stated, so `ipv6` holds in its strict meaning (ADR 0011). Caveat on the same pages: "A
  public Load Balancer must have an IPv4 address, it cannot have only an IPv6 address."
- No `udp`: the FAQ says "Scaleway Load Balancer does not currently support `UDP`." and "All protocols
  based on `TCP` are supported." A source load balancer with a UDP listener is `partial` here.
- No `node-high-availability` (PRD #552 milestone 6's decision 3). The FAQ says "In case of a failure
  of the Load Balancer, a replica Load Balancer is immediately spawned and deployed, and the IP address
  is automatically rerouted to this replica." The failover is automatic, but it is a replica built after
  the failure, not a standing one: recovery, not continuity, as PRD #14 decided for Object Storage's
  rebuilt data (*Left out on purpose*). No `zone-high-availability` either: a load balancer lives in
  one zone.
- Regions `fr-par`, `nl-ams`, `pl-waw`: the availability page (saved copy of
  <https://www.scaleway.com/en/product-availability-by-region/>, read 2026-11-26) marks Load Balancer
  "Available" in all twelve zones, Milan's three included, and its embedded data agrees. SDK
  `api/lb/v1` declares zones in Paris (fr-par-1 and fr-par-2), Amsterdam and Warsaw and the regions
  fr-par, nl-ams and pl-waw (*API reference and SDK*); the JavaScript SDK agrees. **Milan is left out
  under the regions rule**: the availability page and the price page's data (an `it-mil-1` price for
  every size) say yes, no SDK declares it. Add `it-mil` once the SDK or the API reference's schema
  declares it.
- **Not priced** (decided question 6 of PRD #552): Scaleway sells sizes LB-S, LB-M, LB-L and LB-XL
  (200 Mbps to 4 Gbps, limitations page), a size no inventory records, so the offering has no plan,
  storage price or fixed fee, and a load balancer resource is not estimated, reason `not-priced`.

### Managed Database for PostgreSQL and MySQL

Categories `database.mysql` and `database.postgresql`: one product for both engines, with the same
features. Pages: <https://www.scaleway.com/en/docs/managed-databases-for-postgresql-and-mysql/concepts/>,
<https://www.scaleway.com/en/docs/managed-databases-for-postgresql-and-mysql/faq/>,
<https://www.scaleway.com/en/docs/managed-databases-for-postgresql-and-mysql/how-to/create-a-database/>,
<https://www.scaleway.com/en/docs/managed-databases-for-postgresql-and-mysql/how-to/manage-read-replicas/>.

The base offering is the lowest node configuration, **Standalone**: the page *Create a Database
Instance* offers "High Availability", listed first, and "Standalone: creates a standalone database
provisioned on a single node". No page read names a default. Standalone is taken as the base because
it is the tier that adds no capability; High Availability adds one. The concepts page says a
standalone node can be upgraded to High Availability after creation, "but not the opposite". The High
Availability option is the variant *Managed Database for PostgreSQL and MySQL High Availability*.
Every capability below has **Needs:** nothing beyond the base tier, Standalone.

- `encryption-at-rest`: LUKS volume encryption, generally available since 2025-01-16 (changelog).
- `read-replicas`: Read Replicas for PostgreSQL and MySQL Database Instances. The page *Manage Read
  Replicas* sets no High Availability requirement, so both tiers list it.
- `snapshots`: Database snapshots of the instance's volume (concepts *Database snapshot*).
- No high availability: a Standalone instance runs on a single node.
- Regions `fr-par`, `nl-ams`, `pl-waw`: the availability page marks the product "Available across the
  region" in each of them. SDK `api/rdb/v1` and the schema of the Managed Database API reference
  (<https://www.scaleway.com/en/developers/api/managed-database-postgre-mysql/>) declare these three
  regions.
- **Milan is left out until the API declares it.** The availability page also marks the product
  "Available across the region" in Milan, but nothing in its API declares `it-mil`. The API reference
  says the product "is available in the Paris, Amsterdam and Warsaw regions", its OpenAPI schema has no
  `it-mil`, and SDK `api/rdb/v1` declares the same three regions. The Kubernetes API's schema, by
  contrast, does declare `it-mil`. No request was sent to the API, so this records what the API
  declares, not how it would answer a request for Milan. The other evidence, on each side:
  - For Milan: the changelog entry opening MIL-1 names "Databases" without saying which product. The
    documentation page *Audit Trail product integration*, file
    `pages/audit-trail/reference-content/resource-integration-with-adt.mdx` in `scaleway/docs-content` at
    commit `e149770a6eacd436b33ef8c36ad40cdff90ebdca` (retrieved 2026-11-10; CC BY-NC-SA 4.0), lists
    "PostgreSQL & MySQL" among the products integrated with Audit Trail, with the supported regions
    fr-par, nl-ams, pl-waw and it-mil.
  - Against Milan: Scaleway's official JavaScript SDK, file `packages_generated/rdb/src/v1/api.gen.ts`
    in `scaleway/scaleway-sdk-js` at commit `c3924dd9e33b03b634f05dec84534d7db17443b9`, the head of
    `main` on the retrieval date (retrieved 2026-11-10; Apache-2.0), declares the Managed Database API's
    regions as fr-par, nl-ams and pl-waw only.

  Add `it-mil` once the SDK or the API reference's schema declares it.

- **Prices** (*Managed Databases* price page, tables "Managed PostgreSQL / MySQL - Cost Optimized" and
  "Managed PostgreSQL / MySQL - Production Optimized", read 2026-11-13): the PLAY2, PRO2 and POP2 node
  types, each with one price in every zone of the three regions of this offering. A Standalone instance
  runs one node, so the plan's price is the "Main node" column × 730, per node (*Prices*, *Managed
  databases*). The rows give storage as a separately priced Block volume, so no plan includes storage.

| Plan | vCPU | Memory | Main node EUR/hour | EUR/month (× 730) | Price regions |
|---|---|---|---|---|---|
| `DB-PLAY2-PICO` | 1 | 2 GB | 0.0233 | `17.009` | all |
| `DB-PLAY2-NANO` | 2 | 4 GB | 0.0432 | `31.536` | all |
| `DB-PRO2-XXS` | 2 | 8 GB | 0.11 | `80.30` | all |
| `DB-PRO2-XS` | 4 | 16 GB | 0.22 | `160.60` | all |
| `DB-PRO2-S` | 8 | 32 GB | 0.438 | `319.74` | all |
| `DB-PRO2-M` | 16 | 64 GB | 0.876 | `639.48` | all |
| `DB-PRO2-L` | 32 | 128 GB | 1.64 | `1197.20` | all |
| `DB-POP2-2C-8G` | 2 | 8 GB | 0.1434 | `104.682` | all |
| `DB-POP2-4C-16G` | 4 | 16 GB | 0.2866 | `209.218` | all |
| `DB-POP2-8C-32G` | 8 | 32 GB | 0.5684 | `414.932` | all |
| `DB-POP2-16C-64G` | 16 | 64 GB | 1.1269 | `822.637` | all |
| `DB-POP2-32C-128G` | 32 | 128 GB | 2.1712 | `1584.976` | all |

- **Storage price:** the page's "Block Storage 5K: €0.0993/GB/month", whose hourly price in the page's
  data is €0.000136 per GB, × 730 = 0.09928, rounded half up to `0.0993` per GiB a month, in every region
  of the offering. Block Storage 15K (€0.000204, × 730 = 0.14892, shown €0.1489) is not recorded: one
  storage price per region, and 5K is the lower class.

### Managed Database for PostgreSQL and MySQL High Availability

Categories `database.mysql` and `database.postgresql`. A variant of *Managed Database for PostgreSQL and
MySQL* (tier-variant rule): a Database Instance created with, or upgraded to, the High Availability node
configuration. Pages:
<https://www.scaleway.com/en/docs/managed-databases-for-postgresql-and-mysql/concepts/>,
<https://www.scaleway.com/en/docs/managed-databases-for-postgresql-and-mysql/faq/>,
<https://www.scaleway.com/en/docs/managed-databases-for-postgresql-and-mysql/how-to/create-a-database/>,
<https://www.scaleway.com/en/docs/managed-databases-for-postgresql-and-mysql/how-to/manage-read-replicas/>.
Its `encryption-at-rest`, `read-replicas` and
`snapshots` are the base offering's, and hold on this tier too.

- `node-high-availability`. **Needs:** the High Availability option. A hot standby node, kept current
  by synchronous replication, takes over when the main node fails: "If the main node fails for any
  reason, the standby can take over requests" (concepts *High availability*). **Scope: node level
  only.** The FAQ says: "In a high-availability cluster, active and hot standby nodes are indeed
  located in the same data center but in two separate racks." The pair survives the loss of a node, not
  the loss of the data centre or zone, so `zone-high-availability` is not claimed.
- Regions `fr-par`, `nl-ams`, `pl-waw`: the base offering's regions, for the same reasons; no page
  limits the High Availability option to some regions.

- **Prices** (*Managed Databases* price page, the same tables, read 2026-11-13): an HA Database Instance
  runs the main node and one standby, which the page's note counts as an additional node ("Additional
  node could be another node from the same type, a Read Replica or a HA node."). The pair costs the
  "Main node" price plus the "Additional nodes" price. The plan's price is **per node**: the pair's price
  × 730 ÷ 2, so that an estimate for the two nodes (count 2, ADR 0025) is the pair's price exactly. No
  half needed rounding. The price is right only when the inventory gives count 2 for the HA pair: an HA
  database recorded as one resource with count 1 is priced at half the pair, and its estimate is low.

| Plan | vCPU | Memory | Main node EUR/hour | Additional (HA) node EUR/hour | Pair EUR/hour | Pair × 730 | Per node: pair × 730 ÷ 2 | Price regions |
|---|---|---|---|---|---|---|---|---|
| `DB-PLAY2-PICO` | 1 | 2 GB | 0.0233 | 0.0203 | 0.0436 | 31.828 | `15.914` | all |
| `DB-PLAY2-NANO` | 2 | 4 GB | 0.0432 | 0.0376 | 0.0808 | 58.984 | `29.492` | all |
| `DB-PRO2-XXS` | 2 | 8 GB | 0.11 | 0.0583 | 0.1683 | 122.859 | `61.4295` | all |
| `DB-PRO2-XS` | 4 | 16 GB | 0.22 | 0.1166 | 0.3366 | 245.718 | `122.859` | all |
| `DB-PRO2-S` | 8 | 32 GB | 0.438 | 0.2321 | 0.6701 | 489.173 | `244.5865` | all |
| `DB-PRO2-M` | 16 | 64 GB | 0.876 | 0.4643 | 1.3403 | 978.419 | `489.2095` | all |
| `DB-PRO2-L` | 32 | 128 GB | 1.64 | 0.8692 | 2.5092 | 1831.716 | `915.858` | all |
| `DB-POP2-2C-8G` | 2 | 8 GB | 0.1434 | 0.076 | 0.2194 | 160.162 | `80.081` | all |
| `DB-POP2-4C-16G` | 4 | 16 GB | 0.2866 | 0.1519 | 0.4385 | 320.105 | `160.0525` | all |
| `DB-POP2-8C-32G` | 8 | 32 GB | 0.5684 | 0.3069 | 0.8753 | 638.969 | `319.4845` | all |
| `DB-POP2-16C-64G` | 16 | 64 GB | 1.1269 | 0.6198 | 1.7467 | 1275.091 | `637.5455` | all |
| `DB-POP2-32C-128G` | 32 | 128 GB | 2.1712 | 1.1073 | 3.2785 | 2393.305 | `1196.6525` | all |

- **Storage price:** `0.0993` per GiB a month, as for the base offering (€0.000136 per GB and hour × 730 =
  0.09928, rounded half up). It is the page's one published per-GB price and is **not** doubled for the
  standby's copy of the data: no page read says whether that volume is billed (*Prices*, *Database
  storage*).

### Managed Database for Redis

Category `cache.redis` (PRD #552 milestone 6, read 2026-11-26). Pages:
<https://www.scaleway.com/en/docs/managed-databases-for-redis/how-to/create-a-database-for-redis/>,
<https://www.scaleway.com/en/docs/managed-databases-for-redis/concepts/>,
<https://www.scaleway.com/en/docs/managed-databases-for-redis/faq/>,
<https://www.scaleway.com/en/docs/managed-databases-for-redis/reference-content/ensuring-data-persistence/>,
<https://www.scaleway.com/en/docs/ipam/reference-content/ipv6/>, all from `scaleway/docs-content` at
commit `13ef235ee5926d0a675e9b87cc421df5105762b1`; the changelog entry *Redis v8.4 is now supported*
(2026-02-19). The engine is Redis 8.4.

The create page offers three node configurations: "**Cluster mode**: creates the database as a Cluster
composed of three or more nodes", "**High Availability**: creates a secondary node with an up-to-date
replica" and "**Standalone**: creates a standalone database provisioned on a single node". No page names
a default, so the base offering is the lowest tier, **Standalone**, the tier that adds no capability
(tier-variant rule). Cluster mode adds `sharding` and is the variant *Managed Database for Redis
Cluster*. High Availability (2 nodes) is in public beta and gets no offering (*Left out on purpose*).

- `snapshots`. **Needs:** nothing beyond the base tier, Standalone. The persistence page: "Snapshots are
  available by default with a fixed configuration." and "a Redis™ snapshot is also automatically created
  within specified intervals". This follows the reading of OVHcloud's Valkey, which lists `snapshots`
  for its RDB persistence (PRD #552 milestone 5). **Caveat**, on the same page: "Scaleway Managed
  Database for Redis™ is currently best suited for caching use cases. The data persistence mechanism
  presented on this page do not guarantee data recovery in case of incident.", and the snapshots can be
  turned off. The maintainer may weigh it.
- No high availability: "a standalone database provisioned on a single node" (create page).
- No `encryption-at-rest`: no Redis page states it (the only encryption mentioned is TLS in transit,
  concepts page), so it is not listed (target rule). An ElastiCache, Memorystore or Azure cache that
  lists it is `partial` on Scaleway.
- No `ipv6`: the IPv6 page does not list Redis among the products that support it.
- Regions `fr-par`, `nl-ams`, `pl-waw` (both Redis offerings): the availability page (saved copy of
  <https://www.scaleway.com/en/product-availability-by-region/>, read 2026-11-26) marks Redis™
  "Available" in `fr-par-1`, `fr-par-2`, `nl-ams-1`, `nl-ams-2`, `pl-waw-1` and `pl-waw-2`, "Not
  available" in the third zones and `it-mil-2`/`it-mil-3`, and "Available soon" in `it-mil-1`, so Milan
  is not listed; its embedded data agrees. SDK `api/redis/v1` declares the same six zones (*API
  reference and SDK*), and the create page says "Managed Database for Redis™ is available in PAR1, PAR2,
  AMS1, AMS2, WAW1 and WAW2." The price data prices every node type in exactly those six zones.
- **Node count** (decided question 10 of PRD #552, issue #558): a Standalone database is exactly **1
  node** (create page). An estimate multiplies the plan by the resource's count, so a count above 1 is
  priced as that many Standalone databases.

- **Prices** (saved copy of <https://www.scaleway.com/en/pricing/managed-databases/>, table "Managed
  Database for Redis™", read 2026-11-26; its embedded data `managedRedisDatabase.nodeTypes`): seven RED1
  node types, every one `general_availability`. The table has two per-node columns, "Main node" and
  "Additional nodes", each an hourly SKU per zone; a Standalone database runs one node, the main node, so
  the plan's price is the "Main node" price × 730, per node. vCPU is the table's "vCPU" column (the data's
  `hardware.cpu.virtual.count`, every type `sharedCpu: true`), and memory its "GB", recorded as GiB (the
  data's `ram.size` is binary, RED1-MICRO 2147483648 bytes). No plan includes storage, and the offering
  has **no storage price**: the data shows local SSD storage, no billed volume, and the table has no
  storage column. The price of `fr-par-1`, `fr-par-2`, `nl-ams-1` and `pl-waw-1` (the first zones,
  *Prices*, *Which region, and zones*) applies in every region, so one price with an empty `regions`
  list. No value needed rounding. The node classes, by the FAQ's image text: "Development: RED1-S,
  RED1-XS, RED1-2XS, RED1-micro and Production: RED1-XL, RED1-L, RED1-M".

| Plan | vCPU | Memory | Main node EUR/hour | Additional node EUR/hour | EUR/month (main × 730) |
|---|---|---|---|---|---|
| `RED1-MICRO` | 2 | 2 GB | 0.048 | 0.027 | `35.04` |
| `RED1-2XS` | 3 | 4 GB | 0.075 | 0.041 | `54.75` |
| `RED1-XS` | 4 | 8 GB | 0.137 | 0.082 | `100.01` |
| `RED1-S` | 4 | 12 GB | 0.205 | 0.123 | `149.65` |
| `RED1-M` | 4 | 16 GB | 0.274 | 0.164 | `200.02` |
| `RED1-L` | 8 | 32 GB | 0.548 | 0.329 | `400.04` |
| `RED1-XL` | 16 | 64 GB | 1.096 | 0.658 | `800.08` |

- **Second-zone prices, not recorded.** The second zones of Amsterdam and Warsaw (`nl-ams-2`,
  `pl-waw-2`) charge slightly different prices for some types, for example RED1-MICRO main €0.0479 and
  additional €0.0274, RED1-2XS main €0.0753 and additional €0.0411, RED1-XS additional €0.0822, RED1-L
  main €0.5479 and additional €0.3288 an hour. Zones are not modelled and a region's price is its first
  zones' (*Prices*), so a cache placed in one of those zones may cost slightly more (or less) than the
  estimate. The table's visible prices read "From €… / HOUR" for that reason.

### Managed Database for Redis Cluster

Category `cache.redis`. A variant of *Managed Database for Redis* (tier-variant rule): a database created
in **Cluster mode**. Pages: as the base offering, and
<https://www.scaleway.com/en/docs/managed-databases-for-redis/how-to/scale-up-a-database-for-redis/>.
Its `snapshots` is the base offering's, and holds on this tier too (the persistence page names cluster
mode as an addition, not a replacement).

- `sharding`. **Needs:** Cluster mode. The FAQ, *What is the logic behind the cluster mode?*: "The
  cluster nodes use hash partitioning to split the keyspace into 16,384 key slots. Each source of the
  cluster is responsible for a subset of those slots."
- **No `node-high-availability`** (decided question 11 of PRD #552, and milestone 6's decision 1).
  Cluster mode does survive the loss of a node: the same FAQ section says "If one of the sources fails,
  the remaining nodes hold a vote and elect the replica that will be promoted as the new source.", and
  *How is the Redis™ Database's failover IP managed?* says "In case of unavailability of one of the
  cluster nodes, no downtime is to be expected." But a cluster needs a cluster-aware client. The FAQ,
  *How can I access my Managed Database for Redis™ once it is provisioned?*: "If you are in cluster mode
  (3 nodes or more), you have to use a cluster-specific connector cable to connect to multiple
  endpoints." and "There are as many `IP:port` endpoints as nodes in your cluster (one if standalone)."
  Decided question 11 asks the report to say so wherever it points a cache at this tier for its high
  availability; the snapshot format has no field for an offering's note, and no capability says "needs a
  cluster-aware client", so the decision's fallback applies: Scaleway Redis lists no
  `node-high-availability` until the 2-node High Availability topology leaves public beta (*Left out
  on purpose*). A cache that needs node-level high availability is therefore `partial` on both Redis
  offerings. If it were listed, a non-sharded cache would be pointed at this tier as `direct`, a tier its
  client may not be able to use.
- No `zone-high-availability`: the product is zonal ("Choose an Availability Zone", create page), and no
  page says a cluster spans zones.
- Regions `fr-par`, `nl-ams`, `pl-waw` (tier-variant regions rule): the base offering's regions. The
  create page offers Cluster mode with no regional restriction, and no page limits the tier to some
  regions or zones, the reading used for the PostgreSQL High Availability variant.
- **Node count** (decided question 10 of PRD #552, issue #558): **3 to 6 nodes**. The concepts page,
  *Cluster mode*: "minimum three and maximum six compute Instances"; the price page's table: "Databases
  for Redis™ are available in high availability, with clusters from 3 to 6 nodes."; the scaling page:
  "You can add up to 6 nodes.", and scaling only goes up ("The new total number of nodes has to be bigger
  than your current number of nodes."). The snapshot format cannot hold the bounds, so an inventory count
  below 3 is priced as given, below what Scaleway bills.

- **Prices** (saved copy of <https://www.scaleway.com/en/pricing/managed-databases/>, the same table and
  data, read 2026-11-26): the same seven node types. **Per-node price, an inference.** The page gives a
  "Main node" and an "Additional nodes" price per node type, and says nothing about how a cluster
  combines them. The snapshot reads a cluster as one main node plus additional nodes, by analogy with the
  PostgreSQL and MySQL table's note "Additional node could be another node from the same type, a Read
  Replica or a HA node." (which the Redis table does not carry). The plan's price is the 3-node minimum's
  price, (main + 2 × additional) × 730, divided by its 3 nodes, rounded half up to 4 fractional digits,
  so that an estimate for count 3 returns the 3-node cluster's price (to within the rounding: 2XS, XS, S
  and M differ by €0.0001 at count 3). A **4- to 6-node cluster** is then priced a little high: each node
  beyond the third at the per-node average instead of the additional-node price (RED1-M: €146.4867
  against €119.72 a month). vCPU, memory, storage and regions as the base offering; no storage price.

| Plan | vCPU | Memory | Main EUR/hour | Additional EUR/hour | 3 nodes EUR/hour (main + 2 × additional) | Per node EUR/month (3 nodes × 730 ÷ 3) |
|---|---|---|---|---|---|---|
| `RED1-MICRO` | 2 | 2 GB | 0.048 | 0.027 | 0.102 | `24.82` |
| `RED1-2XS` | 3 | 4 GB | 0.075 | 0.041 | 0.157 | `38.2033` (38.20333…) |
| `RED1-XS` | 4 | 8 GB | 0.137 | 0.082 | 0.301 | `73.2433` (73.24333…) |
| `RED1-S` | 4 | 12 GB | 0.205 | 0.123 | 0.451 | `109.7433` (109.74333…) |
| `RED1-M` | 4 | 16 GB | 0.274 | 0.164 | 0.602 | `146.4867` (146.48666…) |
| `RED1-L` | 8 | 32 GB | 0.548 | 0.329 | 1.206 | `293.46` |
| `RED1-XL` | 16 | 64 GB | 1.096 | 0.658 | 2.412 | `586.92` |

### Managed MongoDB

Category `database.mongodb` (PRD #552 milestone 6, read 2026-11-26). Pages:
<https://www.scaleway.com/en/docs/managed-mongodb-databases/how-to/create-a-database-instance/>,
<https://www.scaleway.com/en/docs/managed-mongodb-databases/concepts/>,
<https://www.scaleway.com/en/docs/managed-mongodb-databases/how-to/manage-snapshots/>,
<https://www.scaleway.com/en/docs/managed-mongodb-databases/how-to/increase-volume/>,
<https://www.scaleway.com/en/docs/ipam/reference-content/ipv6/>, all from `scaleway/docs-content` at
commit `13ef235ee5926d0a675e9b87cc421df5105762b1`; the changelog entries *Managed MongoDB is now in
General availability* (2025-06-10) and *MongoDB v8.0 is now supported* (2026-07-20). The concepts page's
"Currently, MongoDB® 7.0.11 and 7.0.12 are available" predates the 8.0 entry and is stale.

The create page offers two node configurations: "**Replica-set 1-node** - an instance of MongoDB® that
runs as a single server and does not provide redundancy or high availability." and "**Replica-set
3-nodes** - a group of 3 MongoDB® servers (1 primary and 2 standby nodes) that maintain the same data
set.", and the price page prices both ("Price for 1 Node", "Price for 3 Nodes"). No page names a
default, so the base offering is the lowest tier, the **1-node** replica set; the 3-node set adds
`node-high-availability` and is the variant *Managed MongoDB Replica Set*.

- `snapshots`. **Needs:** nothing beyond the base tier. The concepts page, *Database snapshot*: "A
  Snapshot is a consistent, instantaneous copy of the Block Storage volume of your Database Instance at a
  certain point in time." (also the *Manage snapshots* page).
- No `encryption-at-rest`: no MongoDB page mentions encryption, so it is not listed (target rule). A
  DocumentDB or Cosmos DB resource that lists it is `partial` on Scaleway.
- No `point-in-time-restore` and no `read-replicas`: neither is documented (the endpoints are
  read-write; no read-only node or replica endpoint is described).
- No `ipv6`: the IPv6 page does not list MongoDB, and the concepts page gives an endpoint "associated
  with an IPv4 address and a port".
- Regions `fr-par` only (both MongoDB offerings): the availability page (saved copy of
  <https://www.scaleway.com/en/product-availability-by-region/>, read 2026-11-26) marks MongoDB®
  "Available across the region" in Paris, "Not available" in Amsterdam and Warsaw and "Available soon"
  in Milan; its embedded data agrees. SDK `api/mongodb/v1` declares fr-par (*API reference and SDK*),
  and the price data prices only `fr-par`.
- **Node count and volume** (decided question 10 of PRD #552, issue #558): this tier is exactly **1
  node** (create page). The volume can grow "up to 15 TB" and "Block volume sizes cannot be decreased."
  (*Increase volume* page); no minimum volume is stated on the MongoDB pages, and the price data carries
  none.

- **Prices** (saved copy of <https://www.scaleway.com/en/pricing/managed-databases/>, tables "Managed
  MongoDB® - Cost Optimized" and "Managed MongoDB® - Production Optimized", read 2026-11-26; its embedded
  data `managedMongodb.ranges`): twelve node types, every one `general_availability`. A Database
  Instance costs a management fee per instance (`/storage/mgdb/instance/<type>/fr-par`) plus a price per
  node (`/storage/mgdb/node/<type>/<zone>`); the visible "Price for 1 Node" is the fee plus one node, in
  every row (POP2-16C-64G: 0.1675 + 0.962 = "€1.1295 / HOUR"). The plan's price is "Price for 1 Node" ×
  730. All three Paris zones agree (PLAY2-NANO is offered in `fr-par-1` and `fr-par-2` only), so one
  price with an empty `regions` list. vCPU and memory as the tables give them (memory binary in the
  data, recorded as GiB); Cost Optimized types are shared-CPU, Production Optimized are not. No plan
  includes storage.

| Plan | vCPU | Memory | Management EUR/hour | Node EUR/hour | 1 node EUR/hour | EUR/month (× 730) |
|---|---|---|---|---|---|---|
| `MGDB-PLAY2-NANO` | 2 | 4 GB | 0.0677 | 0.04 | 0.1077 | `78.621` |
| `MGDB-PRO2-XXS` | 2 | 8 GB | 0.0821 | 0.093 | 0.1751 | `127.823` |
| `MGDB-PRO2-XS` | 4 | 16 GB | 0.0976 | 0.207 | 0.3046 | `222.358` |
| `MGDB-PRO2-S` | 8 | 32 GB | 0.1554 | 0.409 | 0.5644 | `412.012` |
| `MGDB-PRO2-M` | 16 | 64 GB | 0.2712 | 0.811 | 1.0822 | `790.006` |
| `MGDB-PRO2-L` | 32 | 128 GB | 0.5043 | 1.608 | 2.1123 | `1541.979` |
| `MGDB-POP2-2C-8G` | 2 | 8 GB | 0.0699 | 0.111 | 0.1809 | `132.057` |
| `MGDB-POP2-4C-16G` | 4 | 16 GB | 0.07 | 0.247 | 0.317 | `231.41` |
| `MGDB-POP2-8C-32G` | 8 | 32 GB | 0.1025 | 0.485 | 0.5875 | `428.875` |
| `MGDB-POP2-16C-64G` | 16 | 64 GB | 0.1675 | 0.962 | 1.1295 | `824.535` |
| `MGDB-POP2-32C-128G` | 32 | 128 GB | 0.2985 | 1.908 | 2.2065 | `1610.745` |
| `MGDB-POP2-64C-256G` | 64 | 256 GB | 0.5595 | 3.807 | 4.3665 | `3187.545` |

- **Storage price:** the tables' "Block Storage 5K: €0.0993/GB/month", whose hourly price in the data is
  €0.000136 per GB in every Paris zone, × 730 = 0.09928, rounded half up to `0.0993` per GiB a month,
  the value and class rule of the PostgreSQL and MySQL offerings. Block Storage 15k (€0.1489) and
  "Backups/Snapshots: €0.03/GB/month" are not recorded (*Left out on purpose*). Each plan's data gives a
  block volume only, priced apart, so no plan includes storage and every GiB is priced.

### Managed MongoDB Replica Set

Category `database.mongodb`. A variant of *Managed MongoDB* (tier-variant rule): a Database Instance
created as a **Replica-set 3-nodes**. Pages:
<https://www.scaleway.com/en/docs/managed-mongodb-databases/how-to/create-a-database-instance/>,
<https://www.scaleway.com/en/docs/managed-mongodb-databases/concepts/>, and the base offering's other
pages. Its `snapshots` is the base offering's, and holds on this tier too.

- `node-high-availability`. **Needs:** Replica-set 3-nodes. The concepts page, *Replica-set 3-nodes*:
  "If the main node fails for any reason, one of the remaining standby nodes is assigned and can take
  over requests, reducing downtime." **Scope: node level only**: no page says the three nodes are in
  different zones, so `zone-high-availability` is not claimed. The standby nodes are failover targets,
  not read endpoints, so no `read-replicas`.
- Regions `fr-par` only: the base offering's single region (tier-variant regions rule: the price page
  prices the 3-node set there).
- **Node count** (decided question 10 of PRD #552, issue #558): exactly **3 nodes** (create page: "a
  group of 3 MongoDB® servers (1 primary and 2 standby nodes)"). A DocumentDB cluster of 2 or more
  instances, which lists `node-high-availability`, maps here; one with count 3 is priced at the set's
  price, and one with another count at that many thirds of it.

- **Prices** (the same page, tables and data, read 2026-11-26): the visible "Price for 3 Nodes" is the
  management fee plus three nodes in every row (POP2-16C-64G: 0.1675 + 3 × 0.962 = "€3.0535 / HOUR").
  The plan's price is **per node**: "Price for 3 Nodes" × 730 ÷ 3, rounded half up to 4 fractional
  digits, so that an estimate for count 3 is the set's price (as the PostgreSQL HA pair is divided by
  its two nodes). Regions, vCPU and memory as the base offering.

| Plan | vCPU | Memory | 3 nodes EUR/hour | Per node EUR/month (× 730 ÷ 3) |
|---|---|---|---|---|
| `MGDB-PLAY2-NANO` | 2 | 4 GB | 0.1877 | `45.6737` (45.67366…) |
| `MGDB-PRO2-XXS` | 2 | 8 GB | 0.3611 | `87.8677` (87.86766…) |
| `MGDB-PRO2-XS` | 4 | 16 GB | 0.7186 | `174.8593` (174.85933…) |
| `MGDB-PRO2-S` | 8 | 32 GB | 1.3824 | `336.384` |
| `MGDB-PRO2-M` | 16 | 64 GB | 2.7042 | `658.022` |
| `MGDB-PRO2-L` | 32 | 128 GB | 5.3283 | `1296.553` |
| `MGDB-POP2-2C-8G` | 2 | 8 GB | 0.4029 | `98.039` |
| `MGDB-POP2-4C-16G` | 4 | 16 GB | 0.811 | `197.3433` (197.34333…) |
| `MGDB-POP2-8C-32G` | 8 | 32 GB | 1.5575 | `378.9917` (378.99166…) |
| `MGDB-POP2-16C-64G` | 16 | 64 GB | 3.0535 | `743.0183` (743.01833…) |
| `MGDB-POP2-32C-128G` | 32 | 128 GB | 6.0225 | `1465.475` |
| `MGDB-POP2-64C-256G` | 64 | 256 GB | 11.9805 | `2915.255` |

- **Storage price:** `0.0993` per GiB a month, as for the base offering. The page gives one per-GB price
  per Database Instance with no "per node" wording, and no page says whether each of the three nodes'
  volumes is billed, so the price is **not** tripled; an estimate may be low by up to that much (as for
  PostgreSQL HA, *Prices*, *Database storage*).

### Object Storage

Category `storage.object`. Pages: <https://www.scaleway.com/en/docs/object-storage/concepts/>,
<https://www.scaleway.com/en/docs/object-storage/faq/>.

The base offering is the default storage class, **Standard Multi-AZ** (S3 class `STANDARD`). The page
*Object operations* (<https://www.scaleway.com/en/docs/object-storage/api-cli/object-operations/>)
says: "If no class is specified via the `--storage-class <class>` option, the object is created as
Standard (Multi-AZ) by default." Standard One Zone and Glacier only lack capabilities, so they get no
offering (tier-variant rule). Every capability below has **Needs:** nothing beyond the default class,
Standard Multi-AZ.

- `encryption-at-rest`: server-side encryption with SSE-C, SSE-ONE and SSE-KMS (FAQ *How can I encrypt
  my objects?*).
- `object-versioning`: bucket versioning (concepts *Versioning*; the 2026-05-20 changelog adds full
  versioning and Object Lock support to the console).
- No high availability: see *Left out on purpose*.
- Regions `fr-par`, `it-mil`, `nl-ams`, `pl-waw`: the availability page marks the Standard Multi-AZ and
  Standard One Zone classes "Available across the region" in all four regions. The concepts page lists
  an S3 endpoint in each of the four regions. Object Storage has no API in the SDK (it is S3-compatible).
- **Milan multi-AZ (PRD #11's open question), resolved: no split.** PRD #11 asked whether Scaleway
  should follow OVHcloud's rule, a separate offering where a capability holds only in some regions.
  The changelog entry *A full 3-AZ Region in Milan!* (2026-10-12) names the "Object Storage Standard
  Multi-AZ class" among the products in MIL-2 and MIL-3, and the availability page marks that class in
  all four regions. So the default class is the same in every region, and the offering needs no
  regional split. The concepts page and the quickstart still say Standard Multi-AZ "is available in the
  Paris, Amsterdam and Warsaw regions"; like the Instances FAQ and the Block Storage concepts page,
  they predate the Milan entry and are stale. Since Object Storage now lists no capability that
  depends on zones, the question would only matter again if a refresh found the default class missing
  from a region, and then the region rule applies.

- **Storage price** (*Storage* price page, *Object Storage*, read 2026-11-13): the Standard Multi-AZ class,
  the default (above), €0.000022/GB per hour, × 730 = 0.01606, rounded half up to `0.0161` per GiB a
  month, in every region; the page's data gives the same price for Paris, Milan, Amsterdam and Warsaw.
  The page's own "Approx. per month" is €0.01606/GB. Standard One Zone (€0.000011) and Glacier
  (€0.0000034) are not recorded: one storage price per region, and Standard Multi-AZ is the default
  class. Requests and ingress are "Included"; egress, "75GB free every month then €0.01/GB", is out of
  scope (PRD #190).

### Queues

Category `messaging.queue`. Pages: <https://www.scaleway.com/en/docs/queues/concepts/>,
<https://www.scaleway.com/en/docs/queues/faq/>.

- `dead-letter-queue`: dead-letter (undelivered-message) queues with a maximum receive count, available
  since 2025-04-01 (changelog).
- Regions `fr-par`, `nl-ams`: the availability page marks Queues available in Paris and Amsterdam, and
  "Not available" in Warsaw and Milan. SDK `api/mnq/v1beta1` `SqsAPI` serves the same two regions.

### Serverless Containers

Category `serverless.container` (PRD #552 milestone 6, read 2026-11-26). Pages:
<https://www.scaleway.com/en/docs/serverless-containers/concepts/>,
<https://www.scaleway.com/en/docs/serverless-containers/faq/>,
<https://www.scaleway.com/en/docs/ipam/reference-content/ipv6/>, all from `scaleway/docs-content` at
commit `13ef235ee5926d0a675e9b87cc421df5105762b1`; the changelog entry deprecating the `v1beta1` API
(2026-07-13; `v1` is current).

- Category fit: a container image Scaleway runs and scales with requests. The concepts page, *Scale to
  zero*: "When provisioned with a minimum scale of `0`, Serverless Containers scale down to zero active
  instances as long as they are not triggered."
- `autoscaling`. **Needs:** nothing (no tier). The concepts page, *Autoscaling*: "Autoscaling parameters
  are min-scale and max-scale". Listed as *Serverless Functions* lists it; the guide gives source
  resources of this category no capability, so it changes no fit today.
- No `ipv6`: as for *Serverless Functions* (*Left out on purpose*): the IPv6 page says "A function or
  container endpoint resolves to both IPv4 and IPv6 addresses (dual stack)", inbound traffic only.
- Regions `fr-par`, `it-mil`, `nl-ams`, `pl-waw`: the availability page (saved copy of
  <https://www.scaleway.com/en/product-availability-by-region/>, read 2026-11-26) marks Serverless
  Containers "Available across the region" in all four regions; its embedded data agrees. SDK
  `api/container/v1` declares the four (*API reference and SDK*), and the changelog entry of 2026-08-31
  brought the product to Milan (*Declared EU regions*).
- **Usage-priced**: the FAQ says it is "billed on a pay-as-you-go basis, strictly on resource
  consumption (Memory and CPU)", per GB-second and vCPU-second, so the offering has no plan, storage
  price or fixed fee, and a resource of the category is "not estimated: usage-priced" (PRD #190).

### Serverless Functions

Category `serverless.function`. Pages: <https://www.scaleway.com/en/docs/serverless-functions/faq/>,
<https://www.scaleway.com/en/docs/serverless-functions/reference-content/functions-autoscaling/>,
<https://www.scaleway.com/en/docs/ipam/reference-content/ipv6/>.

- `autoscaling`: functions scale between a minimum (down to zero) and maximum number of instances with
  demand.
- No `ipv6`: see *Left out on purpose*.
- Regions `fr-par`, `nl-ams`, `pl-waw`: the availability page marks Serverless Functions available in
  Paris, Amsterdam and Warsaw, and "Available soon" in Milan, so `it-mil` is not listed. SDK
  `api/function/v1beta1` serves the same three regions.

### Topics and Events

Category `messaging.topic` (PRD #552 milestone 6, read 2026-11-26). Pages:
<https://www.scaleway.com/en/docs/topics-and-events/faq/>,
<https://www.scaleway.com/en/docs/topics-and-events/concepts/>,
<https://www.scaleway.com/en/docs/topics-and-events/reference-content/topics-and-events-support/>,
<https://www.scaleway.com/en/docs/topics-and-events/reference-content/verifying-webhooks/>, all from
`scaleway/docs-content` at commit `13ef235ee5926d0a675e9b87cc421df5105762b1`; the changelog entry of
2024-06-25, which says "It is now in General Availability, ready for your production use cases."

- Category fit: publish and subscribe. The FAQ calls it "an in-house implementation of AWS SNS
  protocol", and the concepts page defines a *Subscriber* as "the entity (e.g. a queue, function, or
  URL) that messages from topics are pushed to".
- No capability. The SNS support page marks `RedrivePolicy` **N** (not supported) among the
  subscription attributes, and `DeliveryPolicy` N on topics and subscriptions, so there is no
  dead-letter setting and `dead-letter-queue` is not listed. It marks `KmsMasterKeyId` N on topics, and
  no page states encryption at rest, so `encryption-at-rest` is not listed either (target rule). Other
  limits the page records: `Subscribe` supports "Only http, sqs and lambda protocols", `FilterPolicy`
  and `RawMessageDelivery` are not supported, and FIFO topics are (`FifoTopic` Y).
- Regions `fr-par`, `nl-ams`: the availability page (saved copy of
  <https://www.scaleway.com/en/product-availability-by-region/>, read 2026-11-26) marks Topics & Events
  "Available across the region" in Paris and Amsterdam and "Not available" in Warsaw and Milan; its
  embedded data agrees. SDK `api/mnq/v1beta1` `SnsAPI` declares fr-par and nl-ams (*API reference and
  SDK*), and the webhook page gives trust chains for those two regions only.
- **Usage-priced**: the FAQ says "Billing is based on topic volume", the size of the messages a topic
  delivers, so the offering has no plan, storage price or fixed fee, and a resource of the category is
  "not estimated: usage-priced" (PRD #190).

## Left out on purpose

- **Instances `autoscaling`:** Autoscaling groups are in public beta. The changelog entry *Autoscaling
  Groups in Public Beta* (2026-08-25) opens them in fr-par, nl-ams and pl-waw, no later entry announces
  general availability, and the Autoscaling Groups API guide
  (<https://www.scaleway.com/en/docs/instances/api-cli/getting-started-autoscaling-groups/>) still
  requires "Beta access". Re-checked on 2026-11-13 for issue #187: `scaleway/docs-content` `main` at
  commit `6349594be902c7191c324538265827ea4f1ee1aa` still lists "Beta access to the Autoscaling Groups
  Private Beta" among the guide's requirements, so the capability stays out.
- **Block Storage `encryption-at-rest`:** the Key Manager encryption page
  (<https://www.scaleway.com/en/docs/block-storage/api-cli/encrypt-volumes-with-key-manager/>, validated
  2026-10-15) says volume encryption "is currently in Private Beta, and is only available via the Block
  Storage API".
- **Elastic Metal `snapshots`:** the FAQ says Elastic Metal servers must be backed up differently from
  Instances, which have the snapshot feature.
- **Managed Database `point-in-time-restore` and `ipv6`:** no point-in-time restore is documented (backups
  are restored as taken; snapshots and clones copy the current state), and the FAQ says IPv6 is not
  supported.
- **`ipv6` on Kubernetes Kapsule (both tiers), Object Storage, Queues, and both Redis and both MongoDB
  offerings:** the page *IPv6 and
  the Scaleway ecosystem* (<https://www.scaleway.com/en/docs/ipam/reference-content/ipv6/>) lists the
  products that support IPv6, and none of these is among them. It says that "Products other than those
  listed here do not officially support IPv6", and names Managed Databases and Kubernetes Kapsule among
  them.
- **Products not in the snapshot:** Kubernetes Kosmos (multi-cloud Kubernetes whose nodes may run
  outside Scaleway; Kapsule already serves the category); Serverless Jobs (batch jobs, no taxonomy
  category); NATS (streams, not queues or topics); Document Database (FerretDB on PostgreSQL, which has
  no product pages left, PRD #552; Managed MongoDB serves `database.mongodb`); File Storage (see below);
  Serverless SQL Database (see below). Serverless Containers, Topics and Events and Managed Database for
  Redis were in this list until PRD #552 milestone 6 gave them their categories (their sections above).
- **File Storage, so no `storage.file` offering (PRD #552 milestone 6, decision 4; flagged for the
  maintainer).** The category is "a shared file system the provider runs, mounted by several machines
  over NFS". Scaleway's File Storage is a shared file system, but it is not mounted over NFS: the FAQ
  (<https://www.scaleway.com/en/docs/file-storage/faq/>), *Is File Storage compatible with NFS?*, says
  "You do not need to manually configure or connect via the NFS protocol", the mount page
  (<https://www.scaleway.com/en/docs/file-storage/how-to/mount-file-system/>) mounts it with
  "`mount -t virtiofs -o nodev,nosuid <FILE_SYSTEM_ID> /mnt/my_fs`", and the FAQ says it is "designed to
  be used in conjunction with Scaleway Instances and Kubernetes Kapsule" (only compatible Instance types,
  <https://www.scaleway.com/en/docs/file-storage/reference-content/file-system-instance-selection/>). It
  does not fully serve the category, so it is left out (target rule), and a `storage.file` resource on
  Scaleway is fit `none`, "no offering in the Scaleway catalog for `storage.file`". The facts read, for a
  later decision (all `scaleway/docs-content` at commit `13ef235ee5926d0a675e9b87cc421df5105762b1`, the
  saved copy of <https://www.scaleway.com/en/pricing/storage/> and the saved copy of
  <https://www.scaleway.com/en/product-availability-by-region/>, read 2026-11-26):
  - general availability in Paris on 2026-07-16 and in Amsterdam on 2026-10-21 (changelog); the FAQ's
    "The AMS region will be available soon" predates the second entry;
  - regions: the availability page marks File Storage Standard "Available across the region" in Paris
    and Amsterdam, and the price data prices both, but SDK `api/file/v1alpha1`, the only File Storage
    package, declares fr-par only, so under the regions rule an offering would list `fr-par` alone;
  - price: the *File Storage* table's "File System | 25 GB up to 50 TB* | €0.000221 /GB / HOUR |
    €0.16133 /GB / MONTH", billed on "the provisioned size of the file system"; × 730 = 0.16133, rounded
    half up to `0.1613` per GiB a month, the same in both regions;
  - sizes: 25 GB minimum, 50 TB maximum per file system, in 1 GB steps (FAQ; resize page: "File system
    size must be at least 25 GB."), and at most 10 file systems per Organization;
  - capabilities: `encryption-at-rest` ("Scaleway automatically encrypts all data at rest on physical
    disks", FAQ); **no `snapshots`** (no File Storage page mentions them, so PRD #552's "snapshots" is not
    confirmed); no `zone-high-availability` ("triple replication" "within one Availability Zone", FAQ).

  A decision to accept virtiofs mounts by Scaleway's own machines as `storage.file` would add one
  offering, *File Storage*, category `storage.file`, capability `encryption-at-rest`, region `fr-par`
  (and `nl-ams` once its SDK declares it), no plans and a storage price of `0.1613` per GiB a month,
  with the 25 GB minimum recorded here (decided question 10).
- **Redis High Availability (2 nodes):** in public beta. The changelog entry of 2024-12-03 says "Our
  2-node High Availability offer is now in Public Beta.", and no later Redis entry announces general
  availability (the create page and FAQ describe the option without a beta label). Under decided
  question 11 of PRD #552 it is no offering or tier, and Scaleway Redis lists no
  `node-high-availability` until it is GA (*Managed Database for Redis Cluster*).
- **Redis `node-high-availability` on Cluster mode:** stated (the cluster survives a node's loss), but
  not listed, because a cluster needs a cluster-aware client and the snapshot cannot say so (*Managed
  Database for Redis Cluster*; decided question 11's fallback).
- **`encryption-at-rest` on Redis, MongoDB, Topics and Events and Container Registry:** no page of any
  of them states it (Redis names TLS in transit only; Topics and Events marks `KmsMasterKeyId` not
  supported). Their sections say so. Every source registry lists it, so every registry resource is
  `partial` on Scaleway.
- **Topics and Events `dead-letter-queue`:** `RedrivePolicy` is not supported (*Topics and Events*).
- **Load Balancer `node-high-availability`:** a failed load balancer is replaced by a replica
  "immediately spawned and deployed", with its IP rerouted automatically (FAQ). That is a rebuild after
  the failure, recovery rather than continuity, as for Object Storage below (milestone 6's decision 3).
  **Load Balancer `udp`:** not supported (FAQ).
- **Milan for Load Balancer and Container Registry:** the availability page marks both available there,
  but their SDKs do not declare `it-mil` (regions rule; each offering's section).
- **Serverless SQL Database, removed from the snapshot (PRD #14, target rule).** It is a
  PostgreSQL-compatible database with documented differences from standard PostgreSQL: the
  known-differences page
  (<https://www.scaleway.com/en/docs/serverless-sql-databases/reference-content/known-differences/>)
  lists, because of connection pooling and scaling to zero, no temporary tables or views, no
  subscriptions, no prepared transactions, and no guarantee for `LISTEN`/`NOTIFY`, advisory locks or
  session settings, and it points to Managed Database for PostgreSQL where strict compatibility is
  needed. So it does not fully serve `database.postgresql`, and a PostgreSQL workload is pointed at
  Managed Database instead. It returns once a report can say "compatible, with differences".
- **Serverless Functions `ipv6` (PRD #14, target rule).** `ipv6` means IPv6 connectivity in both
  directions (ADR 0011). The page *IPv6 and the Scaleway ecosystem*
  (<https://www.scaleway.com/en/docs/ipam/reference-content/ipv6/>), section *Serverless Functions and
  Containers and IPv6*, says only that "A function or container endpoint resolves to both IPv4 and IPv6
  addresses (dual stack)": inbound traffic only. Nothing documents IPv6 for traffic a function sends.
- **Object Storage `node-high-availability` and `zone-high-availability` (PRD #14, target rule).** The
  FAQ (<https://www.scaleway.com/en/docs/object-storage/faq/>) says Standard Multi-AZ distributes data
  "across three different Availability Zones (AZ). In the event of a complete data center failure in an
  AZ, we are still able to rebuild your data and make it accessible". Rebuilding the data and making it
  accessible again is recovery, not continuity without manual failover, so neither level is stated in
  full. Node level is not claimed either: the FAQ describes Standard One Zone's three racks as where
  data is stored, not as continuity of service.
- **Kubernetes Kapsule Dedicated `zone-high-availability`:** the control plane is reached through a Load
  Balancer in the region's primary zone and is unreachable if that zone fails, "This limitation also
  applies to HA Dedicated control planes" (Multi-AZ clusters page,
  <https://www.scaleway.com/en/docs/kubernetes/reference-content/multi-az-clusters/>). See that
  offering's section.
- **Managed Database `zone-high-availability`:** the High Availability option keeps both nodes in one
  data centre (FAQ). A Read Replica can be "Multi-AZ", hosted in a different zone (page *Manage Read
  Replicas*), but promoting it is a manual, irreversible step ("It is possible to promote a Read Replica
  node to a standalone Database Instance"), so it is not failover without manual action, and neither
  tier lists zone level.

Prices and plans left out (PRD #190 milestone 4, read 2026-11-13):

- **The Dedicated-8 and Dedicated-16 control planes of Kubernetes Kapsule Dedicated** (*Containers*
  price page): €0.18 and €0.35 an hour, × 730 = €131.40 and €255.50 a month. An offering has one fixed
  fee per region (ADR 0026), and the snapshot records the smallest tier, Dedicated-4 (`80.30`), so an
  estimate for a cluster on a larger tier is low by the difference (*Kubernetes Kapsule Dedicated*).
  The page prices Dedicated-4 and Dedicated-8 in `it-mil` too, and says Dedicated-16 is "Not available
  in this region" there; that statement added `it-mil` to the offering on 2026-11-19 (issue #211,
  *Kubernetes Kapsule Dedicated*, Milan).
- **The `fr-par-3` zone prices** of PRO2 and POP2 Instances and Kubernetes nodes, about 1.5 times the
  other zones' (*Instances*): zones are not modelled, and a region has one price per plan (*Prices*).
- **Instance and node ranges not recorded:** BASIC2, BASIC3, STANDARD2 and STANDARD3 (General Purpose,
  priced in Paris, Amsterdam and Milan only), GP1 and the DEV1 and STARDUST1 Development types (earlier
  generations with local storage), the Specialized ranges (Memory, Compute and Network Optimized), GPU
  Instances, and the `-WIN` Windows POP2 rows. They are priced on the page; the snapshot keeps a smaller,
  fully cited general-purpose set (*Prices*, *Plans chosen*).
- **Elastic Metal servers not recorded:** every server other than the eight under *Elastic Metal*. Some
  have different hourly prices in different zones (`EM-A315X-SSD`: €0.1529 in Paris, €0.153 in Amsterdam
  and Warsaw), others are offered in only one or two regions. Elastic Metal monthly billing, with its
  one-month commitment fee, is a commitment price, not a list price (*Prices*).
- **Database node types not recorded:** DB-DEV-S/M/L/XL and DB-GP-XS/S/M/L/XL (the earlier generation,
  with local storage), and RDB-DEDICATED3, priced in `it-mil` only, a region the database offerings do
  not list (*Managed Database for PostgreSQL and MySQL*, Milan). The "Multi-AZ option", for Read Replicas
  only, and backup and snapshot storage ("Backups/Snapshots: €0.03/GB/month") are not recorded either.
- **Database storage for the HA standby:** not added; see *Prices*, *Database storage*.
- **Block Storage snapshots** (€0.000049/GB/hour) and **Block Storage 15K** (*Block Storage*): one
  storage price per region, the 5K class.
- **Instance storage:** no plan of *Instances*, *Kubernetes Kapsule* or *Kubernetes Kapsule Dedicated*
  includes storage. Their root and data volumes are *Block Storage* volumes billed separately, so each
  of those offerings records the *Block Storage* 5K price as its storage price (`0.0949`). Block Storage
  15K, and the higher price of a node volume placed on it, are left out, as for *Block Storage*.
- **Elastic Metal storage beyond a server's disks** (issue #219, read 2026-11-14): not priced, because
  Elastic Metal is not compatible with Block Storage and sells no other storage option (*Elastic Metal*,
  *No storage price*).
- **Usage-priced offerings:** Queues, Serverless Functions and, since PRD #552 milestone 6, Serverless
  Containers and Topics and Events are priced by use (PRD #190) and have no plans or storage prices.
- **Unpriced offerings** (PRD #552 milestone 6, decided question 6): Load Balancer (sizes LB-S to LB-XL)
  and Container Registry (€0.027 per GB and month of private images) record no plan, storage price or
  fixed fee; a resource of either category is not estimated, reason `not-priced`.

Prices and plans left out (PRD #552 milestone 6, read 2026-11-26):

- **The Redis prices of `nl-ams-2` and `pl-waw-2`**, which differ slightly from the first zones'
  (*Managed Database for Redis*): one price per region, its first zones'.
- **MongoDB Block Storage 15k** (€0.000204 per GB and hour, €0.1489 a month) and **MongoDB backups and
  snapshots** (€0.03 per GB and month; €0.000044 an hour in the data): one storage price per region, the
  5K class, as for the PostgreSQL and MySQL offerings.
- **MongoDB storage on the 3-node set's other nodes:** not added; see *Managed MongoDB Replica Set*.
- **Redis Cluster nodes beyond the third:** priced at the per-node average of the 3-node cluster, not at
  the additional-node price (*Managed Database for Redis Cluster*).
