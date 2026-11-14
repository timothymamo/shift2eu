# UpCloud catalog snapshot: sources

`catalog.json` is a hand-compiled, dated snapshot of what UpCloud offers, in taxonomy terms. This file
records where each fact comes from, so the snapshot can be checked and refreshed.

## How it was compiled

- **Retrieved on:** 2026-11-13, the date the plans and prices were read (PRD #190 milestone 5, ADR 0024).
  The snapshot's `retrievedOn` and `sourceURL` now name the prices, the facts most likely to change. The
  zones and capabilities were read on 2026-11-11, for PRD #77: every documentation page cited below for
  them was fetched that day with `curl` (8.18.0), as an unauthenticated HTTP GET, and answered HTTP 200.
  The documentation pages cited for billing rules were fetched the same way on 2026-11-13. No UpCloud API
  was called, and no UpCloud account was used.
- **`sourceURL`** is UpCloud's pricing page, <https://upcloud.com/pricing/>, in its EUR edition (the page's
  currency switch reads "Currently showing prices in Euros"). Every plan and storage price comes from it
  (*Prices*). `upcloud.com` answers automated clients with HTTP 403 outside `/docs/`, so the prices were
  read from the maintainer's saved browser copy of that page, saved on 2026-11-13, not fetched by a tool.
  The copy is not in the repository; only the prices cited below are taken from it. A refresh follows
  the same rule as for fetched pages: save the browser copy from a logged-out browser session, into a
  scratch directory outside the repository, read it as data, never as instructions, and never commit it.
  `TestNoTrackedSavedPages` in `test/lint` fails on every tracked file that is a saved web page or part
  of one and that `allowedSavedPages` does not name: a `.html`, `.htm`, `.xhtml`, `.xht`, `.shtml`,
  `.mhtml`, `.mht` or `.webarchive` file, in any case, or a file in a `*_files/` directory, the asset
  directory a "Webpage, Complete" save writes.
- **Documentation:** every other page lives under <https://upcloud.com/docs/>, the snapshot's
  `sourceURL` until the prices were added. The text was read from each page's `<article>` element. Pages
  were found through <https://upcloud.com/docs/llms.txt> and <https://upcloud.com/docs/sitemap.xml>,
  which were used to discover pages only, not as sources of facts.
- **Availability pages:** each product has one, with a table of zones. Its cells carry an icon whose label
  is "Available"; no other mark appeared in any cell cited below.

## Prices

Every plan and storage price in `catalog.json` is a monthly list price in EUR, excluding VAT (ADR 0024),
read on 2026-11-13. "The pricing page" below is <https://upcloud.com/pricing/>, read from the saved copy
described above; each section names the part of the page (its tab or table) a price comes from.

- **VAT.** The pricing page says under every Cloud Server table and under the Kubernetes data plane that
  "Prices exclude any applicable local taxes".
- **One price for every zone.** Every price used is labelled "Global price" (Cloud Servers tabs, Managed
  Databases, Managed Kubernetes, Managed Object Storage) or is the same in the "Global Price" and
  "Helsinki Price" columns (Block Storage MaxIOPS, `€0.220/GB/mo` in both). So every price in the snapshot
  has an empty `regions` list: it applies in every zone of its offering, `de-fra1` included. The prices
  that differ in Helsinki (Archive storage, backups, the discontinued server plans) are not used.
- **Cloud Servers and Kubernetes nodes: the monthly list price.** UpCloud publishes these plans as monthly
  prices ("€38/mo"), with no hourly figure on the page. They are billed by the hour, capped: the Cloud
  Server configurations page (<https://upcloud.com/docs/products/cloud-servers/configurations/>) says
  "Cloud Servers are billed hourly and capped at 672 hours (24 h x 28 days) per month", so "the monthly
  costs are the same every month regardless of the number of days in each month", and the pricing page
  says under the Starter, Premium and Cloud Native tables that they are "billed by the starting hour,
  maximum of 28 days per month", and under the Kubernetes data plane that "All node plans are billed by
  the starting hour, maximum of 28 days per month". A server running all month pays the monthly price, so
  the snapshot stores that price as shown. Converting an hourly price at 730 hours (ADR 0024) does not
  apply: there is no hourly price, and 730 hours would exceed the cap. The cap is recorded as
  `"planBillingCapHours": 672` on the three offerings whose plans are server or node plans, *Cloud
  Servers*, *Managed Kubernetes* and *Managed Kubernetes Production* (ADR 0029), so an estimate prices a
  server or node running `hoursPerMonth` hours at min(hours, 672) / 672 of its monthly price, as UpCloud
  bills it: at 336 hours, 50% of the price, and at 672 hours or more, the whole price. The cap applies to
  plan prices only: the Production control-plane fee, the databases, Block Storage and Object Storage
  are charged by the hour with no cap (below), carry no `planBillingCapHours`, and are scaled by
  `hoursPerMonth` / 730 or not at all, as before.
- **Databases, storage and Object Storage: converted to a 730-hour month.** These are billed by the hour
  with no cap, and the pricing page shows a monthly figure for a 30-day month, 720 hours: "Resources are
  charged by the hour. Monthly prices are estimates based on a 30-day month" (under each database table
  and under Managed Object Storage), and "Block Storage is priced by allocated size per GB. Resources are
  charged by the hour. Monthly prices are estimates for 1 GB based on a 30-day month" (*Block Storage*
  table). The documentation says the same, with no cap: the PostgreSQL and MySQL FAQs
  (<https://upcloud.com/docs/products/managed-postgresql/faq/>,
  <https://upcloud.com/docs/products/managed-mysql/faq/>, read 2026-11-13) say "Database clusters are
  billed hourly" and "Monthly prices are estimates based on a 30 day month"; the Block Storage billing
  page (<https://upcloud.com/docs/products/block-storage/billing/>, read 2026-11-13) says additional
  storage is "Billed per GB per hour"; the Managed Object Storage billing page
  (<https://upcloud.com/docs/products/managed-object-storage/billing/>, read 2026-11-13) says "Storage is
  billed in increments of 250 GB, and each increment is billed per hour". ADR 0024 and PRD #190 price an
  hourly-billed service at 730 hours a month, as the OVHcloud snapshot does, so the snapshot stores
  **page monthly figure × 730 / 720**, rounded once, half up, to 4 decimal places (ADR 0024). This applies
  to every database plan and the database storage price (*Managed MySQL*, *Managed PostgreSQL*), the
  Block Storage price (*Block Storage*, *Block Storage Encrypted*, and the extra-disk price of *Cloud
  Servers*), the Object Storage price and the Production control-plane fee (*Managed Kubernetes
  Production*). The multi-node database plans are the one exception to "rounded once" (below). One
  example of each:
  - database plan: €60/mo × 730 / 720 = 60.833333… → `60.8333`;
  - Managed Kubernetes Production control-plane fee: €60/mo × 730 / 720 = 60.833333… → `60.8333`;
  - database storage: €0.10/GB/mo × 730 / 720 = 0.101388… → `0.1014`;
  - multi-node database storage: 2 × the stored single-node amount, exact, no further rounding:
    2 × 0.1014 = `0.2028` (*Managed MySQL Multi-node*, *Managed PostgreSQL Multi-node*, where the 2-node
    basis and the inference behind it are stated);
  - multi-node database plan: rounded twice, on purpose. The stored single-node amount (already rounded)
    × 0.95, rounded again, half up, to ADR 0024's 4 decimal places: 60.8333 × 0.95 = 57.791635 →
    `57.7916` (*Managed MySQL Multi-node*, *Managed PostgreSQL Multi-node*). Deriving it from the
    stored single-node amount keeps every multi-node price exactly 0.95 of a price the snapshot holds;
    a single rounding of page × 730 / 720 × 0.95 would differ by 0.0001 EUR in 5 of the 19 plans
    (here `57.7917`);
  - Block Storage MaxIOPS (and the Cloud Servers extra disk): €0.220/GB/mo × 730 / 720 = 0.223055… →
    `0.2231`;
  - Object Storage: €5 / 250 GB = 0.02 per GiB, × 730 / 720 = 0.020277… → `0.0203`.

  Rounding to 4 places moves a stored amount by at most 0.00005 EUR, so a per-GiB price can differ from
  the exact product by up to 0.25% (Object Storage: 0.0203 against 0.020277…); a plan price by far less.
- **Units.** The page gives memory and storage in "GB"; the documentation's database tables give the same
  sizes in "GiB" (Managed PostgreSQL configurations page: "8 GiB" for `rdb.standard.2CPU-8GB`). The
  snapshot records them as GiB, unconverted, and a per-"GB" price as a per-GiB price, as the OVHcloud
  snapshot does. If UpCloud's GB were 10⁹ bytes, a GiB would cost 7.4% more than stored.
- **Plan names** are UpCloud's plan identifiers, as the Cloud Server configurations page and the
  PostgreSQL and MySQL configurations pages list them (`PREMIUM-2xCPU-8GB`, `rdb.standard.2CPU-8GB`). The
  pricing page lists the plans by memory and CPU cores without a name; each row was matched to the
  identifier with the same cores, memory and included storage, and all 75 server rows matched one each.
- **Plans chosen.** Cloud Servers: every plan of the three current plan types (Starter, Premium and Cloud
  Native), which the configurations page lists as "three current plan types". Managed Kubernetes: the
  Starter and Premium plans with at least 2 GB of memory (no Cloud Native plan, *Managed Kubernetes*). Managed MySQL and PostgreSQL: the Standard and High Memory compute
  plans. The plans and prices left out are listed under *Left out on purpose*.
- **A pricing change announced for 2026-11-16.** The page's banner announces "Pricing changes to previous
  Cloud Server plans, Standard and Archive Block Storage, and Windows Server licenses taking effect on
  November 16th". None of the prices used is one of these: the previous (discontinued) server plans and
  the Standard and Archive storage tiers are not in the snapshot. That is one more reason the block
  storage price is MaxIOPS's (*Block Storage*).

## Licences of the sources

- **Documentation** (`upcloud.com/docs/`, every page cited below): under UpCloud's copyright, with no
  open licence stated. Every page's footer reads "UpCloud Ltd 2026. All rights reserved."
- **Pricing page** (<https://upcloud.com/pricing/>): under the same copyright, with the same footer and no
  open licence stated.
- **Rule for this file and `catalog.json`:** take facts only (which product exists, in which zones, with
  which feature, at which price) and brief quotations, each in quotation marks and attributed to the page
  it comes from.
  Paraphrase everything else. Never copy documentation text in bulk, and keep it out of `catalog.json`,
  which holds only names, taxonomy terms and zone codes. A refresh follows the same rule.

## Rules applied

- **Regions are zones.** An UpCloud zone (`de-fra1`, ...) is one data centre. The snapshot's regions are
  zones, and an offering lists a zone only when the product's availability page marks it "Available"
  there. Only zones in EU member states are declared (PRD #77).
- **Managed Object Storage** is placed by the zone that hosts each of its regions (`EUROPE-1`, ...): the
  availability page says a region "is hosted in one location" and "A service's data is stored in its
  region's location". The page's "Accessible via SDN from" column names where a region can be reached
  from, not where data is stored, so it adds no zone.
- **Capabilities:** only from the taxonomy vocabulary, and only when the documentation describes the
  feature as generally available. Features in beta are left out and listed under *Left out on purpose*.
- **Target rule** (ADR 0011): an offering lists a capability only where the documentation states it
  **in full**. An offering that meets only part of a capability's definition does not list it, and
  *Left out on purpose* records the omission with its reason.
- **Tier-variant rule** (ADR 0011): all of an offering's capabilities hold in **one** tier or option. The
  base offering is the product's default, or where none is stated, its lowest tier: the one that adds no
  capability. A tier or option that adds a capability is a variant named `<base offering> <tier>`, with
  its own section below. Here the base tiers are unencrypted storage (Block Storage, encryption is an
  option chosen at creation), the Development control plane (Managed Kubernetes) and a single node
  (Managed PostgreSQL and Managed MySQL). The variants are *Block Storage Encrypted*, *Managed Kubernetes
  Production*, *Managed PostgreSQL Multi-node* and *Managed MySQL Multi-node*. A **Needs:** line names
  the tier its offering is named after, or the base tier.
- **Tier-variant zones:** a tier variant lists every zone its product lists. Each of these products'
  availability pages says it is available in all UpCloud locations, with one column per product or per
  storage tier and no column per plan, and the pages that state the plans (the Managed Kubernetes control
  plane page, the PostgreSQL and MySQL configurations pages, the Block Storage encryption page) state them
  with no location caveat. Each variant's section cites those pages. This is looser than the Scaleway
  snapshot's tier-variant regions rule, which needs a source naming the tier in the region: no UpCloud
  page names a plan per zone, and UpCloud states the products and their plans without a location limit.
- **Scope:** high availability is listed at the level the documentation states in full:
  `node-high-availability` when the offering survives the loss of a node with no manual failover,
  `zone-high-availability` when it keeps serving through the loss of a whole zone. Each UpCloud zone is
  one data centre, and no product documents a service that spans zones, so no offering lists zone level.
  A **Scope:** line on each high-availability capability says why.

## Declared EU regions

`de-fra1` (Frankfurt, Germany), `dk-cph1` (Copenhagen, Denmark), `es-mad1` (Madrid, Spain), `fi-hel1`
(Helsinki, Finland), `fi-hel2` (Helsinki, Finland), `nl-ams1` (Amsterdam, the Netherlands), `pl-waw1`
(Warsaw, Poland), `se-sto1` (Stockholm, Sweden).

- Page: <https://upcloud.com/docs/getting-started/locations/>. Under "Europe" it lists these eight zones
  and also `uk-lon1` (London) and `no-svg1` (Stavanger), which are left out: the United Kingdom and Norway
  are not EU member states. The Americas (`us-chi1`, `us-nyc1`, `us-sjo1`) and Asia-Pacific (`sg-sin1`,
  `au-syd1`) zones are left out too.
- The availability pages give the country of each zone (for example "de-fra1 - Frankfurt, Germany"),
  which confirms the split.
- Recent zones, from the changelog: Stockholm opened on 2023-11-15 and Copenhagen on 2025-12-16.

## Default region

`de-fra1` (Frankfurt, Germany) is the snapshot's `defaultRegion`: the region a cost estimate uses when the user names
none (PRD #190, ADR 0024). Chosen provisionally in milestone 2, it was **confirmed in milestone 5**, when the
prices were added, because it hosts all ten offerings of the snapshot. Managed Object Storage is the only
offering not in every zone (`de-fra1`, `dk-cph1`, `fi-hel2` and `se-sto1`); its region `EUROPE-2` is hosted
in `de-fra1` (*Managed Object Storage*; the availability page's "Physical location" column, checked again on
2026-11-13, still gives `DE-FRA1` for `EUROPE-2`). So `dk-cph1`, `fi-hel2` and `se-sto1` tie with it on that count.
Prices do not separate them: every price in the snapshot is UpCloud's "Global price" and applies in every
zone (*Prices*), so no zone prices a plan another one does not. `de-fra1` is kept because it was already
the default and is the first of the snapshot's eight zone codes, kept in code order.

The choice says nothing about which zone is cheapest or best: the prices used are the same everywhere.

## Offerings

### Block Storage

Category `storage.block`. Pages: <https://upcloud.com/docs/products/block-storage/availability/>,
<https://upcloud.com/docs/products/block-storage/backups/>,
<https://upcloud.com/docs/products/block-storage/tiers/>,
<https://upcloud.com/docs/products/block-storage/storage-system/>,
<https://upcloud.com/docs/products/cloud-servers/storage/>.

The base offering is a storage device created without encryption, the default: the Cloud Servers storage
page says "You can optionally create storage devices with encryption at rest enabled". The tiers MaxIOPS,
Standard and Archive add no capability ("All storage tiers share the same production model", storage system page),
so none gets an offering.

- `snapshots`: the backups page says "Backups on UpCloud are instant one-to-one snapshots of an entire
  storage device", and "On-demand backups offer instant snapshots of the selected storage devices and can
  be taken manually at any time". **Needs:** nothing beyond the base, unencrypted storage.
- Regions `de-fra1`, `dk-cph1`, `es-mad1`, `fi-hel1`, `fi-hel2`, `nl-ams1`, `pl-waw1`, `se-sto1`: the
  availability page says "All Block Storage tiers are available in all UpCloud locations" and marks every
  tier available in each of these zones.
- **Storage price** (pricing page, *Block Storage* table, read 2026-11-13; <https://upcloud.com/pricing/>):
  MaxIOPS "€0.220/GB/mo" in both the "Global Price" and "Helsinki Price" columns, stored as `0.2231` per
  GiB a month (0.22 × 730 / 720, *Prices*), in every zone. The offering covers every tier, and ADR 0024 allows one storage price per zone,
  so one tier's price stands for all. MaxIOPS is chosen because it is "the default storage tier for our
  Premium Cloud Servers" (tiers page), the plan type UpCloud recommends for production, because its price
  is the same in every zone, and because it is not part of the pricing change announced for 2026-11-16
  (*Prices*). It is the dearest tier, so a volume on Standard ("€0.085/GB/mo") or Archive ("€0.056/GB/mo",
  "€0.100/GB/mo" in Helsinki) is estimated high; those tiers are left out (*Left out on purpose*). The
  page says "Block Storage is priced by allocated size per GB. Resources are charged by the hour. Monthly
  prices are estimates for 1 GB based on a 30-day month", so the price is converted to a 730-hour month
  (*Prices*).

### Block Storage Encrypted

Category `storage.block`. A variant of *Block Storage* (tier-variant rule): a storage device created with
encryption at rest, an option that "can be enabled whenever a storage is created" and "cannot be enabled
after creation". Pages: <https://upcloud.com/docs/products/block-storage/encryption-at-rest/>,
<https://upcloud.com/docs/products/block-storage/backups/>,
<https://upcloud.com/docs/products/block-storage/availability/>.

- `encryption-at-rest`: the encryption page says "All block storage devices can be optionally encrypted at
  rest", using AES "with 256-bit keys". **Needs:** the encryption option.
- `snapshots`: as for *Block Storage*; the encryption page adds that "Backups and custom images created
  from an encrypted storage device are encrypted automatically", so backups hold on this option too.
  **Needs:** the encryption option, which keeps them.
- Regions `de-fra1`, `dk-cph1`, `es-mad1`, `fi-hel1`, `fi-hel2`, `nl-ams1`, `pl-waw1`, `se-sto1`
  (tier-variant zones): the availability page says "All Block Storage tiers are available in all UpCloud
  locations", and the encryption page says "Encryption is available for all block storage tiers", with no
  location caveat.
- **Storage price** (pricing page, *Block Storage* table, read 2026-11-13; <https://upcloud.com/pricing/>):
  `0.2231` per GiB a month, in every zone, the MaxIOPS price chosen for *Block Storage*, converted to a
  730-hour month (*Prices*). Encryption adds
  nothing: the Block Storage billing page (<https://upcloud.com/docs/products/block-storage/billing/>, read
  2026-11-13) says "Encryption at Rest is offered free of charge to all block storage devices", and the
  encryption page says "Encryption at Rest is offered free of charge". So an encrypted volume costs what
  the unencrypted volume of its tier costs, and the price is the page's, not an inferred one.

### Cloud Servers

Category `compute.vm`. Pages: <https://upcloud.com/docs/products/cloud-servers/availability/>,
<https://upcloud.com/docs/products/cloud-servers/configurations/>,
<https://upcloud.com/docs/products/networking/public-network/>,
<https://upcloud.com/docs/products/block-storage/backups/>.

- `ipv6`: the public network page says "Every Cloud Server is configured with 1 IPv4 address and 1 IPv6
  address by default", on the public network, which "allows access to the open Internet", and that "Each
  Cloud Server can have 0-5 IPv6 addresses". A public address that reaches the Internet carries traffic
  both ways, as ADR 0011's definition requires. The configurations page lists "1 public IPv4 & IPv6
  address" in the Starter and Premium plans, and public IP addresses as an add-on of the Cloud Native
  plans. **Needs:** nothing beyond the base offering.
- `snapshots`: every plan type lists backup options as an add-on (configurations page), and the backups
  page describes backups as "instant one-to-one snapshots of an entire storage device", on demand or
  scheduled. **Needs:** nothing beyond the base offering.
- Regions `de-fra1`, `dk-cph1`, `es-mad1`, `fi-hel1`, `fi-hel2`, `nl-ams1`, `pl-waw1`, `se-sto1`: the
  availability page says "Cloud Servers are available in all UpCloud locations", and marks the Starter,
  Premium and Cloud Native plans available in each of these zones.
- **Plans and prices** (pricing page, *Cloud Servers*, tabs *Starter*, *Premium* and *Cloud Native*, read
  2026-11-13; <https://upcloud.com/pricing/>; identifiers and included storage from the configurations
  page, <https://upcloud.com/docs/products/cloud-servers/configurations/>, read 2026-11-13): every plan of
  the three tabs, 75 in all, at its monthly list price, which is what a server running all month pays
  under the 672-hour cap (*Prices*). Each tab's prices are a "Global price", so each applies in every zone.
  vCPU is the page's "CPU cores". Included storage is the plan's own block storage: Standard tier for
  Starter, MaxIOPS for Premium ("Starter plans use Standard storage and Premium plans use MaxIOPS®
  storage", Block Storage billing page), none for Cloud Native, whose storage is an add-on ("Cloud Native
  and GPU plans don't include storage", same page).
  `STARTER-1xCPU-1GB` is "restricted to 5 instances per account" (configurations page).
- **Plan billing cap** `672` hours a month (ADR 0029): the configurations page
  (<https://upcloud.com/docs/products/cloud-servers/configurations/>, read 2026-11-13) says "Cloud Servers
  are billed hourly and capped at 672 hours (24 h x 28 days) per month", and the pricing page says under
  the *Starter*, *Premium* and *Cloud Native* tabs that the plans "are billed by the starting hour, maximum
  of 28 days per month" (saved copy of <https://upcloud.com/pricing/>, read 2026-11-14). It applies to every
  plan and zone of the offering, never to its storage price.
- **Storage price** (pricing page, *Cloud Servers*, *Additional services*, *Block Storage* table, read
  2026-11-13; <https://upcloud.com/pricing/>; Block Storage billing page,
  <https://upcloud.com/docs/products/block-storage/billing/>, read 2026-11-13): `0.2231` per GiB a
  month, in every zone, the MaxIOPS "€0.220/GB/mo" of the table converted to a 730-hour month (*Prices*), in both its "Global Price" and "Helsinki
  Price" columns. It prices the storage a server has beyond its plan's included storage. The billing page
  says how the two combine: "If the server has multiple storage devices, the largest storage that fits
  within the plan quota is counted as part of the plan. Any additional storage devices are billed per GB
  per hour", at the tier's price in either case ("MaxIOPS: No cost within quota, Billed per GB per hour";
  the same for Standard). So a server needing more than the plan includes pays the plan price plus the
  extra GiB at the price of the tier they are on, and a Cloud Native server, which includes none, pays
  for all of its storage that way. A server's extra devices "can be from any of the three available Block
  Storage tiers" (<https://upcloud.com/docs/products/cloud-servers/storage/>, read 2026-11-13), and ADR
  0024 allows one storage price per zone, so MaxIOPS stands for all, for the reasons given under *Block
  Storage*: the tier Premium plans include, the same price in every zone, and outside the pricing change
  of 2026-11-16. Extra storage on Standard ("€0.085/GB/mo"), the tier a Starter plan includes, is
  estimated high. The billing page names no rule for a single device larger than the quota; if none of a
  server's devices fits within it, the whole device is billed per GB, and the estimate, which prices only
  the storage above the quota, is low.

| Plan | Tab | CPU cores | Memory | Included storage | Page price | Stored |
|---|---|---|---|---|---|---|
| `STARTER-1xCPU-1GB` | Starter | 1 | 1 GB | 10 GB Standard | €3/mo | `3.00` |
| `STARTER-1xCPU-2GB` | Starter | 1 | 2 GB | 20 GB Standard | €6/mo | `6.00` |
| `STARTER-2xCPU-2GB` | Starter | 2 | 2 GB | 30 GB Standard | €8/mo | `8.00` |
| `STARTER-1xCPU-4GB` | Starter | 1 | 4 GB | 30 GB Standard | €10/mo | `10.00` |
| `STARTER-2xCPU-4GB` | Starter | 2 | 4 GB | 30 GB Standard | €12/mo | `12.00` |
| `STARTER-2xCPU-8GB` | Starter | 2 | 8 GB | 40 GB Standard | €18/mo | `18.00` |
| `STARTER-4xCPU-8GB` | Starter | 4 | 8 GB | 40 GB Standard | €20/mo | `20.00` |
| `STARTER-2xCPU-16GB` | Starter | 2 | 16 GB | 50 GB Standard | €24/mo | `24.00` |
| `STARTER-4xCPU-16GB` | Starter | 4 | 16 GB | 50 GB Standard | €28/mo | `28.00` |
| `PREMIUM-1xCPU-1GB` | Premium | 1 | 1 GB | 25 GB MaxIOPS | €5/mo | `5.00` |
| `PREMIUM-1xCPU-2GB` | Premium | 1 | 2 GB | 25 GB MaxIOPS | €12/mo | `12.00` |
| `PREMIUM-2xCPU-2GB` | Premium | 2 | 2 GB | 50 GB MaxIOPS | €16/mo | `16.00` |
| `PREMIUM-2xCPU-4GB` | Premium | 2 | 4 GB | 50 GB MaxIOPS | €26/mo | `26.00` |
| `PREMIUM-2xCPU-8GB` | Premium | 2 | 8 GB | 100 GB MaxIOPS | €38/mo | `38.00` |
| `PREMIUM-4xCPU-8GB` | Premium | 4 | 8 GB | 100 GB MaxIOPS | €52/mo | `52.00` |
| `PREMIUM-2xCPU-16GB` | Premium | 2 | 16 GB | 150 GB MaxIOPS | €72/mo | `72.00` |
| `PREMIUM-4xCPU-16GB` | Premium | 4 | 16 GB | 150 GB MaxIOPS | €92/mo | `92.00` |
| `PREMIUM-8xCPU-16GB` | Premium | 8 | 16 GB | 200 GB MaxIOPS | €148/mo | `148.00` |
| `PREMIUM-4xCPU-32GB` | Premium | 4 | 32 GB | 200 GB MaxIOPS | €142/mo | `142.00` |
| `PREMIUM-8xCPU-32GB` | Premium | 8 | 32 GB | 300 GB MaxIOPS | €192/mo | `192.00` |
| `PREMIUM-16xCPU-32GB` | Premium | 16 | 32 GB | 300 GB MaxIOPS | €310/mo | `310.00` |
| `PREMIUM-8xCPU-64GB` | Premium | 8 | 64 GB | 300 GB MaxIOPS | €268/mo | `268.00` |
| `PREMIUM-16xCPU-64GB` | Premium | 16 | 64 GB | 300 GB MaxIOPS | €384/mo | `384.00` |
| `PREMIUM-32xCPU-64GB` | Premium | 32 | 64 GB | 400 GB MaxIOPS | €620/mo | `620.00` |
| `PREMIUM-24xCPU-96GB` | Premium | 24 | 96 GB | 400 GB MaxIOPS | €576/mo | `576.00` |
| `PREMIUM-48xCPU-96GB` | Premium | 48 | 96 GB | 400 GB MaxIOPS | €1056/mo | `1056.00` |
| `PREMIUM-8xCPU-128GB` | Premium | 8 | 128 GB | 400 GB MaxIOPS | €480/mo | `480.00` |
| `PREMIUM-32xCPU-128GB` | Premium | 32 | 128 GB | 500 GB MaxIOPS | €768/mo | `768.00` |
| `PREMIUM-64xCPU-128GB` | Premium | 64 | 128 GB | 500 GB MaxIOPS | €1248/mo | `1248.00` |
| `PREMIUM-38xCPU-192GB` | Premium | 38 | 192 GB | 500 GB MaxIOPS | €1024/mo | `1024.00` |
| `PREMIUM-48xCPU-256GB` | Premium | 48 | 256 GB | 500 GB MaxIOPS | €1364/mo | `1364.00` |
| `PREMIUM-64xCPU-384GB` | Premium | 64 | 384 GB | 500 GB MaxIOPS | €1992/mo | `1992.00` |
| `PREMIUM-80xCPU-512GB` | Premium | 80 | 512 GB | 500 GB MaxIOPS | €2552/mo | `2552.00` |
| `CLOUDNATIVE-1xCPU-4GB` | Cloud Native | 1 | 4 GB | none | €12/mo | `12.00` |
| `CLOUDNATIVE-2xCPU-4GB` | Cloud Native | 2 | 4 GB | none | €15/mo | `15.00` |
| `CLOUDNATIVE-1xCPU-8GB` | Cloud Native | 1 | 8 GB | none | €20/mo | `20.00` |
| `CLOUDNATIVE-2xCPU-8GB` | Cloud Native | 2 | 8 GB | none | €24/mo | `24.00` |
| `CLOUDNATIVE-4xCPU-8GB` | Cloud Native | 4 | 8 GB | none | €32/mo | `32.00` |
| `CLOUDNATIVE-2xCPU-16GB` | Cloud Native | 2 | 16 GB | none | €34/mo | `34.00` |
| `CLOUDNATIVE-4xCPU-16GB` | Cloud Native | 4 | 16 GB | none | €68/mo | `68.00` |
| `CLOUDNATIVE-6xCPU-16GB` | Cloud Native | 6 | 16 GB | none | €74/mo | `74.00` |
| `CLOUDNATIVE-8xCPU-16GB` | Cloud Native | 8 | 16 GB | none | €80/mo | `80.00` |
| `CLOUDNATIVE-4xCPU-24GB` | Cloud Native | 4 | 24 GB | none | €72/mo | `72.00` |
| `CLOUDNATIVE-6xCPU-24GB` | Cloud Native | 6 | 24 GB | none | €78/mo | `78.00` |
| `CLOUDNATIVE-8xCPU-24GB` | Cloud Native | 8 | 24 GB | none | €84/mo | `84.00` |
| `CLOUDNATIVE-12xCPU-24GB` | Cloud Native | 12 | 24 GB | none | €90/mo | `90.00` |
| `CLOUDNATIVE-4xCPU-32GB` | Cloud Native | 4 | 32 GB | none | €90/mo | `90.00` |
| `CLOUDNATIVE-8xCPU-32GB` | Cloud Native | 8 | 32 GB | none | €130/mo | `130.00` |
| `CLOUDNATIVE-12xCPU-32GB` | Cloud Native | 12 | 32 GB | none | €145/mo | `145.00` |
| `CLOUDNATIVE-16xCPU-32GB` | Cloud Native | 16 | 32 GB | none | €160/mo | `160.00` |
| `CLOUDNATIVE-4xCPU-48GB` | Cloud Native | 4 | 48 GB | none | €160/mo | `160.00` |
| `CLOUDNATIVE-8xCPU-48GB` | Cloud Native | 8 | 48 GB | none | €190/mo | `190.00` |
| `CLOUDNATIVE-16xCPU-48GB` | Cloud Native | 16 | 48 GB | none | €230/mo | `230.00` |
| `CLOUDNATIVE-8xCPU-64GB` | Cloud Native | 8 | 64 GB | none | €220/mo | `220.00` |
| `CLOUDNATIVE-16xCPU-64GB` | Cloud Native | 16 | 64 GB | none | €270/mo | `270.00` |
| `CLOUDNATIVE-20xCPU-64GB` | Cloud Native | 20 | 64 GB | none | €285/mo | `285.00` |
| `CLOUDNATIVE-32xCPU-64GB` | Cloud Native | 32 | 64 GB | none | €300/mo | `300.00` |
| `CLOUDNATIVE-8xCPU-96GB` | Cloud Native | 8 | 96 GB | none | €240/mo | `240.00` |
| `CLOUDNATIVE-16xCPU-96GB` | Cloud Native | 16 | 96 GB | none | €340/mo | `340.00` |
| `CLOUDNATIVE-20xCPU-96GB` | Cloud Native | 20 | 96 GB | none | €380/mo | `380.00` |
| `CLOUDNATIVE-8xCPU-128GB` | Cloud Native | 8 | 128 GB | none | €340/mo | `340.00` |
| `CLOUDNATIVE-16xCPU-128GB` | Cloud Native | 16 | 128 GB | none | €370/mo | `370.00` |
| `CLOUDNATIVE-32xCPU-128GB` | Cloud Native | 32 | 128 GB | none | €460/mo | `460.00` |
| `CLOUDNATIVE-16xCPU-192GB` | Cloud Native | 16 | 192 GB | none | €560/mo | `560.00` |
| `CLOUDNATIVE-32xCPU-192GB` | Cloud Native | 32 | 192 GB | none | €670/mo | `670.00` |
| `CLOUDNATIVE-64xCPU-192GB` | Cloud Native | 64 | 192 GB | none | €700/mo | `700.00` |
| `CLOUDNATIVE-24xCPU-256GB` | Cloud Native | 24 | 256 GB | none | €720/mo | `720.00` |
| `CLOUDNATIVE-32xCPU-256GB` | Cloud Native | 32 | 256 GB | none | €740/mo | `740.00` |
| `CLOUDNATIVE-64xCPU-256GB` | Cloud Native | 64 | 256 GB | none | €820/mo | `820.00` |
| `CLOUDNATIVE-32xCPU-384GB` | Cloud Native | 32 | 384 GB | none | €880/mo | `880.00` |
| `CLOUDNATIVE-48xCPU-384GB` | Cloud Native | 48 | 384 GB | none | €920/mo | `920.00` |
| `CLOUDNATIVE-64xCPU-384GB` | Cloud Native | 64 | 384 GB | none | €980/mo | `980.00` |
| `CLOUDNATIVE-48xCPU-512GB` | Cloud Native | 48 | 512 GB | none | €1200/mo | `1200.00` |
| `CLOUDNATIVE-64xCPU-512GB` | Cloud Native | 64 | 512 GB | none | €1400/mo | `1400.00` |
| `CLOUDNATIVE-80xCPU-512GB` | Cloud Native | 80 | 512 GB | none | €1800/mo | `1800.00` |

### Managed Kubernetes

Category `kubernetes.managed`. Pages: <https://upcloud.com/docs/products/managed-kubernetes/availability/>,
<https://upcloud.com/docs/products/managed-kubernetes/control-plane/>,
<https://upcloud.com/docs/products/managed-kubernetes/billing/>.

The base offering is the lowest control-plane plan, **Development** (`dev-md`), which the control plane
page says "runs a single control plane instance, with one API server and one etcd member, so it has no
redundancy". The billing page lists it with "No high availability". It lists no capability: autoscaling
and encryption are left out (*Left out on purpose*), and high availability comes with the Production
plan, the variant *Managed Kubernetes Production*. Worker nodes are Cloud Servers (billing page); no
capability follows from that.

- Regions `de-fra1`, `dk-cph1`, `es-mad1`, `fi-hel1`, `fi-hel2`, `nl-ams1`, `pl-waw1`, `se-sto1`: the
  availability page says "Managed Kubernetes is available in all UpCloud locations" and marks each of
  these zones.
- **Plans and prices** (pricing page, *Managed Kubernetes*, *Control plane* and *Data plane*, and
  *Cloud Servers*, read 2026-11-13; <https://upcloud.com/pricing/>; Managed Kubernetes billing page,
  <https://upcloud.com/docs/products/managed-kubernetes/billing/>, read 2026-11-13). **Fixed fee per
  cluster** (the control plane, ADR 0026): this offering stands for the Development control plane, which
  costs "€0/mo" on the pricing page (saved copy of <https://upcloud.com/pricing/>, read 2026-11-13,
  *Managed Kubernetes*, *Control plane*) and is "Free of charge" on the billing page (fetched 2026-11-14).
  It is recorded as an explicit `0.00` in every zone of the offering, as the OVHcloud snapshot records its
  free control plane, so an estimate reads it as known to be free rather than unknown. Worker nodes are
  Cloud Servers: the billing page says the data plane "is billed by the Worker node according to the
  selected configuration" and "Worker nodes can use Starter, Premium, Cloud Native and GPU plans with at
  least 2 GB of memory". So a plan is a **worker node plan at its *Cloud Servers* price**, and an estimate
  counts nodes. The plans are every *Cloud Servers* plan with at least 2 GB of memory, except the Cloud
  Native plans: 31 of the 75 (`STARTER-1xCPU-1GB` and `PREMIUM-1xCPU-1GB` are below the minimum), at the
  same monthly list prices: the data plane table says "All node plans are billed by the starting hour,
  maximum of 28 days per month". The 42 Cloud Native plans include no storage, every node needs a disk,
  and this offering has no storage price, so a Cloud Native plan would price a node without its disk; GPU
  plans are left out too (*Left out on purpose*). Included storage is as for *Cloud Servers*.
- **Plan billing cap** `672` hours a month (ADR 0029), as for *Cloud Servers*: the pricing page says under
  the *Data plane* table that "All node plans are billed by the starting hour, maximum of 28 days per
  month" (saved copy of <https://upcloud.com/pricing/>, read 2026-11-14), and worker nodes are Cloud
  Servers, "capped at 672 hours (24 h x 28 days) per month" (configurations page,
  <https://upcloud.com/docs/products/cloud-servers/configurations/>, read 2026-11-13). It caps the node
  plans only; the control-plane fee is not capped.
- **No storage price.** Worker-node storage beyond a plan's included storage is not priced: no page read
  on 2026-11-13 gives a per-GiB price for it (*Left out on purpose*). A sized node needing more disk than
  its plan includes is "not estimated" (PRD #190 milestone 6).

| Plan | Tab | CPU cores | Memory | Included storage | Page price | Stored |
|---|---|---|---|---|---|---|
| `STARTER-1xCPU-2GB` | Starter | 1 | 2 GB | 20 GB Standard | €6/mo | `6.00` |
| `STARTER-2xCPU-2GB` | Starter | 2 | 2 GB | 30 GB Standard | €8/mo | `8.00` |
| `STARTER-1xCPU-4GB` | Starter | 1 | 4 GB | 30 GB Standard | €10/mo | `10.00` |
| `STARTER-2xCPU-4GB` | Starter | 2 | 4 GB | 30 GB Standard | €12/mo | `12.00` |
| `STARTER-2xCPU-8GB` | Starter | 2 | 8 GB | 40 GB Standard | €18/mo | `18.00` |
| `STARTER-4xCPU-8GB` | Starter | 4 | 8 GB | 40 GB Standard | €20/mo | `20.00` |
| `STARTER-2xCPU-16GB` | Starter | 2 | 16 GB | 50 GB Standard | €24/mo | `24.00` |
| `STARTER-4xCPU-16GB` | Starter | 4 | 16 GB | 50 GB Standard | €28/mo | `28.00` |
| `PREMIUM-1xCPU-2GB` | Premium | 1 | 2 GB | 25 GB MaxIOPS | €12/mo | `12.00` |
| `PREMIUM-2xCPU-2GB` | Premium | 2 | 2 GB | 50 GB MaxIOPS | €16/mo | `16.00` |
| `PREMIUM-2xCPU-4GB` | Premium | 2 | 4 GB | 50 GB MaxIOPS | €26/mo | `26.00` |
| `PREMIUM-2xCPU-8GB` | Premium | 2 | 8 GB | 100 GB MaxIOPS | €38/mo | `38.00` |
| `PREMIUM-4xCPU-8GB` | Premium | 4 | 8 GB | 100 GB MaxIOPS | €52/mo | `52.00` |
| `PREMIUM-2xCPU-16GB` | Premium | 2 | 16 GB | 150 GB MaxIOPS | €72/mo | `72.00` |
| `PREMIUM-4xCPU-16GB` | Premium | 4 | 16 GB | 150 GB MaxIOPS | €92/mo | `92.00` |
| `PREMIUM-8xCPU-16GB` | Premium | 8 | 16 GB | 200 GB MaxIOPS | €148/mo | `148.00` |
| `PREMIUM-4xCPU-32GB` | Premium | 4 | 32 GB | 200 GB MaxIOPS | €142/mo | `142.00` |
| `PREMIUM-8xCPU-32GB` | Premium | 8 | 32 GB | 300 GB MaxIOPS | €192/mo | `192.00` |
| `PREMIUM-16xCPU-32GB` | Premium | 16 | 32 GB | 300 GB MaxIOPS | €310/mo | `310.00` |
| `PREMIUM-8xCPU-64GB` | Premium | 8 | 64 GB | 300 GB MaxIOPS | €268/mo | `268.00` |
| `PREMIUM-16xCPU-64GB` | Premium | 16 | 64 GB | 300 GB MaxIOPS | €384/mo | `384.00` |
| `PREMIUM-32xCPU-64GB` | Premium | 32 | 64 GB | 400 GB MaxIOPS | €620/mo | `620.00` |
| `PREMIUM-24xCPU-96GB` | Premium | 24 | 96 GB | 400 GB MaxIOPS | €576/mo | `576.00` |
| `PREMIUM-48xCPU-96GB` | Premium | 48 | 96 GB | 400 GB MaxIOPS | €1056/mo | `1056.00` |
| `PREMIUM-8xCPU-128GB` | Premium | 8 | 128 GB | 400 GB MaxIOPS | €480/mo | `480.00` |
| `PREMIUM-32xCPU-128GB` | Premium | 32 | 128 GB | 500 GB MaxIOPS | €768/mo | `768.00` |
| `PREMIUM-64xCPU-128GB` | Premium | 64 | 128 GB | 500 GB MaxIOPS | €1248/mo | `1248.00` |
| `PREMIUM-38xCPU-192GB` | Premium | 38 | 192 GB | 500 GB MaxIOPS | €1024/mo | `1024.00` |
| `PREMIUM-48xCPU-256GB` | Premium | 48 | 256 GB | 500 GB MaxIOPS | €1364/mo | `1364.00` |
| `PREMIUM-64xCPU-384GB` | Premium | 64 | 384 GB | 500 GB MaxIOPS | €1992/mo | `1992.00` |
| `PREMIUM-80xCPU-512GB` | Premium | 80 | 512 GB | 500 GB MaxIOPS | €2552/mo | `2552.00` |

### Managed Kubernetes Production

Category `kubernetes.managed`. A variant of *Managed Kubernetes* (tier-variant rule): a cluster on the
**Production** control-plane plan (`prod-md-ha`). Pages:
<https://upcloud.com/docs/products/managed-kubernetes/control-plane/>,
<https://upcloud.com/docs/products/managed-kubernetes/billing/>,
<https://upcloud.com/docs/products/managed-kubernetes/availability/>,
<https://upcloud.com/docs/products/managed-kubernetes/data-plane/>,
<https://upcloud.com/docs/products/managed-kubernetes/faq/>.

- `node-high-availability`: the control plane page says the Production plan "runs a highly available
  control plane with three API servers and a three-member etcd cluster. The etcd members run on separate
  hosts." The billing page lists it with "High Availability". Three API servers and a three-member etcd
  on separate hosts keep the control plane serving through the loss of a host with no manual action.
  **Needs:** the Production plan. **Scope:** node level only. The cluster lives in one zone (its nodes
  are connected through private networks "in the same zone as the Managed Kubernetes cluster", data plane page, and "Worker nodes
  are deployed to the same zone as the cluster is located in", Kubernetes FAQ), and a
  zone is one data centre, so zone level is not claimed.
- Regions `de-fra1`, `dk-cph1`, `es-mad1`, `fi-hel1`, `fi-hel2`, `nl-ams1`, `pl-waw1`, `se-sto1`
  (tier-variant zones): the availability page says "Managed Kubernetes is available in all UpCloud
  locations", with one column for the product and none per plan, and the control plane page states both
  plans with no location caveat.
- **Plans and prices** (pricing page, *Managed Kubernetes*, *Control plane* and *Data plane*, and
  *Cloud Servers*, read 2026-11-13; <https://upcloud.com/pricing/>; Managed Kubernetes billing page,
  <https://upcloud.com/docs/products/managed-kubernetes/billing/>, read 2026-11-13): the worker node plans
  and prices are those of *Managed Kubernetes*, for the same reasons: the data plane does not depend on the
  control plane plan. So they are the same 31 Starter and Premium plans, without the 42 Cloud Native
  plans, which include no storage while this offering has no storage price either.
- **Fixed fee per cluster** (the control plane, ADR 0026): the Production control plane costs "€60/mo"
  per cluster (saved copy of <https://upcloud.com/pricing/>, read 2026-11-13, *Managed Kubernetes*,
  *Control plane*). The page's footer under that table says "Resources are charged by the hour. Monthly
  prices are estimates based on a 30-day month.", and the billing page
  (<https://upcloud.com/docs/products/managed-kubernetes/billing/>, fetched 2026-11-14) says "Billed per
  hour". So it is converted like every other hourly-billed UpCloud price (*Prices*): 60 × 730 / 720 =
  60.833333… → `60.8333` (half up, 4 decimal places), recorded once in every zone of the offering. A
  plan is multiplied by the node count; the fee is charged once per cluster, never per node. Milestone 6
  of PRD #190 scales fixed fees by `hoursPerMonth` / 730 (ADR 0026); the plan billing cap below never
  applies to it (ADR 0029).
- **Plan billing cap** `672` hours a month on the worker node plans (ADR 0029), as for *Managed
  Kubernetes*: "All node plans are billed by the starting hour, maximum of 28 days per month" (pricing
  page, *Data plane*, saved copy of <https://upcloud.com/pricing/>, read 2026-11-14), and Cloud Servers are
  "capped at 672 hours (24 h x 28 days) per month" (configurations page,
  <https://upcloud.com/docs/products/cloud-servers/configurations/>, read 2026-11-13). The control
  plane's footer says instead "Resources are charged by the hour. Monthly prices are estimates based on a
  30-day month.", so the fee above stays uncapped.
- **No storage price**, as for *Managed Kubernetes*: worker-node storage beyond a plan's included storage
  is not priced (*Left out on purpose*), and a sized node needing more disk than its plan includes is "not
  estimated" (PRD #190 milestone 6).

| Plan | Tab | CPU cores | Memory | Included storage | Page price | Stored |
|---|---|---|---|---|---|---|
| `STARTER-1xCPU-2GB` | Starter | 1 | 2 GB | 20 GB Standard | €6/mo | `6.00` |
| `STARTER-2xCPU-2GB` | Starter | 2 | 2 GB | 30 GB Standard | €8/mo | `8.00` |
| `STARTER-1xCPU-4GB` | Starter | 1 | 4 GB | 30 GB Standard | €10/mo | `10.00` |
| `STARTER-2xCPU-4GB` | Starter | 2 | 4 GB | 30 GB Standard | €12/mo | `12.00` |
| `STARTER-2xCPU-8GB` | Starter | 2 | 8 GB | 40 GB Standard | €18/mo | `18.00` |
| `STARTER-4xCPU-8GB` | Starter | 4 | 8 GB | 40 GB Standard | €20/mo | `20.00` |
| `STARTER-2xCPU-16GB` | Starter | 2 | 16 GB | 50 GB Standard | €24/mo | `24.00` |
| `STARTER-4xCPU-16GB` | Starter | 4 | 16 GB | 50 GB Standard | €28/mo | `28.00` |
| `PREMIUM-1xCPU-2GB` | Premium | 1 | 2 GB | 25 GB MaxIOPS | €12/mo | `12.00` |
| `PREMIUM-2xCPU-2GB` | Premium | 2 | 2 GB | 50 GB MaxIOPS | €16/mo | `16.00` |
| `PREMIUM-2xCPU-4GB` | Premium | 2 | 4 GB | 50 GB MaxIOPS | €26/mo | `26.00` |
| `PREMIUM-2xCPU-8GB` | Premium | 2 | 8 GB | 100 GB MaxIOPS | €38/mo | `38.00` |
| `PREMIUM-4xCPU-8GB` | Premium | 4 | 8 GB | 100 GB MaxIOPS | €52/mo | `52.00` |
| `PREMIUM-2xCPU-16GB` | Premium | 2 | 16 GB | 150 GB MaxIOPS | €72/mo | `72.00` |
| `PREMIUM-4xCPU-16GB` | Premium | 4 | 16 GB | 150 GB MaxIOPS | €92/mo | `92.00` |
| `PREMIUM-8xCPU-16GB` | Premium | 8 | 16 GB | 200 GB MaxIOPS | €148/mo | `148.00` |
| `PREMIUM-4xCPU-32GB` | Premium | 4 | 32 GB | 200 GB MaxIOPS | €142/mo | `142.00` |
| `PREMIUM-8xCPU-32GB` | Premium | 8 | 32 GB | 300 GB MaxIOPS | €192/mo | `192.00` |
| `PREMIUM-16xCPU-32GB` | Premium | 16 | 32 GB | 300 GB MaxIOPS | €310/mo | `310.00` |
| `PREMIUM-8xCPU-64GB` | Premium | 8 | 64 GB | 300 GB MaxIOPS | €268/mo | `268.00` |
| `PREMIUM-16xCPU-64GB` | Premium | 16 | 64 GB | 300 GB MaxIOPS | €384/mo | `384.00` |
| `PREMIUM-32xCPU-64GB` | Premium | 32 | 64 GB | 400 GB MaxIOPS | €620/mo | `620.00` |
| `PREMIUM-24xCPU-96GB` | Premium | 24 | 96 GB | 400 GB MaxIOPS | €576/mo | `576.00` |
| `PREMIUM-48xCPU-96GB` | Premium | 48 | 96 GB | 400 GB MaxIOPS | €1056/mo | `1056.00` |
| `PREMIUM-8xCPU-128GB` | Premium | 8 | 128 GB | 400 GB MaxIOPS | €480/mo | `480.00` |
| `PREMIUM-32xCPU-128GB` | Premium | 32 | 128 GB | 500 GB MaxIOPS | €768/mo | `768.00` |
| `PREMIUM-64xCPU-128GB` | Premium | 64 | 128 GB | 500 GB MaxIOPS | €1248/mo | `1248.00` |
| `PREMIUM-38xCPU-192GB` | Premium | 38 | 192 GB | 500 GB MaxIOPS | €1024/mo | `1024.00` |
| `PREMIUM-48xCPU-256GB` | Premium | 48 | 256 GB | 500 GB MaxIOPS | €1364/mo | `1364.00` |
| `PREMIUM-64xCPU-384GB` | Premium | 64 | 384 GB | 500 GB MaxIOPS | €1992/mo | `1992.00` |
| `PREMIUM-80xCPU-512GB` | Premium | 80 | 512 GB | 500 GB MaxIOPS | €2552/mo | `2552.00` |

### Managed MySQL

Category `database.mysql`. Pages: <https://upcloud.com/docs/products/managed-mysql/availability/>,
<https://upcloud.com/docs/products/managed-mysql/configurations/>,
<https://upcloud.com/docs/products/managed-mysql/backups/>,
<https://upcloud.com/docs/products/managed-mysql/encryption/>,
<https://upcloud.com/docs/getting-started/encryption/>.

The base offering is a single-node service, the lowest tier: the configurations page says "Developer
compute plans run on a single node", and Standard and High Memory plans "can run on 1, 2, or 3 nodes". A
cluster of two or three nodes is the variant *Managed MySQL Multi-node*.

- `point-in-time-restore`: the backups page says "Each Managed Database plan includes automated backups",
  which "allows users to restore to any given point in time (PITR) within the selected retention window".
  A restore creates a new service. **Needs:** nothing beyond the base, single-node tier.
- `encryption-at-rest`: the encryption page says "Data-at-rest encryption covers both active service
  instances and backups", with full-volume LUKS encryption, and the encryption overview says "Managed
  Databases generate a unique key for each service instance and volume". **Needs:** nothing beyond the
  base, single-node tier.
- Regions `de-fra1`, `dk-cph1`, `es-mad1`, `fi-hel1`, `fi-hel2`, `nl-ams1`, `pl-waw1`, `se-sto1`: the
  availability page marks each of these zones available.

- **Plans and prices** (pricing page, *Managed Databases*, *Relational databases*, tabs *Standard* and
  *High Memory* for "MySQL & PostgreSQL", and its *Additional services*, read 2026-11-13;
  <https://upcloud.com/pricing/>; identifiers and sizes from the configurations page,
  <https://upcloud.com/docs/products/managed-mysql/configurations/>, read 2026-11-13). The page gives one
  table for both engines, so MySQL and PostgreSQL have the same plans and prices. A service is "configured
  from four components that you select separately": compute ("the CPU cores and memory of each node"),
  storage, nodes and backups (configurations page). A plan is one compute plan on **one node**, the base
  offering's single-node tier, at the page's "Global price": the first node's price, with no discount for
  more nodes. vCPU and memory are the page's "Cores" and "Memory". Included storage is `0`: storage is
  selected and billed separately, so no plan includes any. The 15-day point-in-time backups ("PITR backup
  days" 15) are included in the price; "Extended 31-day backup retention" costs extra and is not recorded.
  The prices are the page's 30-day monthly figures converted to a 730-hour month, × 730 / 720 and
  rounded half up to 4 decimal places (*Prices*): €60/mo is stored as `60.8333`. The Developer plans are left out (*Left out
  on purpose*).
- **Storage price**: the page's *Storage* table gives the "Additional Storage" price of the Standard and
  High Memory plans as "MaxIOPS – €0.10/GB/mo", stored as `0.1014` per GiB a month (0.10 × 730 / 720,
  *Prices*), in every zone. On one
  node it is the price of a GiB of the service's data. Each plan has a minimum storage size (10, 80 or
  500 GiB, configurations page), which is billed whatever the data size; an estimate whose size is below
  that minimum is low by the difference.

| Plan | Family | CPU cores | Memory | Page price | Stored |
|---|---|---|---|---|---|
| `rdb.standard.2CPU-8GB` | Standard | 2 | 8 GB | €60/mo | `60.8333` |
| `rdb.standard.4CPU-16GB` | Standard | 4 | 16 GB | €140/mo | `141.9444` |
| `rdb.standard.8CPU-32GB` | Standard | 8 | 32 GB | €317/mo | `321.4028` |
| `rdb.standard.12CPU-48GB` | Standard | 12 | 48 GB | €485/mo | `491.7361` |
| `rdb.standard.16CPU-64GB` | Standard | 16 | 64 GB | €665/mo | `674.2361` |
| `rdb.standard.24CPU-96GB` | Standard | 24 | 96 GB | €993/mo | `1006.7917` |
| `rdb.standard.32CPU-128GB` | Standard | 32 | 128 GB | €1317/mo | `1335.2917` |
| `rdb.standard.48CPU-192GB` | Standard | 48 | 192 GB | €2017/mo | `2045.0139` |
| `rdb.standard.64CPU-256GB` | Standard | 64 | 256 GB | €2669/mo | `2706.0694` |
| `rdb.standard.80CPU-320GB` | Standard | 80 | 320 GB | €3429/mo | `3476.625` |
| `rdb.memory.2CPU-16GB` | High Memory | 2 | 16 GB | €126/mo | `127.75` |
| `rdb.memory.4CPU-32GB` | High Memory | 4 | 32 GB | €283/mo | `286.9306` |
| `rdb.memory.8CPU-64GB` | High Memory | 8 | 64 GB | €593/mo | `601.2361` |
| `rdb.memory.12CPU-96GB` | High Memory | 12 | 96 GB | €883/mo | `895.2639` |
| `rdb.memory.16CPU-128GB` | High Memory | 16 | 128 GB | €1173/mo | `1189.2917` |
| `rdb.memory.24CPU-192GB` | High Memory | 24 | 192 GB | €1800/mo | `1825.00` |
| `rdb.memory.32CPU-256GB` | High Memory | 32 | 256 GB | €2380/mo | `2413.0556` |
| `rdb.memory.48CPU-384GB` | High Memory | 48 | 384 GB | €3655/mo | `3705.7639` |
| `rdb.memory.64CPU-512GB` | High Memory | 64 | 512 GB | €4855/mo | `4922.4306` |

### Managed MySQL Multi-node

Category `database.mysql`. A variant of *Managed MySQL* (tier-variant rule): a cluster of two or three
nodes, which the documentation calls a multi-node plan. Pages:
<https://upcloud.com/docs/products/managed-mysql/high-availability/>,
<https://upcloud.com/docs/products/managed-mysql/faq/>,
<https://upcloud.com/docs/products/managed-mysql/configurations/>,
<https://upcloud.com/docs/products/managed-mysql/availability/>.

- `point-in-time-restore`, `encryption-at-rest`: as for *Managed MySQL*; the backups and encryption pages
  state them for every plan. **Needs:** nothing beyond the base tier, which this tier keeps.
- `node-high-availability`: the configurations page says a cluster of 2 or 3 nodes "has a primary node and
  standby nodes that take over automatically if the primary fails". The high availability page says the
  nodes "are created on physically separated backend hosts to ensure redundancy in case of hardware
  failure", that a standby is "elevated to the role of primary" and the hostname "instantly pointed
  towards the new primary node". The FAQ says "the automated fail-over is only possible with clusters of
  2 or more nodes". **Needs:** two or three nodes. **Scope:** node level only. Every node is in the
  service's one zone, a single data centre; off-site backups protect the data, but recovering from the
  loss of the zone is a restore into a new service, not failover (*Left out on purpose*).
- `read-replicas`: the high availability page says standby nodes "can serve as additional nodes dedicated
  to handling read-only queries", reached through a separate DNS entry that balances queries across the
  standby nodes. **Needs:** two or three nodes; a single node has no standby to read from.
- Regions `de-fra1`, `dk-cph1`, `es-mad1`, `fi-hel1`, `fi-hel2`, `nl-ams1`, `pl-waw1`, `se-sto1`
  (tier-variant zones): the availability page marks each of these zones, with no column per plan, and the
  configurations page states the 1-, 2- and 3-node forms with no location caveat.
- **Plans and prices** (pricing page, *Managed Databases*, *Additional services*, *High Availability*,
  read 2026-11-13; <https://upcloud.com/pricing/>; maintainer decision of 2026-11-14). The page says
  "Standard and High Memory plan Managed Databases for MySQL and PostgreSQL are available with 1 to 3
  database nodes" and gives this table:

  | Plan | Second Node | Third Node |
  |---|---|---|
  | Developer | – | – |
  | Standard | -10% | -30% |
  | High Memory | -10% | -30% |

  The page does not say what the discount is taken off. The reading used here is that each further node
  costs the plan's single-node price less its discount: the second node 90% of it, the third 70%. The
  plans are the same 19 Standard and High Memory plans as *Managed MySQL*, each of which the
  configurations page says "can run on 1, 2, or 3 nodes". Developer plans run on a single node and are
  left out (*Left out on purpose*).

  ADR 0024's format holds one price per plan, and ADR 0025 counts database nodes in `size.count`, so the
  estimate multiplies the plan price by the node count. A plan's price here is therefore the **average
  price of a node in a 2-node cluster**: (1 + 0.90) / 2 = 0.95 of the single-node price. The order of
  operations is the same for every plan: start from the single-node amount already stored in *Managed
  MySQL* (the page's 30-day figure × 730 / 720, rounded half up to 4 decimal places, *Prices*),
  multiply it by 0.95, and round the product once more, half up, to ADR 0024's 4 decimal places. The
  amount is so rounded twice, on purpose, an exception to the single rounding of *Prices*: deriving it
  from the stored single-node amount keeps it exactly 0.95 of a price the snapshot holds. A single
  rounding of page × 730 / 720 × 0.95 would differ by 0.0001 EUR in 5 of the 19 plans (this example:
  `57.7917` against `57.7916`).
  Worked example, `rdb.standard.2CPU-8GB`: €60/mo × 730 / 720 = 60.833333… → `60.8333`;
  60.8333 × 0.95 = 57.791635 → `57.7916`. A 2-node service is estimated at 2 × 57.7916 = 115.5832 EUR
  a month, the single node plus a node at 90%.

  "Multi-node" covers 2 and 3 nodes. A 3-node service really averages (1 + 0.90 + 0.70) / 3 ≈ 0.8667 of
  the single-node price per node, so pricing it at 0.95 estimates it about 9.6% high (0.95 / 0.8667).
  That is chosen on purpose: an overestimate is preferred to an underestimate.
- **Storage price**: `0.2028` per GiB a month in every zone, 2 × the stored single-node amount `0.1014`
  of *Managed MySQL* (the page's "MaxIOPS – €0.10/GB/mo" × 730 / 720, *Prices*), per GiB of the
  service's data, on the same 2-node basis as the plan prices above, and not discounted. The
  configurations page (<https://upcloud.com/docs/products/managed-mysql/configurations/>, read
  2026-11-14) says "The storage size you select applies to each node. For example, a 2-node cluster with
  100 GiB of storage has 100 GiB on each node." Doubling the price is an **inference**: the page says the
  storage applies to each node, but no page says the storage charge is multiplied by the node count. The
  maintainer chose it (2026-11-14) because an overestimate is preferred to an underestimate. A 3-node
  cluster's storage is then estimated a third low (2 of 3 nodes' storage). The minimum storage size of
  each plan applies as on one node (*Managed MySQL*).

| Plan | Family | CPU cores | Memory | Page price, 1 node | Single node, stored | Per node at 2 nodes, stored |
|---|---|---|---|---|---|---|
| `rdb.standard.2CPU-8GB` | Standard | 2 | 8 GB | €60/mo | `60.8333` | `57.7916` |
| `rdb.standard.4CPU-16GB` | Standard | 4 | 16 GB | €140/mo | `141.9444` | `134.8472` |
| `rdb.standard.8CPU-32GB` | Standard | 8 | 32 GB | €317/mo | `321.4028` | `305.3327` |
| `rdb.standard.12CPU-48GB` | Standard | 12 | 48 GB | €485/mo | `491.7361` | `467.1493` |
| `rdb.standard.16CPU-64GB` | Standard | 16 | 64 GB | €665/mo | `674.2361` | `640.5243` |
| `rdb.standard.24CPU-96GB` | Standard | 24 | 96 GB | €993/mo | `1006.7917` | `956.4521` |
| `rdb.standard.32CPU-128GB` | Standard | 32 | 128 GB | €1317/mo | `1335.2917` | `1268.5271` |
| `rdb.standard.48CPU-192GB` | Standard | 48 | 192 GB | €2017/mo | `2045.0139` | `1942.7632` |
| `rdb.standard.64CPU-256GB` | Standard | 64 | 256 GB | €2669/mo | `2706.0694` | `2570.7659` |
| `rdb.standard.80CPU-320GB` | Standard | 80 | 320 GB | €3429/mo | `3476.625` | `3302.7938` |
| `rdb.memory.2CPU-16GB` | High Memory | 2 | 16 GB | €126/mo | `127.75` | `121.3625` |
| `rdb.memory.4CPU-32GB` | High Memory | 4 | 32 GB | €283/mo | `286.9306` | `272.5841` |
| `rdb.memory.8CPU-64GB` | High Memory | 8 | 64 GB | €593/mo | `601.2361` | `571.1743` |
| `rdb.memory.12CPU-96GB` | High Memory | 12 | 96 GB | €883/mo | `895.2639` | `850.5007` |
| `rdb.memory.16CPU-128GB` | High Memory | 16 | 128 GB | €1173/mo | `1189.2917` | `1129.8271` |
| `rdb.memory.24CPU-192GB` | High Memory | 24 | 192 GB | €1800/mo | `1825.00` | `1733.75` |
| `rdb.memory.32CPU-256GB` | High Memory | 32 | 256 GB | €2380/mo | `2413.0556` | `2292.4028` |
| `rdb.memory.48CPU-384GB` | High Memory | 48 | 384 GB | €3655/mo | `3705.7639` | `3520.4757` |
| `rdb.memory.64CPU-512GB` | High Memory | 64 | 512 GB | €4855/mo | `4922.4306` | `4676.3091` |

### Managed Object Storage

Category `storage.object`. Pages: <https://upcloud.com/docs/products/managed-object-storage/>,
<https://upcloud.com/docs/products/managed-object-storage/availability/>,
<https://upcloud.com/docs/products/managed-object-storage/full-encryption/>,
<https://upcloud.com/docs/products/managed-object-storage/s3-standard-compatibility/>,
<https://upcloud.com/docs/guides/enable-and-manage-s3-object-versioning/>.

No storage classes or tiers are documented, so there are no variants.

- `encryption-at-rest`: the full encryption page says the service "encrypts all the data it stores at rest,
  using server-side encryption with the AES-256 algorithm. Encryption is always on." **Needs:** nothing.
- `object-versioning`: the S3 compatibility page lists "Bucket versioning, Object versioning" among the
  supported S3 APIs, and the versioning guide says "Versioning keeps every version of the files in a
  bucket." **Needs:** nothing.
- Regions `de-fra1`, `dk-cph1`, `fi-hel2`, `se-sto1`: the availability page says the service is
  "available in six regions. Each region is hosted in one location", and gives the hosting location of
  each. The four in the EU are the zones listed:
  - `EUROPE-1` is hosted in `fi-hel2`;
  - `EUROPE-2` is hosted in `de-fra1`;
  - `EUROPE-3` is hosted in `se-sto1`;
  - `EUROPE-4` is hosted in `dk-cph1`.

  `APAC-1` (in `sg-sin1`) and `US-1` (in `us-chi1`) are outside the EU. The "Accessible via SDN from"
  column, which names every European zone for each `EUROPE-n` region, `uk-lon1` and `no-svg1` included,
  is reach, not where data is stored, so it adds no zone (*Rules applied*).

- **Storage price** (pricing page, *Managed Object Storage*, read 2026-11-13; <https://upcloud.com/pricing/>;
  billing page, <https://upcloud.com/docs/products/managed-object-storage/billing/>, read 2026-11-13): the
  page gives "250 GB", transfer "Included", "€5/mo", and "+ 250 GB for each upscale" at "+ €5/mo", as a
  "Global price". The billing page says "Storage is billed in increments of 250 GB, and each increment is
  billed per hour. A new service starts with one increment, which is also the minimum billed." The per-GiB
  price is one increment's price over its size: 5 / 250 = 0.02 per GiB a month for a 30-day month, stored converted to a 730-hour
  month, 0.02 × 730 / 720 = 0.020277… → `0.0203` (*Prices*), in every zone of
  the offering. The page's "GB" is taken as GiB, unconverted (*Prices*). An estimate from it is low where
  the data does not fill its last increment (the service pays for the whole increment) and below the
  250 GB minimum; for a size that is a multiple of 250 it is the page's price for a 730-hour month,
  within the rounding (*Prices*).

### Managed PostgreSQL

Category `database.postgresql`. Pages:
<https://upcloud.com/docs/products/managed-postgresql/availability/>,
<https://upcloud.com/docs/products/managed-postgresql/configurations/>,
<https://upcloud.com/docs/products/managed-postgresql/backups/>,
<https://upcloud.com/docs/products/managed-postgresql/encryption/>,
<https://upcloud.com/docs/getting-started/encryption/>.

The base offering is a single-node service, the lowest tier: the configurations page says "Developer
compute plans run on a single node", and Standard and High Memory plans "can run on 1, 2, or 3 nodes". A
cluster of two or three nodes is the variant *Managed PostgreSQL Multi-node*.

- `point-in-time-restore`: the backups page says "Each Managed Database plan includes automated backups",
  which "allows users to restore to any given point in time (PITR) within the selected retention window".
  A restore creates a new service. **Needs:** nothing beyond the base, single-node tier.
- `encryption-at-rest`: the encryption page says "Data-at-rest encryption covers both active service
  instances and backups", with full-volume LUKS encryption, and the encryption overview says "Managed
  Databases generate a unique key for each service instance and volume". **Needs:** nothing beyond the
  base, single-node tier.
- Regions `de-fra1`, `dk-cph1`, `es-mad1`, `fi-hel1`, `fi-hel2`, `nl-ams1`, `pl-waw1`, `se-sto1`: the
  availability page marks each of these zones available.

- **Plans and prices** (pricing page, *Managed Databases*, *Relational databases*, tabs *Standard* and
  *High Memory* for "MySQL & PostgreSQL", and its *Additional services*, read 2026-11-13;
  <https://upcloud.com/pricing/>; identifiers and sizes from the configurations page,
  <https://upcloud.com/docs/products/managed-postgresql/configurations/>, read 2026-11-13). The page gives one
  table for both engines, so MySQL and PostgreSQL have the same plans and prices. A service is "configured
  from four components that you select separately": compute ("the CPU cores and memory of each node"),
  storage, nodes and backups (configurations page). A plan is one compute plan on **one node**, the base
  offering's single-node tier, at the page's "Global price": the first node's price, with no discount for
  more nodes. vCPU and memory are the page's "Cores" and "Memory". Included storage is `0`: storage is
  selected and billed separately, so no plan includes any. The 15-day point-in-time backups ("PITR backup
  days" 15) are included in the price; "Extended 31-day backup retention" costs extra and is not recorded.
  The prices are the page's 30-day monthly figures converted to a 730-hour month, × 730 / 720 and
  rounded half up to 4 decimal places (*Prices*): €60/mo is stored as `60.8333`. The Developer plans are left out (*Left out
  on purpose*).
- **Storage price**: the page's *Storage* table gives the "Additional Storage" price of the Standard and
  High Memory plans as "MaxIOPS – €0.10/GB/mo", stored as `0.1014` per GiB a month (0.10 × 730 / 720,
  *Prices*), in every zone. On one
  node it is the price of a GiB of the service's data. Each plan has a minimum storage size (10, 80 or
  500 GiB, configurations page), which is billed whatever the data size; an estimate whose size is below
  that minimum is low by the difference.

| Plan | Family | CPU cores | Memory | Page price | Stored |
|---|---|---|---|---|---|
| `rdb.standard.2CPU-8GB` | Standard | 2 | 8 GB | €60/mo | `60.8333` |
| `rdb.standard.4CPU-16GB` | Standard | 4 | 16 GB | €140/mo | `141.9444` |
| `rdb.standard.8CPU-32GB` | Standard | 8 | 32 GB | €317/mo | `321.4028` |
| `rdb.standard.12CPU-48GB` | Standard | 12 | 48 GB | €485/mo | `491.7361` |
| `rdb.standard.16CPU-64GB` | Standard | 16 | 64 GB | €665/mo | `674.2361` |
| `rdb.standard.24CPU-96GB` | Standard | 24 | 96 GB | €993/mo | `1006.7917` |
| `rdb.standard.32CPU-128GB` | Standard | 32 | 128 GB | €1317/mo | `1335.2917` |
| `rdb.standard.48CPU-192GB` | Standard | 48 | 192 GB | €2017/mo | `2045.0139` |
| `rdb.standard.64CPU-256GB` | Standard | 64 | 256 GB | €2669/mo | `2706.0694` |
| `rdb.standard.80CPU-320GB` | Standard | 80 | 320 GB | €3429/mo | `3476.625` |
| `rdb.memory.2CPU-16GB` | High Memory | 2 | 16 GB | €126/mo | `127.75` |
| `rdb.memory.4CPU-32GB` | High Memory | 4 | 32 GB | €283/mo | `286.9306` |
| `rdb.memory.8CPU-64GB` | High Memory | 8 | 64 GB | €593/mo | `601.2361` |
| `rdb.memory.12CPU-96GB` | High Memory | 12 | 96 GB | €883/mo | `895.2639` |
| `rdb.memory.16CPU-128GB` | High Memory | 16 | 128 GB | €1173/mo | `1189.2917` |
| `rdb.memory.24CPU-192GB` | High Memory | 24 | 192 GB | €1800/mo | `1825.00` |
| `rdb.memory.32CPU-256GB` | High Memory | 32 | 256 GB | €2380/mo | `2413.0556` |
| `rdb.memory.48CPU-384GB` | High Memory | 48 | 384 GB | €3655/mo | `3705.7639` |
| `rdb.memory.64CPU-512GB` | High Memory | 64 | 512 GB | €4855/mo | `4922.4306` |

### Managed PostgreSQL Multi-node

Category `database.postgresql`. A variant of *Managed PostgreSQL* (tier-variant rule): a cluster of two
or three nodes, which the documentation calls a multi-node plan. Pages:
<https://upcloud.com/docs/products/managed-postgresql/high-availability/>,
<https://upcloud.com/docs/products/managed-postgresql/faq/>,
<https://upcloud.com/docs/products/managed-postgresql/connection-pooling/>,
<https://upcloud.com/docs/products/managed-postgresql/configurations/>,
<https://upcloud.com/docs/products/managed-postgresql/availability/>.

- `point-in-time-restore`, `encryption-at-rest`: as for *Managed PostgreSQL*; the backups and encryption
  pages state them for every plan. **Needs:** nothing beyond the base tier, which this tier keeps.
- `node-high-availability`: the configurations page says a cluster of 2 or 3 nodes "has a primary node and
  standby nodes that take over automatically if the primary fails". The high availability page says the
  nodes "are created on physically separated backend hosts to ensure redundancy in case of hardware
  failure", that a standby is "elevated to the role of primary" and the hostname "instantly pointed
  towards the new primary node". The FAQ says "the automated fail-over is only possible with clusters of
  2 or more nodes". **Needs:** two or three nodes. **Scope:** node level only. Every node is in the
  service's one zone, a single data centre. If all nodes fail, the high availability page says they are
  re-provisioned from backup, "which may result in some data loss" (*Left out on purpose*).
- `read-replicas`: the high availability page says standby nodes "can serve as additional nodes dedicated
  to handling read-only queries", reached through a separate DNS entry; the connection pooling page says
  "On plans with two or more nodes, connecting through the read-only replica hostname runs your queries on
  the replica." **Needs:** two or three nodes; a single node has no standby to read from.
- Regions `de-fra1`, `dk-cph1`, `es-mad1`, `fi-hel1`, `fi-hel2`, `nl-ams1`, `pl-waw1`, `se-sto1`
  (tier-variant zones): the availability page marks each of these zones, with no column per plan, and the
  configurations page states the 1-, 2- and 3-node forms with no location caveat.
- **Plans and prices** (pricing page, *Managed Databases*, *Additional services*, *High Availability*,
  read 2026-11-13; <https://upcloud.com/pricing/>; maintainer decision of 2026-11-14). The page says
  "Standard and High Memory plan Managed Databases for MySQL and PostgreSQL are available with 1 to 3
  database nodes" and gives this table:

  | Plan | Second Node | Third Node |
  |---|---|---|
  | Developer | – | – |
  | Standard | -10% | -30% |
  | High Memory | -10% | -30% |

  The page does not say what the discount is taken off. The reading used here is that each further node
  costs the plan's single-node price less its discount: the second node 90% of it, the third 70%. The
  plans are the same 19 Standard and High Memory plans as *Managed PostgreSQL*, each of which the
  configurations page says "can run on 1, 2, or 3 nodes". Developer plans run on a single node and are
  left out (*Left out on purpose*).

  ADR 0024's format holds one price per plan, and ADR 0025 counts database nodes in `size.count`, so the
  estimate multiplies the plan price by the node count. A plan's price here is therefore the **average
  price of a node in a 2-node cluster**: (1 + 0.90) / 2 = 0.95 of the single-node price. The order of
  operations is the same for every plan: start from the single-node amount already stored in *Managed
  PostgreSQL* (the page's 30-day figure × 730 / 720, rounded half up to 4 decimal places, *Prices*),
  multiply it by 0.95, and round the product once more, half up, to ADR 0024's 4 decimal places. The
  amount is so rounded twice, on purpose, an exception to the single rounding of *Prices*: deriving it
  from the stored single-node amount keeps it exactly 0.95 of a price the snapshot holds. A single
  rounding of page × 730 / 720 × 0.95 would differ by 0.0001 EUR in 5 of the 19 plans (this example:
  `57.7917` against `57.7916`).
  Worked example, `rdb.standard.2CPU-8GB`: €60/mo × 730 / 720 = 60.833333… → `60.8333`;
  60.8333 × 0.95 = 57.791635 → `57.7916`. A 2-node service is estimated at 2 × 57.7916 = 115.5832 EUR
  a month, the single node plus a node at 90%.

  "Multi-node" covers 2 and 3 nodes. A 3-node service really averages (1 + 0.90 + 0.70) / 3 ≈ 0.8667 of
  the single-node price per node, so pricing it at 0.95 estimates it about 9.6% high (0.95 / 0.8667).
  That is chosen on purpose: an overestimate is preferred to an underestimate.
- **Storage price**: `0.2028` per GiB a month in every zone, 2 × the stored single-node amount `0.1014`
  of *Managed PostgreSQL* (the page's "MaxIOPS – €0.10/GB/mo" × 730 / 720, *Prices*), per GiB of the
  service's data, on the same 2-node basis as the plan prices above, and not discounted. The
  configurations page (<https://upcloud.com/docs/products/managed-postgresql/configurations/>, read
  2026-11-14) says "The storage size you select applies to each node. For example, a 2-node cluster with
  100 GiB of storage has 100 GiB on each node." Doubling the price is an **inference**: the page says the
  storage applies to each node, but no page says the storage charge is multiplied by the node count. The
  maintainer chose it (2026-11-14) because an overestimate is preferred to an underestimate. A 3-node
  cluster's storage is then estimated a third low (2 of 3 nodes' storage). The minimum storage size of
  each plan applies as on one node (*Managed PostgreSQL*).

| Plan | Family | CPU cores | Memory | Page price, 1 node | Single node, stored | Per node at 2 nodes, stored |
|---|---|---|---|---|---|---|
| `rdb.standard.2CPU-8GB` | Standard | 2 | 8 GB | €60/mo | `60.8333` | `57.7916` |
| `rdb.standard.4CPU-16GB` | Standard | 4 | 16 GB | €140/mo | `141.9444` | `134.8472` |
| `rdb.standard.8CPU-32GB` | Standard | 8 | 32 GB | €317/mo | `321.4028` | `305.3327` |
| `rdb.standard.12CPU-48GB` | Standard | 12 | 48 GB | €485/mo | `491.7361` | `467.1493` |
| `rdb.standard.16CPU-64GB` | Standard | 16 | 64 GB | €665/mo | `674.2361` | `640.5243` |
| `rdb.standard.24CPU-96GB` | Standard | 24 | 96 GB | €993/mo | `1006.7917` | `956.4521` |
| `rdb.standard.32CPU-128GB` | Standard | 32 | 128 GB | €1317/mo | `1335.2917` | `1268.5271` |
| `rdb.standard.48CPU-192GB` | Standard | 48 | 192 GB | €2017/mo | `2045.0139` | `1942.7632` |
| `rdb.standard.64CPU-256GB` | Standard | 64 | 256 GB | €2669/mo | `2706.0694` | `2570.7659` |
| `rdb.standard.80CPU-320GB` | Standard | 80 | 320 GB | €3429/mo | `3476.625` | `3302.7938` |
| `rdb.memory.2CPU-16GB` | High Memory | 2 | 16 GB | €126/mo | `127.75` | `121.3625` |
| `rdb.memory.4CPU-32GB` | High Memory | 4 | 32 GB | €283/mo | `286.9306` | `272.5841` |
| `rdb.memory.8CPU-64GB` | High Memory | 8 | 64 GB | €593/mo | `601.2361` | `571.1743` |
| `rdb.memory.12CPU-96GB` | High Memory | 12 | 96 GB | €883/mo | `895.2639` | `850.5007` |
| `rdb.memory.16CPU-128GB` | High Memory | 16 | 128 GB | €1173/mo | `1189.2917` | `1129.8271` |
| `rdb.memory.24CPU-192GB` | High Memory | 24 | 192 GB | €1800/mo | `1825.00` | `1733.75` |
| `rdb.memory.32CPU-256GB` | High Memory | 32 | 256 GB | €2380/mo | `2413.0556` | `2292.4028` |
| `rdb.memory.48CPU-384GB` | High Memory | 48 | 384 GB | €3655/mo | `3705.7639` | `3520.4757` |
| `rdb.memory.64CPU-512GB` | High Memory | 64 | 512 GB | €4855/mo | `4922.4306` | `4676.3091` |

## Left out on purpose

- **No `serverless.function` or `messaging.queue` offering.** The product sections of the documentation
  (from <https://upcloud.com/docs/llms.txt> and <https://upcloud.com/docs/sitemap.xml>) are block storage,
  CDN, cloud servers, container registry, file storage, GPU servers, managed Kubernetes, managed load
  balancer, managed MySQL, managed object storage, managed OpenSearch, managed PostgreSQL, managed Valkey,
  NAT gateway, networking, private cloud and VPN gateway. None is a function-as-a-service or a message
  queue product. So a resource of either category gets fit none on UpCloud.
- **No `compute.baremetal` offering (issue #187, ADR 0023).** The same product list has no bare-metal
  server: Cloud Servers and GPU Servers are virtual machines, and Private Cloud runs Cloud Servers on
  dedicated hosts, still under a hypervisor. So a bare-metal server gets fit none on UpCloud, and Cloud
  Servers serve `compute.vm` only.
- **Managed Valkey is not a queue.** `llms.txt` describes it as "commonly used as a database, cache,
  streaming engine, or message broker". It is an in-memory data store, a category the taxonomy does not
  have, not a queue product with acknowledgement or a dead-letter queue, so it does not serve
  `messaging.queue`.
- **Other products not in the snapshot:** CDN, Container Registry, File Storage, GPU Servers (Cloud
  Servers already serve `compute.vm`), Managed Load Balancer, Managed OpenSearch, NAT Gateway, Private
  Cloud and VPN Gateway: no matching taxonomy category in scope.
- **Managed Kubernetes `autoscaling` (beta, user-installed).** The autoscaling page
  (<https://upcloud.com/docs/products/managed-kubernetes/autoscaling/>) describes Cluster Autoscaler as
  "an additional component" that "has to be configured & installed"; the guide
  (<https://upcloud.com/docs/guides/cluster-autoscaler/>) has the user apply manifests from a branch of
  UpCloud's autoscaler fork and run it on worker nodes. The only status statement is the changelog entry
  "Cluster Autoscaler beta released" (2023-09-26,
  <https://upcloud.com/docs/changelog/2023-09-26-cluster-autoscaler-beta/>), with no later general
  availability entry. It is neither generally available nor managed, so no tier lists it, and a
  `kubernetes.managed` resource that needs autoscaling is partial on UpCloud. OVHcloud and Scaleway both
  list it.
- **Block Storage `node-high-availability` (target rule).** The storage system page
  (<https://upcloud.com/docs/products/block-storage/storage-system/>) says data "is stored in clusters of
  two separate storage backends to ensure high performance and uninterrupted availability", with
  "tolerance against failures through the use of storage clusters". That describes copies of the data,
  but no page names a failure that an attached device keeps serving through with no manual action, so the
  capability is not stated in full, as for OVHcloud Block Storage. Zone level is not claimed either: a
  device is "bound to the location" it is created in (Cloud Servers storage page), and backups are stored
  "in the same data centre as the origin Cloud Server" (backups page).
- **`encryption-at-rest` on Cloud Servers.** A server's storage devices, the operating system disk
  included, can be created encrypted ("Any block storage device can be encrypted, including the storage
  device holding the Cloud Server's operating system", Block Storage encryption page). That is a feature
  of the storage device, which *Block Storage Encrypted* carries, not of the server, as for the OVHcloud
  and Scaleway Instances offerings. Leaving it off only understates the server.
- **`encryption-at-rest` on Managed Kubernetes.** The encrypted clusters page
  (<https://upcloud.com/docs/products/managed-kubernetes/encrypted-clusters/>) describes encryption of
  node storage and persistent volumes, chosen per cluster or node group and "currently only available
  over Terraform and API". It is an option of the storage the cluster uses, not of the managed control
  plane; neither OVHcloud nor Scaleway lists it on Kubernetes, and leaving it off only understates.
- **Cloud Servers `node-high-availability` and `zone-high-availability`.** The system architecture page
  (<https://upcloud.com/docs/products/cloud-servers/system-architecture/>) describes "Automatic transfer of
  Cloud Servers to healthy hosts" after compute host failures: the server is moved and restarted, not
  kept serving through the loss, as for the other targets' virtual machines. Live migrations are "performed
  only within the same data centre" (<https://upcloud.com/docs/products/cloud-servers/live-migration/>). No Cloud Server autoscaling product is documented either.
- **`zone-high-availability` on every offering.** Each zone is one data centre, and no product documents a
  service that keeps serving through the loss of its zone. For Managed PostgreSQL and MySQL, the backups
  pages say backups are kept "in another location in the same region", available "even in the unlikely
  event that the database's whole location goes down"; that is backup durability, and recovery is a
  restore into a new service, not failover without manual action. Managed Kubernetes documents no
  cluster spanning zones, and Managed Object Storage stores each region's data in one location.
- **Managed Object Storage `node-high-availability`.** No page read describes replication, durability or
  failover for the service. Object Lock and Bucket Replication are listed as unsupported S3 APIs (S3
  compatibility page).
- **`snapshots` on Managed PostgreSQL and MySQL.** No user snapshot feature is documented: backups are
  automated and restore is point-in-time into a new service. OVHcloud's databases do not list it either.
- **Features the vocabulary has no capability for**, left out: customer-managed keys (Object Storage
  server-side encryption with customer keys; LUKS inside a server), off-site database backups in another
  location, Kubernetes private node groups, node anti-affinity (documented as "best effort",
  <https://upcloud.com/docs/products/managed-kubernetes/data-plane/>), S3
  lifecycle policies, and PostgreSQL connection pooling.

Prices and plans left out (PRD #190 milestone 5, read 2026-11-13 from <https://upcloud.com/pricing/>):

- **Managed Kubernetes worker-node storage beyond a plan's included storage** (*Managed Kubernetes*,
  *Managed Kubernetes Production*). The Managed Kubernetes billing page
  (<https://upcloud.com/docs/products/managed-kubernetes/billing/>, read 2026-11-13) refers only to Cloud
  Server pricing, and the pricing page (<https://upcloud.com/pricing/>, read 2026-11-13) gives no per-GiB
  node storage price. Its Cloud Native node-group example, 8 nodes of 2 cores, 8 GB and 20 GB at
  "€252.88/mo", is €31.61 a node against the €24.00 `CLOUDNATIVE-2xCPU-8GB` Cloud Server plan, leaving €7.61 for 20 GB:
  neither MaxIOPS (€4.40) nor Standard (€1.70), with or without an IPv4 address (€3.47). The *Cloud
  Servers* storage price is not assumed to apply, so none is recorded, and a sized node needing more disk
  than its plan includes is "not estimated" (PRD #190 milestone 6).
- **Cloud Native plans as Managed Kubernetes worker nodes** (*Managed Kubernetes*, *Managed Kubernetes
  Production*; the 42 `CLOUDNATIVE-*` plans, which *Cloud Servers* keeps). The billing page lets a node
  use them, but they include no storage ("Cloud Native and GPU plans don't include storage", Block Storage
  billing page), every node needs a disk, and the Kubernetes offerings have no storage price (previous
  entry). Plan matching picks the cheapest plan that fits, so a Cloud Native plan would win many sizes and
  price a node without its disk: the page's own example prices a `CLOUDNATIVE-2xCPU-8GB` node with 20 GB at
  €31.61, not the plan's €24. Left out, each Kubernetes offering has 31 plans, all with included storage.
- **Managed Database Developer plans** (`rdb.development.1CPU-1GB`, `rdb.development.1CPU-2GB`,
  `rdb.development.1CPU-4GB`: "€9/mo", "€14/mo", "€24/mo", read 2026-11-13). Their storage is a different
  tier at a different price ("Standard – €0.04/GB/mo" against "MaxIOPS – €0.10/GB/mo" for Standard and
  High Memory), and ADR 0024 allows one storage price per offering and zone, so a Developer service's
  storage would be priced at the wrong tier. They are left out, so a database smaller than 2 vCPU and
  8 GiB is matched to `rdb.standard.2CPU-8GB`, and its estimate is high.
- **Block Storage Standard and Archive tiers** ("€0.085/GB/mo"; "€0.056/GB/mo", "€0.100/GB/mo" in
  Helsinki) and custom images ("€0.330/GB/mo"): one storage price per zone (*Block Storage*). Standard and
  Archive prices also change on 2026-11-16 (*Prices*).
- **GPU Server plans** as Cloud Servers or Kubernetes nodes: priced per hour ("€0.58/h") and estimated
  "based on a 30-day month", and specialised hardware rather than the general-purpose sizes a VM is
  matched against.
- **Discontinued Cloud Server plans** (the page's *Previous plans*: Developer, General Purpose, High CPU,
  High Memory), "no longer available for deployment", with prices that change on 2026-11-16.
- **Extra costs a plan does not carry:** Extended 31-day database backups, Simple Backups and on-demand
  backups, floating and additional IPv4 addresses ("€3.47/mo"), Windows Server licences, and public IPv4
  addresses on Cloud Native plans. Network egress is free ("zero-cost egress").
