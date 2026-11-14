# OVHcloud catalog snapshot: sources

`catalog.json` is a hand-compiled, dated snapshot of what OVHcloud **Public Cloud** offers in its EU regions,
in taxonomy terms. This file records where each fact comes from, so the snapshot can be checked and refreshed.

## How it was compiled

- **Retrieved on:** 2026-11-13, the date the plans and prices were read (PRD #190 milestone 3, ADR 0024).
  The extra-storage prices were read on 2026-11-14 (issue #219, below), and the snapshot's `retrievedOn`
  stays 2026-11-13 until the tests that pin it move with it. The snapshot's `retrievedOn` and `sourceURL`
  name the prices, the facts most likely to change. The
  regions come from the regions page, read on 2026-11-10, and the capabilities were last re-verified on
  2026-11-11 (below). Every page cited below was fetched on one of those four dates, without credentials
  and without calling any OVHcloud API. A page fetched or saved to read the data from, such as a browser
  copy of a price page, is kept in a scratch directory outside the repository, treated as data, and never
  committed (`TestNoTrackedSavedPages` in `test/lint`, issue #212).
- **`sourceURL`** is OVHcloud's Public Cloud price list in its Ireland edition,
  <https://www.ovhcloud.com/en-ie/public-cloud/prices/>: the English-language edition that shows prices in
  euros, excluding VAT. Every plan and storage price comes from it (*Prices*).
- **Regions page.** OVHcloud's Public Cloud regions and availability page,
  <https://www.ovhcloud.com/en/public-cloud/regions-availability/>, was the snapshot's `sourceURL` until
  the prices were added. It is the one page that lists, per product, the locations where it is offered,
  and it is where the declared regions and most offering regions come from. On it, a tick marks a product
  as available in a location, and a separate "Β" mark (legend "Beta version") marks a beta. No cell of
  the EMEA tables carried the beta mark on 2026-11-10, so every tick relied on below is a generally
  available product.
- **Product pages** on `www.ovhcloud.com` and **documentation** on `docs.ovhcloud.com` give the
  capabilities. They were read live on 2026-11-10 or, for the re-verification below, 2026-11-11; the
  prices were read on 2026-11-13.
- **Documentation source.** `docs.ovhcloud.com` is built from `ovh/ovhcloud-docs`. Its files were read at
  commit
  [`2df4cc152902b880880b77955ab9e52de4a2c2ff`](https://github.com/ovh/ovhcloud-docs/tree/2df4cc152902b880880b77955ab9e52de4a2c2ff)
  (branch `develop`, 2026-11-10) to find the relevant pages, and every fact used was then checked on the
  published page cited. The repository named in PRD #11, `ovh/docs`, is archived by a notice in its README
  at commit
  [`989d55b1b8f7ca0d8c39280cb469e4d40513f18d`](https://github.com/ovh/docs/tree/989d55b1b8f7ca0d8c39280cb469e4d40513f18d)
  (2026-09-01), which says the repository is no longer maintained and the documentation has moved to
  `ovh/ovhcloud-docs` and `docs.ovhcloud.com`. GitHub's archive flag is not set on the repository. No fact
  was taken from `ovh/docs`.
- **Roadmap.** OVHcloud's public roadmap, `ovh/public-cloud-roadmap`, was read for the state of a few
  features: the Block Storage encryption rollouts, the general availability of the Managed Kubernetes
  Standard plan and etcd encryption at rest. Each issue is linked where it is used and was read on the
  retrieval date.
- **Re-verified on 2026-11-11 for PRD #14 (capability granularity).** Every high-availability and
  read-replica claim was checked again, and replaced by its scoped capability (`node-high-availability`
  or `zone-high-availability`) or dropped, under the target rule and the tier-variant rule (*Rules
  applied*). `ovh/ovhcloud-docs` `develop` was still at the commit cited below, and the pages cited were
  read from it; the MySQL and PostgreSQL product pages were read live. Because it changed facts,
  `retrievedOn` in `catalog.json` was its date, 2026-11-11, until the prices were added. No region
  changed; the regions page was read on 2026-11-10.
- **Prices read on 2026-11-13 for PRD #190 milestone 3.** The plans and prices, with the pages under
  *Prices*. No offering, capability or region changed.
- **Extra-storage prices read on 2026-11-14 for issue #219.** The prices page was read again, with the
  product pages and documentation cited in each section, to price the storage beyond what a plan includes.
  *Instances*, *Managed Kubernetes Service* and *Managed Kubernetes Service 3-AZ* gained a storage price;
  *Metal Instances*, *Block Storage LUKS* and *Block Storage LUKS Zonal* say why they have none. The same
  read re-checked every plan price of those four compute offerings (82 prices): each still equals the
  page's Linux hourly price × 730, so no plan price or included storage changed. No offering, capability
  or region changed.

## Prices

Every plan and storage price in `catalog.json` is a monthly list price in EUR, excluding VAT (ADR 0024),
read on 2026-11-13. "The prices page" below is <https://www.ovhcloud.com/en-ie/public-cloud/prices/>.

- **Which edition.** The page exists in one edition per OVHcloud market. The `/en/` edition
  (<https://www.ovhcloud.com/en/public-cloud/prices/>) is the "World [$]" market and shows US dollars, so
  it was not used. The Ireland edition (`/en-ie/`, "Ireland [€]") shows euros, and its column headings say
  "excl. VAT". Its figures equal the France edition's ("HT", before tax: `b3-8` is €0.0512 an hour on
  both); the Germany edition shows them with VAT added (€0.060928 for the same row).
- **Which region.** The page has a *Locations* filter, "All regions" by default. With a location chosen
  it shows that location's prices. Every price below was read for each of the seven declared regions
  (the product-level names under *Declared EU regions*, such as `EU-WEST-PAR` for `PAR`). Where the
  prices of a plan differ by region, the snapshot has one price per group of regions; where they are the
  same in every region of the offering, one price with an empty `regions` list. A plan the page does not
  offer in some region of its offering has no price for that region, and an estimate there says so (ADR
  0024); it never borrows another region's price.
- **Hourly to monthly.** The page gives an hourly price for every plan and storage class, and shows a
  monthly figure beside it. The snapshot always converts the **hourly price × 730**, rounded **half up to
  4 fractional digits** (ADR 0024), so that every price follows one rule. That is also OVHcloud's own rule:
  the instance tables say "The monthly price is estimated based on 730 hours per month for Gen 3
  instances (B3, C3, R3)", the storage tables say billing is "per GB/hour, based on an average of 730
  hours in a month", and the databases pricing guide
  (<https://docs.ovhcloud.com/en/guides/public-cloud/databases/pricing>) calls the hourly price the
  "source of truth" and gives "Monthly price = Hourly price × 730 hours". No plan price needed rounding:
  each hourly price × 730, the per-node database prices included, has at most four fractional digits
  (`Discovery B3-4`: 0.10338 × 730 = `75.4674`); the storage prices that did are worked out in their
  sections.
- **Monthly figures not used.** For B3, C3 and R3 instances the page's monthly column shows the price
  with a 12-month Savings Plan (`b3-8`: "12 months plan €31.77"), a commitment discount, not a list
  price. For B2 instances and Metal Instances it shows a monthly figure below hourly × 730 (`b2-7`:
  €25.17 against €51.757; `bm-s1`: €170 against `365.00`), the price of OVHcloud's monthly billing
  option, which commits the instance for the month (the Kubernetes architecture page lists "Billing:
  Hourly or monthly (for gen2 flavors)"). Both are left out: the snapshot holds no-commitment list prices.
  A user on monthly billing or a Savings Plan pays less than the estimate.
- **Linux prices.** Instance rows are read for Linux. A Windows instance adds a licence per vCore
  ("€0.0347 * excl. VAT/vCore/hour"), which is not recorded.
- **Units.** The page gives memory in "GB" and volume prices per "GB"; the snapshot records them as GiB,
  unconverted, as OVHcloud's own tables and documentation mix the two for the same sizes (the database
  tables give memory in "GB" and storage in "GiB"; Object Storage says "1 TiB = 1024 GiB").
- **Managed databases.** A plan is one node of a database service: vCPU and memory are those of one
  node, as the page lists them, and **the price is per node**, the page's per-node hourly price × 730
  (the page's data carries a "per-node" price beside the "total" for every flavour). An estimate
  multiplies the plan price by the resource's count, which for a database is its nodes (ADR 0025). Each
  tier runs a fixed number of nodes, as the page's tables give them: Essential/Discovery 1,
  Business/Production 2 (in the 3-AZ regions too), Enterprise/Advanced 3. The page's "Total price" is the
  per-node price × that count in every row read. A plan's included storage is the lower bound of the
  page's "Usable storage" range, which is the service's data, not per node. The storage price is the
  page's "Price per GB of additional storage", which is per node ("/hour/node"); the databases pricing
  guide bills it as "100 GB × 30 days × 24 hours × 2 nodes" for a two-node service, since every node
  holds the data. So the snapshot's storage price is **per GiB of the service's data, for the whole
  service at its tier's node count**: that price × the tier's nodes × 730. An inventory's storage size
  for a database is its data size, so the storage price is not multiplied by the count again. The page's
  own data behind each storage line also carries a monthly figure that the page does not display (€0.25,
  €0.37 and €0.44 a month per node) and that is not the displayed hourly price × 730; the displayed
  hourly price, which the guide calls the source of truth, is the one used.
- **Managed Kubernetes.** The page prices only the control plane; worker nodes are Public Cloud
  instances "billed at standard price". A Kubernetes plan is therefore a worker node flavour at its
  *Instances* price, with the flavours the Kubernetes regions page offers. The Free plan's control plane
  is "Included". The Standard plan's control plane costs €0.09 an hour per cluster, which a plan cannot
  hold (a plan is multiplied by the node count), so it is recorded as the offering's fixed fee per
  cluster (`fixedFees`, ADR 0026): 0.09 × 730 = `65.70` a month where the Standard plan is sold, and
  `0.00` where the control plane is "Included".
- **Plans chosen.** Instances: the General Purpose (B3, B2), Compute Optimised (C3) and Memory Optimised
  (R3) ranges, the sizes a general workload is matched against. Metal Instances, MySQL and PostgreSQL:
  every flavour the page lists. The plans left out are listed under *Left out on purpose*.

## Licences of the sources

- **`www.ovhcloud.com`** (product and regions pages): © OVH SAS, under the site's legal notices. No open
  licence is granted.
- **`docs.ovhcloud.com`** and its source **`ovh/ovhcloud-docs`**: no licence is stated. The repository has no
  licence file at the commit above, its README states none, and GitHub detects none. So the content is
  under OVH SAS's copyright with all rights reserved.
- **`ovh/docs`** (archived by its README, not used for facts) is licensed CC BY-NC-SA 4.0 (Creative
  Commons Attribution-NonCommercial-ShareAlike 4.0 International), as its `LICENSE` file states at the
  commit above. Its README (`readme.md`) says the content "remains under the terms provided in the
  LICENSE file".
- **`ovh/public-cloud-roadmap`** issues: no licence is stated. Only an issue's open or closed state, its
  dates and a brief quotation from a comment are used.
- **Rule for this file and `catalog.json`:** take facts only (which product exists, in which locations, with
  which feature) and brief quotations, each in quotation marks and attributed to the page it comes from.
  Paraphrase everything else. Never copy documentation or web text in bulk, and keep it out of
  `catalog.json`, which holds only names, taxonomy terms, region codes, and plan sizes and prices
  recorded as numbers (facts), with no copied prose. A refresh follows the same rule.

## Rules applied

- **Scope:** OVHcloud Public Cloud only (PRD #11). Bare Metal dedicated servers, Hosted Private Cloud, Web
  Cloud, Local Zones and the SecNumCloud platform are out of scope. So `compute.baremetal` is served by the
  Public Cloud's Metal Instances only.
- **Regions:** one code per location, the code the regions page prints under each location (PRD #11,
  *one region name per location*). Only locations in EU member states are declared. Products name their
  regions differently; *Declared EU regions* below records which product-level names each code covers.
  An offering lists a region when the regions page ticks the product there, unless a product page narrows
  it, which is noted.
- **1-AZ and 3-AZ regions:** Paris and Milan are 3-AZ regions (three availability zones); the others are
  1-AZ regions. OVHcloud sets a product's deployment mode by the region: Object Storage, Managed Kubernetes
  and Public Cloud Databases deployed in Paris or Milan are 3-AZ deployments. Where a product reaches
  zone-level high availability only in the 3-AZ regions, the snapshot lists a variant whose name has
  "3-AZ" in `MIL` and `PAR` only, beside the offering in the 1-AZ regions (PRD #11, *a capability limited
  to some regions makes a separate offering*). For the same reason, a capability missing from some of a product's
  regions makes a separate offering (*Block Storage LUKS* in the 1-AZ regions, and *Block Storage LUKS
  Zonal* in the 3-AZ regions, which have no volume snapshots).
- **Capabilities:** only from the taxonomy vocabulary, each as the taxonomy defines it, and only when an
  OVHcloud page describes the feature as available. A capability that needs a particular plan or class is
  marked **Needs:** on its line. A feature with no vocabulary entry is listed under *Left out on purpose*.
- **Target rule** (ADR 0011): an offering lists a capability only where the documentation states it
  **in full**. An offering that meets only part of a capability's definition does not list it, and
  *Left out on purpose* records the omission with its reason.
- **Tier-variant rule** (ADR 0011): all of an offering's capabilities hold in **one** tier. The base
  offering is the product's default tier. A tier that adds a capability is a variant named
  `<base offering> <tier>`, with its own section below. A tier that only lacks capabilities gets no
  offering. Here the base tiers are the Essential/Discovery plan (Managed MySQL and Managed
  PostgreSQL), the Standard class (Object Storage 3-AZ) and the Standard plan (Managed Kubernetes
  Service 3-AZ, the only plan in Paris and Milan). The variants are *Managed MySQL Multi-node* and
  *Managed PostgreSQL Multi-node* (the Business/Production plan), *Managed MySQL Multi-node Advanced*
  and *Managed PostgreSQL Multi-node Advanced* (the Enterprise/Advanced plan, which adds read
  replicas), each with its *3-AZ* form, and *Block Storage 3-AZ Regional Classic*, named after the
  volume class. A **Needs:** line names the tier its offering is named after,
  or the product's default tier. **Naming order:** a variant is `<base offering> <tier>`, and a
  region-group suffix such as `3-AZ` comes after the tier (*Managed PostgreSQL Multi-node 3-AZ*). The
  1-AZ variant's name is then a prefix of the 3-AZ one, so it wins ties by name, and a resource that
  needs only a capability both variants share is pointed at the variant offered in the most regions.
  This departs from PRD #11's `<product> 3-AZ` pattern only where a tier variant also has a 3-AZ form;
  *Block Storage 3-AZ Regional Classic* keeps it, as its class exists only in the 3-AZ regions.
- **Scope:** high availability is listed at the level the documentation states in full.
  `node-high-availability` is listed when the offering keeps serving through the loss of a node, server
  or host with no manual failover; `zone-high-availability` when it keeps serving through the loss of a
  whole availability zone or data centre. A **Scope:** line on the capability says which level, and why
  the other is not claimed. **Node-level bar:** node level is claimed only when a page names the
  failure the service keeps serving through (the loss of a server, rack, disk or host); a bare "high
  availability", with no failure named, is not enough. The catalog adds node level to every offering that lists zone level (ADR
  0011), so each zone-level section also names `node-high-availability`.
- **Copies of data are not availability.** Redundant copies of data inside one zone (replication or
  erasure coding) are durability, and are not counted as `node-high-availability` unless a page states
  that the service keeps **serving** through the loss of a server. OVHcloud's Object Storage deployment
  modes page states that for the 1-AZ setup, so *Object Storage* lists node level. The Block Storage
  pages state only copies of data for the zonal classes, so those offerings list neither level. The
  Block Storage product page also uses the phrase "high availability" for every class ("Thanks to its
  high availability, you can choose between One-Zone, Multi-Zone, or Local Zone configuration"). That
  use is not counted: for the one-zone classes the same page names only data "replicated in the same
  datacentre", and it says high availability "is guaranteed in our 3-AZ regions" with the Regional
  Classic Volume class.

## Declared EU regions

`GRA` (Gravelines, France), `LIM` (Frankfurt/Limburg, Germany), `MIL` (Milan, Italy), `PAR` (Paris, France),
`RBX` (Roubaix, France), `SBG` (Strasbourg, France), `WAW` (Warsaw, Poland).

The regions page's EMEA tables list eight locations on 2026-11-10: Strasbourg `SBG`, Gravelines `GRA`,
Roubaix `RBX`, "3-AZ Paris" `PAR`, "3-AZ Milan" `MIL`, London `ERI`, Frankfurt `LIM` and Warsaw `WAW`, plus a
"Local Zones" column with no location named. The column for `LIM` is headed "Frankfurt" in most tables and
"Limburg" in the identity table. London (`ERI`, Erith) is in the United Kingdom, which is not an EU member
state, so it is not declared. The North America (Montreal `BHS`, Toronto, Seattle, Washington DC) and Asia
Pacific (Singapore, Sydney, Mumbai) tabs list no EU location. Local Zones are excluded (PRD #11).

Product-level names each code covers:

| Code | Compute (OpenStack regions) | Object Storage endpoint | Databases | Managed Kubernetes |
|---|---|---|---|---|
| `GRA` | GRA1, GRA3, GRA5, GRA7, GRA9, GRA11 | `gra` | GRA | GRA5, GRA7, GRA9, GRA11 |
| `LIM` | DE1 | `de` | DE | DE1 |
| `MIL` | EU-SOUTH-MIL | `eu-south-mil` | EU-SOUTH-MIL | EU-SOUTH-MIL |
| `PAR` | EU-WEST-PAR | `eu-west-par` | EU-WEST-PAR | EU-WEST-PAR |
| `RBX` | RBX-A | `rbx` | RBX | EU-WEST-RBX |
| `SBG` | SBG5, SBG7 | `sbg` | SBG | SBG5 |
| `WAW` | WAW1 | `waw` | WAW | WAW1 |

Sources: the regions page footnotes (GRA and SBG OpenStack regions); the Object Storage endpoints page
<https://docs.ovhcloud.com/en/guides/storage-and-backup/object-storage/s3-location>; the database backups page
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/backups> (service locations); the Managed
Kubernetes regions page
<https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-kubernetes/datacenters-nodes-storage-flavors>;
the Block Storage encryption page
<https://docs.ovhcloud.com/en/guides/public-cloud/compute/encrypting-a-block-storage-volume-with-luks>
(DE1, WAW1, RBX-A, EU-WEST-PAR and EU-SOUTH-MIL as OpenStack regions).

## Default region

`GRA` (Gravelines, France) is the snapshot's `defaultRegion`: the region a cost estimate uses when the user names
none (PRD #190, ADR 0024). Chosen provisionally in milestone 2, it was **confirmed in milestone 3**, when the
prices were added, because it hosts the most offerings: 12 of the 20 (Instances, Block Storage, Block Storage
LUKS, Managed Kubernetes Service, the six 1-AZ database offerings, Metal Instances and Object Storage). `LIM`
and `SBG` host the same 12; `RBX` and `WAW` host 11 (no Metal Instances in `RBX`, no Block Storage LUKS in
`WAW`), and `PAR` and `MIL` 9 each (Instances and the eight 3-AZ offerings). Of the three tied regions, `GRA`
and `LIM` price every plan of their offerings, while `SBG` has no price for the B3, C3 and R3 Kubernetes node
flavours (*Managed Kubernetes Service*). `GRA` is kept over `LIM`, which ties it on both counts, because it was
already the default and is the first of the declared codes, kept in code order: no price makes either one a
better default. The eight offerings available only in `MIL` and `PAR` (the seven 3-AZ offerings and Block
Storage LUKS Zonal) are still estimated when the user names `--region PAR` or `MIL`.

The choice says nothing about which region is cheapest or best. Prices are the same in every 1-AZ region
where a plan is offered; the 3-AZ regions charge more for databases (*Prices*).

## Offerings

### Block Storage

Category `storage.block`: Block Storage volumes in the 1-AZ regions (the Classic, High Speed and High Speed
Gen2 classes, unencrypted). Pages: <https://www.ovhcloud.com/en/public-cloud/block-storage/>,
<https://docs.ovhcloud.com/en/guides/storage-and-backup/block-storage/block-storage-the-right-storage-class>,
<https://docs.ovhcloud.com/en/guides/public-cloud/compute/storage-create-volume-snapshot>.

- `snapshots`: volume snapshots, "a recovery point stored within the same storage cluster as the original
  volume", taken without detaching the volume (volume snapshot page). The regions page ticks "Volume
  Snapshot" in all five regions below.
- No `node-high-availability` and no `zone-high-availability`: the classes in 1-AZ regions replicate
  data within one zone (storage class page, "Zonal" replication). That is a copy of data, not a stated
  continuity of an attached volume, so neither level is claimed (*Rules applied*).
- Regions `GRA`, `LIM`, `RBX`, `SBG`, `WAW`: the regions page ticks the three Block Storage classes and
  "Volume Snapshot" in each. Volumes in `PAR` and `MIL` are under *Block Storage 3-AZ Regional Classic*
  and *Block Storage LUKS Zonal*.

- **Storage price** (prices page, *Block Storage*, read 2026-11-13): the Classic volume row, €0.000059 per GB and hour, × 730 = 0.04307, rounded half up to `0.0431` per GiB a month, one price in every region. The offering also covers High Speed and High Speed Gen2 volumes (€0.000119, × 730 = 0.08687, would be 0.0869); an offering has one storage price per region, and Classic, the base class, is the one recorded.

### Block Storage 3-AZ Regional Classic

Category `storage.block`: the Regional Classic Volume class, offered only in 3-AZ regions. It is a tier
variant (ADR 0011), named after the class, because Paris and Milan also offer zonal High Speed Gen2
volumes, and no page names Regional Classic as the default class there. Pages:
<https://www.ovhcloud.com/en/public-cloud/block-storage/>,
<https://docs.ovhcloud.com/en/guides/storage-and-backup/block-storage/block-storage-the-right-storage-class>,
<https://docs.ovhcloud.com/en/guides/public-cloud/compute/storage-classic-multi-attach-3az>.

- `zone-high-availability`. **Needs:** the Regional Classic Volume class (volume type
  `classic-multiattach`, "Classic 3AZ" in the console), the class this offering is named after.
  **Scope: zone-level.** The zone-level evidence is continued service, stated on two pages. The storage
  class page says: "To ensure service continuity even in the event of an AZ failure, this class also
  supports Multi-Attach, allowing multiple instances located in different availability zones to
  simultaneously attach and use the same volume." The reference architecture page
  (<https://docs.ovhcloud.com/en/guides/public-cloud/cross-functional/deployments-modes-reference-architecture>,
  row "Block storage Classic Multi-Zone") says that in an AZ failure "Block Storage data remains
  available without impact or downtime, provided the conditions of the multi-attached resilient
  architecture are met". The condition is that the volume is attached from instances in more than one
  zone (up to 16 instances, multi-attach page); a volume attached from one zone only is not stated to
  keep serving. Supporting evidence only, since copies of data are not availability (*Rules applied*):
  the storage class page says the class "provides high availability by automatically replicating data
  across three availability zones", with a 99.99% SLA and "Multi-zone" replication, and the product page
  says high availability is guaranteed in 3-AZ regions with this class. The zonal High Speed volumes also offered in these regions do not have this
  capability, and get no offering of their own (*Block Storage LUKS Zonal* covers the encrypted ones).
- `node-high-availability`: implied by zone level (ADR 0011); a volume replicated across three zones
  also survives the loss of one storage server.
- No `snapshots`: the regions page does not tick "Volume Snapshot" or "Volume Backup" in Paris or Milan.
- No `encryption-at-rest`: multi-attach volumes cannot be encrypted (multi-attach page, *Limitations*; storage
  class page: Regional Classic does not support LUKS). Encrypted volumes in these regions are zonal, under
  *Block Storage LUKS Zonal*. See *Left out on purpose*.
- Regions `MIL`, `PAR`: the 3-AZ regions; the product page offers Regional Classic Volume "in 3-AZ regions
  only", and the regions page ticks the Block Storage classes in both.

- **Storage price** (prices page, *Block Storage*, read 2026-11-13): the Classic volume row, which the page prices the same with its *Locations* filter on Paris or Milan: €0.000059 per GB and hour, × 730 = 0.04307, rounded half up to `0.0431` per GiB a month. The page has no separate Regional Classic row.

### Block Storage LUKS

Category `storage.block`: the encrypted variants of the Block Storage classes (volume types
`classic-luks`, `high-speed-luks`, `high-speed-gen2-luks`), listed apart because they are not offered in
every 1-AZ region. Pages: <https://docs.ovhcloud.com/en/guides/public-cloud/compute/encrypting-a-block-storage-volume-with-luks>,
<https://docs.ovhcloud.com/en/guides/storage-and-backup/block-storage/block-storage-the-right-storage-class>,
<https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-kubernetes/datacenters-nodes-storage-flavors>,
<https://docs.ovhcloud.com/en/guides/public-cloud/compute/storage-create-volume-snapshot>.

- `encryption-at-rest`: volumes "encrypted at rest using LUKS", with an OVHcloud Managed Key by default or
  a Customer Managed Key in OKMS (encryption page). The storage class page says each class has an encrypted
  version "depending on the region".
- `snapshots`: volume snapshots as for *Block Storage*. The Managed Kubernetes volume snapshot page
  (<https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-kubernetes/backup-restore-pv-volume-snapshot>)
  says "Volume snapshots work with all storage classes, including LUKS encrypted volumes".
- Regions `GRA`, `LIM`, `RBX`, `SBG`: the Managed Kubernetes regions page lists the regions where LUKS
  encrypted storage "is currently available": RBX, SBG, GRA, EU-WEST-PAR, DE1 and EU-SOUTH-MIL in the EU.
  OVHcloud's roadmap closed the rollouts for GRA and DE1 on 2026-02-19
  (<https://github.com/ovh/public-cloud-roadmap/issues/951>,
  <https://github.com/ovh/public-cloud-roadmap/issues/952>). Warsaw is not listed, and its rollout is still
  open (<https://github.com/ovh/public-cloud-roadmap/issues/954>). The same page lists EU-WEST-PAR and
  EU-SOUTH-MIL, but volume snapshots are not offered there, so encrypted volumes in Paris and Milan make an
  offering of their own, *Block Storage LUKS Zonal*.

- **No storage price.** Re-examined on 2026-11-14 (issue #219): the prices page's *Block Storage* table and the price data behind it list the Classic, High Speed and High Speed Gen2 volumes (`volume.classic`, `volume.high-speed`, `volume.high-speed-gen2`) and no encrypted (`-luks`) volume type. The storage class page (<https://docs.ovhcloud.com/en/guides/storage-and-backup/block-storage/block-storage-the-right-storage-class>) says each class "is also available in an encrypted version (LUKS), depending on the region", "leveraging the same performance and features as standard volumes", but gives no price, and the encryption page (<https://docs.ovhcloud.com/en/guides/public-cloud/compute/encrypting-a-block-storage-volume-with-luks>) says nothing about billing. No page read states that an encrypted volume costs what its unencrypted class costs, so no price is recorded rather than the unencrypted class's (*Left out on purpose*).

### Block Storage LUKS Zonal

Category `storage.block`: encrypted zonal volumes in the 3-AZ regions, of the High Speed Gen2 class (volume
type `high-speed-gen2-luks`). Each volume sits in one availability zone of the region. Listed apart from
*Block Storage LUKS* because volume snapshots are not offered in these regions. Pages:
<https://docs.ovhcloud.com/en/guides/public-cloud/compute/encrypting-a-block-storage-volume-with-luks>,
<https://docs.ovhcloud.com/en/guides/storage-and-backup/block-storage/block-storage-the-right-storage-class>,
<https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-kubernetes/datacenters-nodes-storage-flavors>,
<https://www.ovhcloud.com/en/public-cloud/regions-availability/>.

- `encryption-at-rest`: LUKS encryption at rest, as for *Block Storage LUKS* (encryption page). The
  encryption page says "classic-luks is not available in 3-AZ regions" and to "use high-speed-gen2-luks
  there"; its example creates a `high-speed-gen2-luks` volume in region `EU-WEST-PAR`, availability zone
  `eu-west-par-a`. The storage class page offers High Speed Volume Gen2 in "3-AZ, 1-AZ, Local Zones".
- No `snapshots`: the regions page ticks neither "Volume Snapshot" nor "Volume Backup" in Paris or Milan,
  as for *Block Storage 3-AZ Regional Classic*.
- No `node-high-availability` and no `zone-high-availability`: the storage class page gives High Speed
  Volume Gen2 "Zonal" replication, so a volume stays in its zone; copies within one zone are durability
  under the rules above.
- Regions `MIL`, `PAR`: the Managed Kubernetes regions page lists EU-WEST-PAR and EU-SOUTH-MIL among the
  regions where LUKS encrypted storage "is currently available", and the regions page ticks Block Storage
  High Speed Gen 2 in "3-AZ Paris" and "3-AZ Milan".
- The unencrypted zonal volumes in these regions have no offering of their own: they have no capability
  that this offering lacks.

- **No storage price**, as for *Block Storage LUKS*, re-examined on 2026-11-14 (issue #219): the prices page, with its *Locations* filter on Paris or Milan, lists the High Speed Gen2 volume (`volume.high-speed-gen2`, €0.000119 per GB and hour) and no `high-speed-gen2-luks` type, and neither the storage class page nor the encryption page gives a price for encrypted volumes (*Left out on purpose*).

### Instances

Category `compute.vm`: Public Cloud virtual machine instances (General Purpose, Compute and Memory
optimized, Discovery, GPU and Storage optimized ranges). "Instances" is the name the OVHcloud Console and
documentation use (the getting-started page's navigation path ends in "Compute > Instances"); the product
page is titled "Virtual Machine Instances". Pages:
<https://www.ovhcloud.com/en/public-cloud/virtual-instances/>,
<https://docs.ovhcloud.com/en/guides/public-cloud/network-services/ipv6-configuration>,
<https://docs.ovhcloud.com/en/guides/public-cloud/compute/getting-started>,
<https://docs.ovhcloud.com/en/guides/public-cloud/compute/save-an-instance>.

- `ipv6`: each Public Cloud instance "is delivered with an IPv4 address and an IPv6 address" (IPv6 page),
  through a Basic IP. Caveats on the same page: Floating IP and Gateway do not support IPv6, and in 3-AZ
  regions the IPv6 address of a Basic IP "cannot be provisioned from the OVHcloud Console yet" (the API and
  OpenStack tools are not excluded).
- `snapshots`: manual and scheduled instance snapshots, local or copied to another region (snapshot page).
  The regions page ticks "Instance Backup" in all seven regions below.
- Regions `GRA`, `LIM`, `MIL`, `PAR`, `RBX`, `SBG`, `WAW`: the regions page ticks General Purpose, Compute
  optimized and Memory optimized instances in each. Some ranges are narrower (Discovery not in Paris or
  Milan; GPU only in Gravelines, Frankfurt and Warsaw); the offering lists a region when at least the General
  Purpose range is there.

- **Prices** (prices page, *Virtual Machine Instances*, Linux, read 2026-11-13): the General Purpose B3 and B2 ranges, the Compute Optimised C3 range and the Memory Optimised R3 range. B3, C3 and R3 have one price in all seven regions. B2 is priced in the five 1-AZ regions only; the page offers no B2 flavour in Paris or Milan. The B3, C3 and R3 rows give their storage as "Local storage low latency", a link to the separately priced *Local Storage* option, so those plans include no storage; a B2 plan includes the SSD its row shows.

| Plan | vCPU | Memory | Storage on the page | EUR/hour | EUR/month (× 730) | Price regions |
|---|---|---|---|---|---|---|
| `b2-7` | 2 | 7 GB | 50 GB SSD | 0.0709 | `51.757` | `GRA`, `LIM`, `RBX`, `SBG`, `WAW` |
| `b2-15` | 4 | 15 GB | 100 GB SSD | 0.1342 | `97.966` | `GRA`, `LIM`, `RBX`, `SBG`, `WAW` |
| `b2-30` | 8 | 30 GB | 200 GB SSD | 0.2715 | `198.195` | `GRA`, `LIM`, `RBX`, `SBG`, `WAW` |
| `b2-60` | 16 | 60 GB | 400 GB SSD | 0.526 | `383.98` | `GRA`, `LIM`, `RBX`, `SBG`, `WAW` |
| `b2-120` | 32 | 120 GB | 400 GB SSD | 1.033 | `754.09` | `GRA`, `LIM`, `RBX`, `SBG`, `WAW` |
| `b3-8` | 2 | 8 GB | local storage, priced apart | 0.0512 | `37.376` | all |
| `b3-16` | 4 | 16 GB | local storage, priced apart | 0.1023 | `74.679` | all |
| `b3-32` | 8 | 32 GB | local storage, priced apart | 0.2046 | `149.358` | all |
| `b3-64` | 16 | 64 GB | local storage, priced apart | 0.4092 | `298.716` | all |
| `b3-128` | 32 | 128 GB | local storage, priced apart | 0.819 | `597.87` | all |
| `b3-256` | 64 | 256 GB | local storage, priced apart | 1.637 | `1195.01` | all |
| `b3-512` | 128 | 512 GB | local storage, priced apart | 3.274 | `2390.02` | all |
| `b3-640` | 160 | 640 GB | local storage, priced apart | 4.092 | `2987.16` | all |
| `c3-4` | 2 | 4 GB | local storage, priced apart | 0.0457 | `33.361` | all |
| `c3-8` | 4 | 8 GB | local storage, priced apart | 0.0913 | `66.649` | all |
| `c3-16` | 8 | 16 GB | local storage, priced apart | 0.1825 | `133.225` | all |
| `c3-32` | 16 | 32 GB | local storage, priced apart | 0.365 | `266.45` | all |
| `c3-64` | 32 | 64 GB | local storage, priced apart | 0.7301 | `532.973` | all |
| `c3-128` | 64 | 128 GB | local storage, priced apart | 1.461 | `1066.53` | all |
| `c3-256` | 128 | 256 GB | local storage, priced apart | 2.921 | `2132.33` | all |
| `c3-320` | 160 | 320 GB | local storage, priced apart | 3.651 | `2665.23` | all |
| `r3-16` | 2 | 16 GB | local storage, priced apart | 0.0663 | `48.399` | all |
| `r3-32` | 4 | 32 GB | local storage, priced apart | 0.1324 | `96.652` | all |
| `r3-64` | 8 | 64 GB | local storage, priced apart | 0.2648 | `193.304` | all |
| `r3-128` | 16 | 128 GB | local storage, priced apart | 0.53 | `386.90` | all |
| `r3-256` | 32 | 256 GB | local storage, priced apart | 1.059 | `773.07` | all |
| `r3-512` | 64 | 512 GB | local storage, priced apart | 2.118 | `1546.14` | all |
| `r3-1024` | 128 | 1024 GB | local storage, priced apart | 4.236 | `3092.28` | all |

- **Storage price** (prices page, *Block Storage*, read 2026-11-14, issue #219): the Classic volume row, €0.000059 per GB and hour, × 730 = 0.04307, rounded half up to `0.0431` per GiB a month, one price in all seven regions (the page's data prices `volume.classic` the same in DE1, EU-SOUTH-MIL, EU-WEST-PAR, GRA, RBX-A, SBG and WAW1). It is the price of the storage beyond what a plan includes: all of a B3, C3 or R3 instance's disk, and a B2 instance's disk above its SSD. A Public Cloud instance's persistent disks beyond its plan are Block Storage volumes: an additional disk is a volume attached to the instance (<https://docs.ovhcloud.com/en/guides/public-cloud/compute/create-and-configure-an-additional-disk-on-an-instance>), and an instance can boot from a volume (<https://docs.ovhcloud.com/en/guides/public-cloud/compute/start-instance-on-attached-volume>). Classic is the base class, as under *Block Storage*. The Gen 3 *Local Storage* option (`instance.local-storage-gen3-gb`, "Local storage low latency", €0.0001458 per GB and hour, × 730 = 0.106434) is not the price recorded: the prices page calls it "Ephemeral local storage, designed for hot, temporary data directly on your instances", so it does not hold a migrated machine's persistent disk (*Left out on purpose*).

### Managed Kubernetes Service

Category `kubernetes.managed`: Managed Kubernetes in the 1-AZ regions (Free plan, and the Standard plan in
Roubaix). Pages: <https://www.ovhcloud.com/en/public-cloud/kubernetes/>,
<https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-kubernetes/mks-plans>,
<https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-kubernetes/datacenters-nodes-storage-flavors>,
<https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-kubernetes/using-cluster-autoscaler>.

- `autoscaling`: the cluster autoscaler adds and removes nodes of a node pool with the workload (cluster
  autoscaler page). No plan restriction is documented.
- No `node-high-availability` and no `zone-high-availability`: no replica or failover mechanism is
  documented for the control plane of a 1-AZ cluster, so neither level is stated in full (target rule).
  The plans page describes the Free plan's control plane as "single zone" with a 99.5% SLO. See *Left
  out on purpose*.
- No tier variant for the Standard plan in Roubaix: it adds no capability in the vocabulary (Floating IPs
  per node and a custom router have no entry), so it gets no offering (tier-variant rule).
- Regions `GRA`, `LIM`, `RBX`, `SBG`, `WAW`: the regions page ticks Managed Kubernetes Service in each. The
  Kubernetes regions page lists GRA5, GRA7, GRA9, GRA11, SBG5, DE1 and WAW1 (Free plan, 1-AZ) and EU-WEST-RBX
  (Standard plan, 1-AZ).

- **Prices** (prices page, *Managed Kubernetes Service*, read 2026-11-13): the page says its prices "only apply to MKS (Managed Kubernetes Service) control planes", and the worker nodes "correspond to the standard OVHcloud Public Cloud catalog offered in this region and are billed at standard price" (Kubernetes regions page). So a plan is a **worker node flavour at its *Instances* price**, and an estimate counts nodes. **Fixed fee per cluster** (the control plane, ADR 0026): the Free plan's control plane is "Included", recorded as `0.00` in `GRA`, `LIM`, `SBG` and `WAW`; `RBX` (EU-WEST-RBX) offers the Standard plan only, whose control plane costs €0.09 an hour per cluster, × 730 = 65.70, recorded as `65.70` in `RBX`. The fee is charged once per cluster, never per node. Flavours: B2 ("All region" in the Kubernetes regions page's flavour table) at its price in all five regions, and B3, C3 and R3, which the same table offers in "DE1 / GRA11 / UK1 / WAW1" only, so their price is recorded for `GRA`, `LIM` and `WAW` and an estimate in `RBX` or `SBG` finds no price for them.

| Plan | vCPU | Memory | Storage on the page | EUR/hour | EUR/month (× 730) | Price regions |
|---|---|---|---|---|---|---|
| `b2-7` | 2 | 7 GB | 50 GB SSD | 0.0709 | `51.757` | all |
| `b2-15` | 4 | 15 GB | 100 GB SSD | 0.1342 | `97.966` | all |
| `b2-30` | 8 | 30 GB | 200 GB SSD | 0.2715 | `198.195` | all |
| `b2-60` | 16 | 60 GB | 400 GB SSD | 0.526 | `383.98` | all |
| `b2-120` | 32 | 120 GB | 400 GB SSD | 1.033 | `754.09` | all |
| `b3-8` | 2 | 8 GB | local storage, priced apart | 0.0512 | `37.376` | `GRA`, `LIM`, `WAW` |
| `b3-16` | 4 | 16 GB | local storage, priced apart | 0.1023 | `74.679` | `GRA`, `LIM`, `WAW` |
| `b3-32` | 8 | 32 GB | local storage, priced apart | 0.2046 | `149.358` | `GRA`, `LIM`, `WAW` |
| `b3-64` | 16 | 64 GB | local storage, priced apart | 0.4092 | `298.716` | `GRA`, `LIM`, `WAW` |
| `b3-128` | 32 | 128 GB | local storage, priced apart | 0.819 | `597.87` | `GRA`, `LIM`, `WAW` |
| `b3-256` | 64 | 256 GB | local storage, priced apart | 1.637 | `1195.01` | `GRA`, `LIM`, `WAW` |
| `b3-512` | 128 | 512 GB | local storage, priced apart | 3.274 | `2390.02` | `GRA`, `LIM`, `WAW` |
| `b3-640` | 160 | 640 GB | local storage, priced apart | 4.092 | `2987.16` | `GRA`, `LIM`, `WAW` |
| `c3-4` | 2 | 4 GB | local storage, priced apart | 0.0457 | `33.361` | `GRA`, `LIM`, `WAW` |
| `c3-8` | 4 | 8 GB | local storage, priced apart | 0.0913 | `66.649` | `GRA`, `LIM`, `WAW` |
| `c3-16` | 8 | 16 GB | local storage, priced apart | 0.1825 | `133.225` | `GRA`, `LIM`, `WAW` |
| `c3-32` | 16 | 32 GB | local storage, priced apart | 0.365 | `266.45` | `GRA`, `LIM`, `WAW` |
| `c3-64` | 32 | 64 GB | local storage, priced apart | 0.7301 | `532.973` | `GRA`, `LIM`, `WAW` |
| `c3-128` | 64 | 128 GB | local storage, priced apart | 1.461 | `1066.53` | `GRA`, `LIM`, `WAW` |
| `c3-256` | 128 | 256 GB | local storage, priced apart | 2.921 | `2132.33` | `GRA`, `LIM`, `WAW` |
| `c3-320` | 160 | 320 GB | local storage, priced apart | 3.651 | `2665.23` | `GRA`, `LIM`, `WAW` |
| `r3-16` | 2 | 16 GB | local storage, priced apart | 0.0663 | `48.399` | `GRA`, `LIM`, `WAW` |
| `r3-32` | 4 | 32 GB | local storage, priced apart | 0.1324 | `96.652` | `GRA`, `LIM`, `WAW` |
| `r3-64` | 8 | 64 GB | local storage, priced apart | 0.2648 | `193.304` | `GRA`, `LIM`, `WAW` |
| `r3-128` | 16 | 128 GB | local storage, priced apart | 0.53 | `386.90` | `GRA`, `LIM`, `WAW` |
| `r3-256` | 32 | 256 GB | local storage, priced apart | 1.059 | `773.07` | `GRA`, `LIM`, `WAW` |
| `r3-512` | 64 | 512 GB | local storage, priced apart | 2.118 | `1546.14` | `GRA`, `LIM`, `WAW` |
| `r3-1024` | 128 | 1024 GB | local storage, priced apart | 4.236 | `3092.28` | `GRA`, `LIM`, `WAW` |

- **Storage price** (prices page, *Block Storage*, read 2026-11-14, issue #219): the Classic volume row, €0.000059 per GB and hour, × 730 = 0.04307, rounded half up to `0.0431` per GiB a month, one price in all five regions. The prices page's *Managed Kubernetes Service* section says "Block Storage volumes and Public IP addresses (Floating IPs) used by your Kubernetes nodes on Public Cloud instances are billed individually", and the Kubernetes regions page says a persistent volume "will actually be deployed using Public Cloud additional disks (Cinder Volumes)", whose classes are the Block Storage classes (`csi-cinder-classic`, `csi-cinder-high-speed`, `csi-cinder-high-speed-gen2`). So storage beyond a node's plan is a Block Storage volume, priced at its base class, Classic, as for *Instances*. It is counted per node (ADR 0027).

### Managed Kubernetes Service 3-AZ

Category `kubernetes.managed`: Managed Kubernetes on the Standard plan in the 3-AZ regions. Pages:
<https://www.ovhcloud.com/en/public-cloud/kubernetes/>,
<https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-kubernetes/mks-plans>,
<https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-kubernetes/understanding-mks-architecture>,
<https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-kubernetes/datacenters-nodes-storage-flavors>,
<https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-kubernetes/using-cluster-autoscaler>.

- `autoscaling`: the cluster autoscaler, as for *Managed Kubernetes Service*.
- `zone-high-availability`. **Needs:** the Standard plan, the only plan Paris and Milan offer
  (Kubernetes regions page), so it is this offering's default tier and every cluster there has the
  capability. **Scope: zone-level.** The Standard plan replicates the control plane and a dedicated etcd
  across three availability zones (architecture page), and a Standard cluster in a 3-AZ region "can
  remain fully functional during an AZ failure" (plans page), with a 99.99% SLA in 3-AZ regions. Node
  pools are spread over the zones by the customer (product page FAQ). The reference architecture page
  (<https://docs.ovhcloud.com/en/guides/public-cloud/cross-functional/deployments-modes-reference-architecture>,
  row "Managed Kubernetes Service") corroborates it: in an AZ failure "the Control Plane remains
  available and the customer's workload is rescheduled on the nodes on another available AZ".
  **Caveat:** zone level covers the control plane and node pools spread across the zones. A stateful
  workload survives a zone loss only when its persistent volumes use Regional Classic (*Block Storage
  3-AZ Regional Classic*): the same row says "workloads using persistent volumes of single-zone classes
  cannot be migrated to other AZs".
- `node-high-availability`: implied by zone level (ADR 0011); three control plane replicas also survive
  the loss of one of them.
- Regions `MIL`, `PAR`: the Kubernetes regions page lists EU-WEST-PAR and EU-SOUTH-MIL as Standard plan, 3-AZ.
- **Status:** generally available. OVHcloud's roadmap closed "MKS Standard offer"
  (<https://github.com/ovh/public-cloud-roadmap/issues/693>) on 2025-09-17 with a collaborator's comment
  that the "Managed Kubernetes Standard plan is now available on General Availability", and closed
  "Multi-AZs clusters" (<https://github.com/ovh/public-cloud-roadmap/issues/22>) on 2025-10-02 after a
  comment that the Standard plan is in General Availability in EU-WEST-PAR with 3-AZ deployment. The
  regions page ticks both regions without the beta mark. Three lines still describe the plan as before
  GA, and are taken as stale: the product page FAQ says the 99.99% SLA "will be offered after the beta
  phase", the plans page gives a "99.99% SLA at General Availability", and the architecture page's
  comparison table gives "99.99% SLA (at GA)".

- **Prices** (prices page, *Managed Kubernetes Service*, read 2026-11-13): as for *Managed Kubernetes Service*, a plan is a worker node flavour at its *Instances* price. The Kubernetes regions page's flavour table names no 3-AZ region, so the flavours follow its general rule that worker nodes "correspond to the standard OVHcloud Public Cloud catalog offered in this region and are billed at standard price": the B3, C3 and R3 flavours, which the prices page offers in Paris and Milan at the same price as elsewhere. B2 is not offered there. These node plans are an inference from that "standard Public Cloud catalog of the region ... billed at standard price" sentence, not from a page listing the 3-AZ flavours; re-check them at the next refresh. **Fixed fee per cluster** (the control plane, ADR 0026): every cluster here is on the Standard plan, whose control plane costs €0.09 an hour per cluster, × 730 = 65.70, recorded as `65.70` in `MIL` and `PAR`. The fee is charged once per cluster, never per node.

| Plan | vCPU | Memory | Storage on the page | EUR/hour | EUR/month (× 730) | Price regions |
|---|---|---|---|---|---|---|
| `b3-8` | 2 | 8 GB | local storage, priced apart | 0.0512 | `37.376` | all |
| `b3-16` | 4 | 16 GB | local storage, priced apart | 0.1023 | `74.679` | all |
| `b3-32` | 8 | 32 GB | local storage, priced apart | 0.2046 | `149.358` | all |
| `b3-64` | 16 | 64 GB | local storage, priced apart | 0.4092 | `298.716` | all |
| `b3-128` | 32 | 128 GB | local storage, priced apart | 0.819 | `597.87` | all |
| `b3-256` | 64 | 256 GB | local storage, priced apart | 1.637 | `1195.01` | all |
| `b3-512` | 128 | 512 GB | local storage, priced apart | 3.274 | `2390.02` | all |
| `b3-640` | 160 | 640 GB | local storage, priced apart | 4.092 | `2987.16` | all |
| `c3-4` | 2 | 4 GB | local storage, priced apart | 0.0457 | `33.361` | all |
| `c3-8` | 4 | 8 GB | local storage, priced apart | 0.0913 | `66.649` | all |
| `c3-16` | 8 | 16 GB | local storage, priced apart | 0.1825 | `133.225` | all |
| `c3-32` | 16 | 32 GB | local storage, priced apart | 0.365 | `266.45` | all |
| `c3-64` | 32 | 64 GB | local storage, priced apart | 0.7301 | `532.973` | all |
| `c3-128` | 64 | 128 GB | local storage, priced apart | 1.461 | `1066.53` | all |
| `c3-256` | 128 | 256 GB | local storage, priced apart | 2.921 | `2132.33` | all |
| `c3-320` | 160 | 320 GB | local storage, priced apart | 3.651 | `2665.23` | all |
| `r3-16` | 2 | 16 GB | local storage, priced apart | 0.0663 | `48.399` | all |
| `r3-32` | 4 | 32 GB | local storage, priced apart | 0.1324 | `96.652` | all |
| `r3-64` | 8 | 64 GB | local storage, priced apart | 0.2648 | `193.304` | all |
| `r3-128` | 16 | 128 GB | local storage, priced apart | 0.53 | `386.90` | all |
| `r3-256` | 32 | 256 GB | local storage, priced apart | 1.059 | `773.07` | all |
| `r3-512` | 64 | 512 GB | local storage, priced apart | 2.118 | `1546.14` | all |
| `r3-1024` | 128 | 1024 GB | local storage, priced apart | 4.236 | `3092.28` | all |

- **Storage price** (prices page, *Block Storage*, read 2026-11-14, issue #219): as for *Managed Kubernetes Service*, a node's storage beyond its plan is a Block Storage volume. The Classic volume row, which the page prices the same with its *Locations* filter on Paris or Milan, €0.000059 per GB and hour, × 730 = 0.04307, rounded half up to `0.0431` per GiB a month, one price in both regions. In these regions the Classic class is the Regional Classic Volume (*Block Storage 3-AZ Regional Classic*), the class a persistent volume must use to survive the loss of a zone.

### Managed MySQL

Category `database.mysql`: Public Cloud Databases for MySQL ("Managed MySQL" on the product page) on the
Essential/Discovery plan, one node, in the 1-AZ regions. This is the base offering (tier-variant rule):
the multi-node plans add capabilities, and are *Managed MySQL Multi-node*, *Managed MySQL Multi-node
Advanced* and their *3-AZ* forms. Pages: <https://www.ovhcloud.com/en/public-cloud/mysql/>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/mysql-capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/public-cloud-databases-regions-comparison>
(the deployment modes page),
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/backups>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/concepts-security-overview>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/faq>.

- `encryption-at-rest`: nodes use full volume encryption with LUKS and backups are encrypted; the security
  page answers "Yes, on every plan". **Needs:** nothing beyond the default Essential/Discovery plan.
- `point-in-time-restore`: PITR for MySQL to any point "within the retention period of the chosen customer
  plan" (backups page; feature table on the capabilities page). The MySQL capabilities page
  (`mysql-capabilities`) gives a backup retention for every plan, Discovery included. **Needs:**
  nothing beyond the default Essential/Discovery plan.
- No `node-high-availability`, no `zone-high-availability` and no `read-replicas`: the Discovery plan runs
  one node (MySQL capabilities page, *Plans*), and the "High availability" row of the capabilities page's
  plan overview is empty for Essential/Discovery. See *Left out on purpose*.
- Regions `GRA`, `LIM`, `RBX`, `SBG`, `WAW`: the regions page ticks MySQL in each, and the backups page lists
  GRA, DE, RBX, SBG and WAW as service locations. The MySQL capabilities page says the Public Cloud
  Databases offer is available "in all Public Cloud 1-AZ and 3-AZ regions". The 3-AZ regions are not
  listed for this plan; see *Left out on purpose*.

- **Prices** (prices page, *Databases*, table "Essential/Discovery plan - Databases for MySQL", read 2026-11-13). Plans: the Gen 2 Essential flavours and the Gen 3 Discovery flavours; the page says "Gen 2 instances will still be available under our Essential/Business/Enterprise plans" and "The new Gen 3 instances are now available under our Discovery/Production/Advanced plans". The Essential flavours are priced in `GRA`, `LIM`, `SBG` and `WAW` only (no Roubaix price), the Discovery flavours in all five 1-AZ regions at one price. vCPU and memory are per node, as the page gives them; the plan's price is the page's price per node (excl. VAT/hour), × 730, so an estimate multiplies it by the resource's count, its nodes (ADR 0025). The tier runs 1 node: the page's "Total price" is the per-node price × 1. Included storage is the lower bound of the page's usable storage range (TiB × 1024).

| Plan | vCPU | Memory | Storage on the page | EUR/hour per node | EUR/month per node (× 730) | Price regions |
|---|---|---|---|---|---|---|
| `Essential DB1-4` | 2 | 4 GB × 1 node | From 80 GiB to 240 GiB | 0.0746 | `54.458` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Essential DB1-7` | 2 | 7 GB × 1 node | From 160 GiB to 480 GiB | 0.1477 | `107.821` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Essential DB1-15` | 4 | 15 GB × 1 node | From 320 GiB to 960 GiB | 0.2968 | `216.664` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Essential DB1-30` | 8 | 30 GB × 1 node | From 640 GiB to 1.875 TiB | 0.5967 | `435.591` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Discovery B3-4` | 1 | 4 GB × 1 node | From 80 GiB to 400 GiB | 0.10338 | `75.4674` | all |
| `Discovery B3-8` | 2 | 8 GB × 1 node | From 160 GiB to 800 GiB | 0.1962 | `143.226` | all |
| `Discovery B3-16` | 4 | 16 GB × 1 node | From 320 GiB to 1.5625 TiB | 0.3923 | `286.379` | all |
| `Discovery B3-32` | 8 | 32 GB × 1 node | From 640 GiB to 3.125 TiB | 0.7846 | `572.758` | all |
| `Discovery B3-64` | 16 | 64 GB × 1 node | From 1.25 TiB to 6.25 TiB | 1.5691 | `1145.443` | all |
| `Discovery B3-128` | 32 | 128 GB × 1 node | From 2.5 TiB to 12.5 TiB | 3.1382 | `2290.886` | all |
| `Discovery B3-256` | 64 | 256 GB × 1 node | From 5 TiB to 25 TiB | 6.2763 | `4581.699` | all |

- **Storage price:** the Essential/Discovery table's "Price per GB of additional storage: €0.0004/hour/node", × 1 (one node, since every node holds the data) × 730 = `0.292` per GiB of the service's data a month, for the whole service at its tier's node count, in every region of the offering; it is not multiplied by the resource's count. The databases pricing guide bills additional storage per GB, per hour and per node (*Prices*).

### Managed MySQL Multi-node

Category `database.mysql`: Public Cloud Databases for MySQL on the Business/Production plan (2 nodes) in
the 1-AZ regions. A tier variant of *Managed MySQL* (tier-variant rule). Pages: <https://www.ovhcloud.com/en/public-cloud/mysql/>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/mysql-capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/public-cloud-databases-regions-comparison>
(the deployment modes page),
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/backups>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/concepts-security-overview>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/faq>.

- `encryption-at-rest` and `point-in-time-restore`: as for *Managed MySQL*; the pages state them for every
  plan.
- `node-high-availability`. **Needs:** the Business/Production plan (2 nodes; MySQL capabilities page, *Plans*), the plan this
  offering is named after; the capabilities page's plan overview ticks "High availability" for it in
  "Single AZ". **Scope: node-level only**, by the node-level bar (*Rules applied*). Data is replicated
  across nodes and "in case of a node failure, the failover mechanism is automatic" (FAQ). In a 1-AZ
  region "the nodes of a multi-node database service are deployed in different physical hosts offering
  resilience to host outage", and "this single AZ setup offers resilience against node and disk
  failures", but the nodes "may be deployed within a single data center" (deployment modes page). It
  survives the loss of a node or host, not of the zone, so `zone-high-availability` is not claimed.
- No `read-replicas`: the product page names "Read replica included" on the Advanced plan only, so the
  capability is listed on *Managed MySQL Multi-node Advanced* and its 3-AZ form (target rule). See *Left
  out on purpose*.
- Regions `GRA`, `LIM`, `RBX`, `SBG`, `WAW`: as for *Managed MySQL*.

- **Prices** (prices page, *Databases*, table "Business/Production plan - Databases for MySQL", read 2026-11-13). Plans: the Gen 2 Business flavours and the Gen 3 Production flavours; the page says "Gen 2 instances will still be available under our Essential/Business/Enterprise plans" and "The new Gen 3 instances are now available under our Discovery/Production/Advanced plans". The Business flavours are priced in `GRA`, `LIM`, `SBG` and `WAW` only (no Roubaix price), the Production flavours in all five 1-AZ regions at one price. vCPU and memory are per node, as the page gives them; the plan's price is the page's price per node (excl. VAT/hour), × 730, so an estimate multiplies it by the resource's count, its nodes (ADR 0025). The tier runs 2 nodes: the page's "Total price" is the per-node price × 2. Included storage is the lower bound of the page's usable storage range (TiB × 1024).

| Plan | vCPU | Memory | Storage on the page | EUR/hour per node | EUR/month per node (× 730) | Price regions |
|---|---|---|---|---|---|---|
| `Business DB1-4` | 2 | 4 GB per node × 2 nodes | From 80 GiB to 240 GiB | 0.0949 | `69.277` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Business DB1-7` | 2 | 7 GB per node × 2 nodes | From 160 GiB to 480 GiB | 0.1899 | `138.627` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Business DB1-15` | 4 | 15 GB per node × 2 nodes | From 320 GiB to 960 GiB | 0.3797 | `277.181` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Business DB1-30` | 8 | 30 GB per node × 2 nodes | From 640 GiB to 1.875 TiB | 0.761 | `555.53` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Business DB1-60` | 16 | 60 GB per node × 2 nodes | From 1.25 TiB to 3.75 TiB | 1.5234 | `1112.082` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Business DB1-120` | 32 | 120 GB per node × 2 nodes | From 2.5 TiB to 7.5 TiB | 3.0484 | `2225.332` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Production B3-8` | 2 | 8 GB per node × 2 nodes | From 160 GiB to 800 GiB | 0.223 | `162.79` | all |
| `Production B3-16` | 4 | 16 GB per node × 2 nodes | From 320 GiB to 1.5625 TiB | 0.4461 | `325.653` | all |
| `Production B3-32` | 8 | 32 GB per node × 2 nodes | From 640 GiB to 3.125 TiB | 0.8922 | `651.306` | all |
| `Production B3-64` | 16 | 64 GB per node × 2 nodes | From 1.25 TiB to 6.25 TiB | 1.7844 | `1302.612` | all |
| `Production B3-128` | 32 | 128 GB per node × 2 nodes | From 2.5 TiB to 12.5 TiB | 3.5688 | `2605.224` | all |
| `Production B3-256` | 64 | 256 GB per node × 2 nodes | From 5 TiB to 25 TiB | 7.1377 | `5210.521` | all |

- **Storage price:** the Business/Production table's "Price per GB of additional storage: €0.0006/hour/node", × 2 (2 nodes, since every node holds the data) × 730 = `0.876` per GiB of the service's data a month, for the whole service at its tier's node count, in every region of the offering; it is not multiplied by the resource's count. The databases pricing guide bills additional storage per GB, per hour and per node (*Prices*).

### Managed MySQL Multi-node 3-AZ

Category `database.mysql`: Public Cloud Databases for MySQL on the Business/Production plan, Multi AZ
("Production Multi AZ"), in the 3-AZ regions. A tier variant of *Managed MySQL* (tier-variant rule). Pages:
<https://www.ovhcloud.com/en/public-cloud/mysql/>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/mysql-capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/public-cloud-databases-regions-comparison>
(the deployment modes page),
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/migrate-from-1az-to-3az>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/backups>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/concepts-security-overview>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/faq>.

- `encryption-at-rest` and `point-in-time-restore`: as for *Managed MySQL*; the pages state them for every
  plan, and per engine, not per region.
- `zone-high-availability`. **Needs:** the Business/Production plan, Multi AZ, the plan this offering is named
  after (the capabilities page's plan overview ticks "High availability" for it). **Scope: zone-level.**
  The deployment modes page says a multi-node service "is deployed across three independent
  availability zones, ensuring service continuity even if one zone experiences an outage"; its data is
  replicated synchronously across the three zones (1-AZ to 3-AZ migration page). The capabilities page
  gives 99.95% and 99.99% SLAs for the Multi AZ plans. The reference architecture page
  (<https://docs.ovhcloud.com/en/guides/public-cloud/cross-functional/deployments-modes-reference-architecture>,
  row "DBaaS") corroborates it: in an AZ failure "databases and data will remain available".
- `node-high-availability`: implied by zone level (ADR 0011); failover after a node failure is automatic
  (FAQ).
- No `read-replicas`: the product page names "Read replica included" on the Advanced plan only, so the
  capability is listed on *Managed MySQL Multi-node Advanced* and its 3-AZ form (target rule). See *Left
  out on purpose*.
- Regions `MIL`, `PAR`: the regions page ticks MySQL in "3-AZ Paris" and "3-AZ Milan"; the backups page lists
  EU-WEST-PAR and EU-SOUTH-MIL as service locations.

- **Prices** (prices page, *Databases*, table "Business/Production plan - Databases for MySQL", read 2026-11-13). Plans: the Gen 3 Production flavours at the price the page gives with its *Locations* filter on Paris or Milan, the same in both. The page offers no Business (Gen 2) flavour there. vCPU and memory are per node, as the page gives them; the plan's price is the page's price per node (excl. VAT/hour), × 730, so an estimate multiplies it by the resource's count, its nodes (ADR 0025). The tier runs 2 nodes: the page's "Total price" is the per-node price × 2. Included storage is the lower bound of the page's usable storage range (TiB × 1024).

| Plan | vCPU | Memory | Storage on the page | EUR/hour per node | EUR/month per node (× 730) | Price regions |
|---|---|---|---|---|---|---|
| `Production B3-8` | 2 | 8 GB per node × 2 nodes | From 160 GiB to 800 GiB | 0.2362 | `172.426` | all |
| `Production B3-16` | 4 | 16 GB per node × 2 nodes | From 320 GiB to 1.5625 TiB | 0.4723 | `344.779` | all |
| `Production B3-32` | 8 | 32 GB per node × 2 nodes | From 640 GiB to 3.125 TiB | 0.9447 | `689.631` | all |
| `Production B3-64` | 16 | 64 GB per node × 2 nodes | From 1.25 TiB to 6.25 TiB | 1.8894 | `1379.262` | all |
| `Production B3-128` | 32 | 128 GB per node × 2 nodes | From 2.5 TiB to 12.5 TiB | 3.7788 | `2758.524` | all |
| `Production B3-256` | 64 | 256 GB per node × 2 nodes | From 5 TiB to 25 TiB | 7.5575 | `5516.975` | all |

- **Storage price:** the Business/Production table's "Price per GB of additional storage: €0.0006/hour/node", × 2 (2 nodes, since every node holds the data) × 730 = `0.876` per GiB of the service's data a month, for the whole service at its tier's node count, in every region of the offering; it is not multiplied by the resource's count. The databases pricing guide bills additional storage per GB, per hour and per node (*Prices*).

### Managed MySQL Multi-node Advanced

Category `database.mysql`: Public Cloud Databases for MySQL on the Enterprise/Advanced plan (3 nodes)
in the 1-AZ regions. A tier variant of *Managed MySQL* (tier-variant rule), apart from *Managed MySQL
Multi-node* because it adds `read-replicas`. Pages: <https://www.ovhcloud.com/en/public-cloud/mysql/>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/mysql-capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/public-cloud-databases-regions-comparison>
(the deployment modes page),
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/backups>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/concepts-security-overview>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/faq>.

- `encryption-at-rest` and `point-in-time-restore`: as for *Managed MySQL*; the pages state them for every
  plan.
- `node-high-availability`. **Needs:** the Enterprise/Advanced plan (3 nodes; MySQL capabilities page, *Plans*), the plan this
  offering is named after; the capabilities page's plan overview ticks "High availability" for it in
  "Single AZ". **Scope: node-level only**, by the node-level bar (*Rules applied*). Data is replicated
  across nodes and "in case of a node failure, the failover mechanism is automatic" (FAQ). In a 1-AZ
  region "the nodes of a multi-node database service are deployed in different physical hosts offering
  resilience to host outage", and "this single AZ setup offers resilience against node and disk
  failures", but the nodes "may be deployed within a single data center" (deployment modes page). It
  survives the loss of a node or host, not of the zone, so `zone-high-availability` is not claimed.
- `read-replicas`. **Needs:** the Enterprise/Advanced plan, the plan this offering is named
  after. The product page names "Read replica included" on the Advanced plan (3 nodes) only. "Read-only
  nodes" for MySQL (capabilities page); a replica node "can accept READ operations only" (FAQ), and the
  product page's key features give "Replication node access" as "Read-only".
- Regions `GRA`, `LIM`, `RBX`, `SBG`, `WAW`: as for *Managed MySQL*.

- **Prices** (prices page, *Databases*, table "Enterprise/Advanced plan - Databases for MySQL", read 2026-11-13). Plans: the Gen 2 Enterprise flavours and the Gen 3 Advanced flavours; the page says "Gen 2 instances will still be available under our Essential/Business/Enterprise plans" and "The new Gen 3 instances are now available under our Discovery/Production/Advanced plans". The Enterprise flavours are priced in `GRA`, `LIM`, `SBG` and `WAW` only (no Roubaix price), the Advanced flavours in all five 1-AZ regions at one price. vCPU and memory are per node, as the page gives them; the plan's price is the page's price per node (excl. VAT/hour), × 730, so an estimate multiplies it by the resource's count, its nodes (ADR 0025). The tier runs 3 nodes: the page's "Total price" is the per-node price × 3. Included storage is the lower bound of the page's usable storage range (TiB × 1024).

| Plan | vCPU | Memory | Storage on the page | EUR/hour per node | EUR/month per node (× 730) | Price regions |
|---|---|---|---|---|---|---|
| `Enterprise DB1-4` | 2 | 4 GB per node × 3 nodes | From 80 GiB to 240 GiB | 0.0964 | `70.372` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Enterprise DB1-7` | 2 | 7 GB per node × 3 nodes | From 160 GiB to 480 GiB | 0.1899 | `138.627` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Enterprise DB1-15` | 4 | 15 GB per node × 3 nodes | From 320 GiB to 960 GiB | 0.3797 | `277.181` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Enterprise DB1-30` | 8 | 30 GB per node × 3 nodes | From 640 GiB to 1.875 TiB | 0.761 | `555.53` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Enterprise DB1-60` | 16 | 60 GB per node × 3 nodes | From 1.25 TiB to 3.75 TiB | 1.5234 | `1112.082` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Enterprise DB1-120` | 32 | 120 GB per node × 3 nodes | From 2.5 TiB to 7.5 TiB | 3.0484 | `2225.332` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Advanced B3-8` | 2 | 8 GB per node × 3 nodes | From 160 GiB to 800 GiB | 0.2404 | `175.492` | all |
| `Advanced B3-16` | 4 | 16 GB per node × 3 nodes | From 320 GiB to 1.5625 TiB | 0.4808 | `350.984` | all |
| `Advanced B3-32` | 8 | 32 GB per node × 3 nodes | From 640 GiB to 3.125 TiB | 0.9616 | `701.968` | all |
| `Advanced B3-64` | 16 | 64 GB per node × 3 nodes | From 1.25 TiB to 6.25 TiB | 1.9232 | `1403.936` | all |
| `Advanced B3-128` | 32 | 128 GB per node × 3 nodes | From 2.5 TiB to 12.5 TiB | 3.8464 | `2807.872` | all |
| `Advanced B3-256` | 64 | 256 GB per node × 3 nodes | From 5 TiB to 25 TiB | 7.6928 | `5615.744` | all |

- **Storage price:** the Enterprise/Advanced table's "Price per GB of additional storage: €0.0007/hour/node", × 3 (3 nodes, since every node holds the data) × 730 = `1.533` per GiB of the service's data a month, for the whole service at its tier's node count, in every region of the offering; it is not multiplied by the resource's count. The databases pricing guide bills additional storage per GB, per hour and per node (*Prices*).

### Managed MySQL Multi-node Advanced 3-AZ

Category `database.mysql`: Public Cloud Databases for MySQL on the Enterprise/Advanced plan, Multi AZ
("Advanced Multi AZ"), in the 3-AZ regions. A tier variant of *Managed MySQL* (tier-variant rule), apart
from *Managed MySQL Multi-node 3-AZ* because it adds `read-replicas`. Pages:
<https://www.ovhcloud.com/en/public-cloud/mysql/>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/mysql-capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/public-cloud-databases-regions-comparison>
(the deployment modes page),
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/migrate-from-1az-to-3az>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/backups>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/concepts-security-overview>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/faq>.

- `encryption-at-rest` and `point-in-time-restore`: as for *Managed MySQL*; the pages state them for every
  plan, and per engine, not per region.
- `zone-high-availability`. **Needs:** the Enterprise/Advanced plan, Multi AZ, the plan this offering is named
  after (the capabilities page's plan overview ticks "High availability" for it). **Scope: zone-level.**
  The deployment modes page says a multi-node service "is deployed across three independent
  availability zones, ensuring service continuity even if one zone experiences an outage"; its data is
  replicated synchronously across the three zones (1-AZ to 3-AZ migration page). The capabilities page
  gives 99.95% and 99.99% SLAs for the Multi AZ plans. The reference architecture page
  (<https://docs.ovhcloud.com/en/guides/public-cloud/cross-functional/deployments-modes-reference-architecture>,
  row "DBaaS") corroborates it: in an AZ failure "databases and data will remain available".
- `node-high-availability`: implied by zone level (ADR 0011); failover after a node failure is automatic
  (FAQ).
- `read-replicas`. **Needs:** the Enterprise/Advanced plan, Multi AZ, the plan this offering is named
  after. The product page names "Read replica included" on the Advanced plan (3 nodes) only. "Read-only
  nodes" for MySQL (capabilities page); a replica node "can accept READ operations only" (FAQ), and the
  product page's key features give "Replication node access" as "Read-only".
- Regions `MIL`, `PAR`: the regions page ticks MySQL in "3-AZ Paris" and "3-AZ Milan"; the backups page lists
  EU-WEST-PAR and EU-SOUTH-MIL as service locations.

- **Prices** (prices page, *Databases*, table "Enterprise/Advanced plan - Databases for MySQL", read 2026-11-13). Plans: the Gen 3 Advanced flavours at the price the page gives with its *Locations* filter on Paris or Milan, the same in both. The page offers no Enterprise (Gen 2) flavour there. vCPU and memory are per node, as the page gives them; the plan's price is the page's price per node (excl. VAT/hour), × 730, so an estimate multiplies it by the resource's count, its nodes (ADR 0025). The tier runs 3 nodes: the page's "Total price" is the per-node price × 3. Included storage is the lower bound of the page's usable storage range (TiB × 1024).

| Plan | vCPU | Memory | Storage on the page | EUR/hour per node | EUR/month per node (× 730) | Price regions |
|---|---|---|---|---|---|---|
| `Advanced B3-8` | 2 | 8 GB per node × 3 nodes | From 160 GiB to 800 GiB | 0.2545 | `185.785` | all |
| `Advanced B3-16` | 4 | 16 GB per node × 3 nodes | From 320 GiB to 1.5625 TiB | 0.5091 | `371.643` | all |
| `Advanced B3-32` | 8 | 32 GB per node × 3 nodes | From 640 GiB to 3.125 TiB | 1.0182 | `743.286` | all |
| `Advanced B3-64` | 16 | 64 GB per node × 3 nodes | From 1.25 TiB to 6.25 TiB | 2.0363 | `1486.499` | all |
| `Advanced B3-128` | 32 | 128 GB per node × 3 nodes | From 2.5 TiB to 12.5 TiB | 4.0727 | `2973.071` | all |
| `Advanced B3-256` | 64 | 256 GB per node × 3 nodes | From 5 TiB to 25 TiB | 8.1453 | `5946.069` | all |

- **Storage price:** the Enterprise/Advanced table's "Price per GB of additional storage: €0.0007/hour/node", × 3 (3 nodes, since every node holds the data) × 730 = `1.533` per GiB of the service's data a month, for the whole service at its tier's node count, in every region of the offering; it is not multiplied by the resource's count. The databases pricing guide bills additional storage per GB, per hour and per node (*Prices*).

### Managed PostgreSQL

Category `database.postgresql`: Public Cloud Databases for PostgreSQL ("Managed PostgreSQL" on the product page) on the
Essential/Discovery plan, one node, in the 1-AZ regions. This is the base offering (tier-variant rule):
the multi-node plans add capabilities, and are *Managed PostgreSQL Multi-node*, *Managed PostgreSQL
Multi-node Advanced* and their *3-AZ* forms. Pages: <https://www.ovhcloud.com/en/public-cloud/postgresql/>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/postgresql-capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/public-cloud-databases-regions-comparison>
(the deployment modes page),
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/backups>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/concepts-security-overview>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/faq>.

- `encryption-at-rest`: nodes use full volume encryption with LUKS and backups are encrypted; the security
  page answers "Yes, on every plan". **Needs:** nothing beyond the default Essential/Discovery plan.
- `point-in-time-restore`: PITR for PostgreSQL to any point "within the retention period of the chosen customer
  plan" (backups page; feature table on the capabilities page). The PostgreSQL capabilities page
  (`postgresql-capabilities`) gives a backup retention for every plan, Discovery included. **Needs:**
  nothing beyond the default Essential/Discovery plan.
- No `node-high-availability`, no `zone-high-availability` and no `read-replicas`: the Discovery plan runs
  one node (PostgreSQL capabilities page, *Plans*), and the "High availability" row of the capabilities page's
  plan overview is empty for Essential/Discovery. See *Left out on purpose*.
- Regions `GRA`, `LIM`, `RBX`, `SBG`, `WAW`: the regions page ticks PostgreSQL in each, and the backups page lists
  GRA, DE, RBX, SBG and WAW as service locations. The PostgreSQL capabilities page says the Public Cloud
  Databases offer is available "in all Public Cloud 1-AZ and 3-AZ regions". The 3-AZ regions are not
  listed for this plan; see *Left out on purpose*.

- **Prices** (prices page, *Databases*, table "Essential/Discovery plan - Databases for PostgreSQL", read 2026-11-13). Plans: the Gen 2 Essential flavours and the Gen 3 Discovery flavours; the page says "Gen 2 instances will still be available under our Essential/Business/Enterprise plans" and "The new Gen 3 instances are now available under our Discovery/Production/Advanced plans". The Essential flavours are priced in `GRA`, `LIM`, `SBG` and `WAW` only (no Roubaix price), the Discovery flavours in all five 1-AZ regions at one price. vCPU and memory are per node, as the page gives them; the plan's price is the page's price per node (excl. VAT/hour), × 730, so an estimate multiplies it by the resource's count, its nodes (ADR 0025). The tier runs 1 node: the page's "Total price" is the per-node price × 1. Included storage is the lower bound of the page's usable storage range (TiB × 1024).

| Plan | vCPU | Memory | Storage on the page | EUR/hour per node | EUR/month per node (× 730) | Price regions |
|---|---|---|---|---|---|---|
| `Essential DB1-4` | 2 | 4 GB × 1 node | From 80 GiB to 240 GiB | 0.0746 | `54.458` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Essential DB1-7` | 2 | 7 GB × 1 node | From 160 GiB to 480 GiB | 0.1477 | `107.821` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Essential DB1-15` | 4 | 15 GB × 1 node | From 320 GiB to 960 GiB | 0.2968 | `216.664` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Essential DB1-30` | 8 | 30 GB × 1 node | From 640 GiB to 1.875 TiB | 0.5967 | `435.591` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Discovery B3-4` | 1 | 4 GB × 1 node | From 80 GiB to 400 GiB | 0.10338 | `75.4674` | all |
| `Discovery B3-8` | 2 | 8 GB × 1 node | From 160 GiB to 800 GiB | 0.1962 | `143.226` | all |
| `Discovery B3-16` | 4 | 16 GB × 1 node | From 320 GiB to 1.5625 TiB | 0.3923 | `286.379` | all |
| `Discovery B3-32` | 8 | 32 GB × 1 node | From 640 GiB to 3.125 TiB | 0.7846 | `572.758` | all |
| `Discovery B3-64` | 16 | 64 GB × 1 node | From 1.25 TiB to 6.25 TiB | 1.5691 | `1145.443` | all |
| `Discovery B3-128` | 32 | 128 GB × 1 node | From 2.5 TiB to 12.5 TiB | 3.1382 | `2290.886` | all |
| `Discovery B3-256` | 64 | 256 GB × 1 node | From 5 TiB to 25 TiB | 6.2763 | `4581.699` | all |

- **Storage price:** the Essential/Discovery table's "Price per GB of additional storage: €0.0004/hour/node", × 1 (one node, since every node holds the data) × 730 = `0.292` per GiB of the service's data a month, for the whole service at its tier's node count, in every region of the offering; it is not multiplied by the resource's count. The databases pricing guide bills additional storage per GB, per hour and per node (*Prices*).

### Managed PostgreSQL Multi-node

Category `database.postgresql`: Public Cloud Databases for PostgreSQL on the Business/Production plan (2 nodes) in
the 1-AZ regions. A tier variant of *Managed PostgreSQL* (tier-variant rule). Pages: <https://www.ovhcloud.com/en/public-cloud/postgresql/>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/postgresql-capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/public-cloud-databases-regions-comparison>
(the deployment modes page),
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/backups>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/concepts-security-overview>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/faq>.

- `encryption-at-rest` and `point-in-time-restore`: as for *Managed PostgreSQL*; the pages state them for every
  plan.
- `node-high-availability`. **Needs:** the Business/Production plan (2 nodes; PostgreSQL capabilities page, *Plans*), the plan this
  offering is named after; the capabilities page's plan overview ticks "High availability" for it in
  "Single AZ". **Scope: node-level only**, by the node-level bar (*Rules applied*). Data is replicated
  across nodes and "in case of a node failure, the failover mechanism is automatic" (FAQ). In a 1-AZ
  region "the nodes of a multi-node database service are deployed in different physical hosts offering
  resilience to host outage", and "this single AZ setup offers resilience against node and disk
  failures", but the nodes "may be deployed within a single data center" (deployment modes page). It
  survives the loss of a node or host, not of the zone, so `zone-high-availability` is not claimed.
- No `read-replicas`: the product page names "Read replica included" on the Advanced plan only, so the
  capability is listed on *Managed PostgreSQL Multi-node Advanced* and its 3-AZ form (target rule). See *Left
  out on purpose*.
- Regions `GRA`, `LIM`, `RBX`, `SBG`, `WAW`: as for *Managed PostgreSQL*.

- **Prices** (prices page, *Databases*, table "Business/Production plan - Databases for PostgreSQL", read 2026-11-13). Plans: the Gen 2 Business flavours and the Gen 3 Production flavours; the page says "Gen 2 instances will still be available under our Essential/Business/Enterprise plans" and "The new Gen 3 instances are now available under our Discovery/Production/Advanced plans". The Business flavours are priced in `GRA`, `LIM`, `SBG` and `WAW` only (no Roubaix price), the Production flavours in all five 1-AZ regions at one price. vCPU and memory are per node, as the page gives them; the plan's price is the page's price per node (excl. VAT/hour), × 730, so an estimate multiplies it by the resource's count, its nodes (ADR 0025). The tier runs 2 nodes: the page's "Total price" is the per-node price × 2. Included storage is the lower bound of the page's usable storage range (TiB × 1024).

| Plan | vCPU | Memory | Storage on the page | EUR/hour per node | EUR/month per node (× 730) | Price regions |
|---|---|---|---|---|---|---|
| `Business DB1-4` | 2 | 4 GB per node × 2 nodes | From 80 GiB to 240 GiB | 0.0949 | `69.277` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Business DB1-7` | 2 | 7 GB per node × 2 nodes | From 160 GiB to 480 GiB | 0.1899 | `138.627` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Business DB1-15` | 4 | 15 GB per node × 2 nodes | From 320 GiB to 960 GiB | 0.3797 | `277.181` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Business DB1-30` | 8 | 30 GB per node × 2 nodes | From 640 GiB to 1.875 TiB | 0.761 | `555.53` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Business DB1-60` | 16 | 60 GB per node × 2 nodes | From 1.25 TiB to 3.75 TiB | 1.5234 | `1112.082` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Business DB1-120` | 32 | 120 GB per node × 2 nodes | From 2.5 TiB to 7.5 TiB | 3.0484 | `2225.332` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Production B3-8` | 2 | 8 GB per node × 2 nodes | From 160 GiB to 800 GiB | 0.223 | `162.79` | all |
| `Production B3-16` | 4 | 16 GB per node × 2 nodes | From 320 GiB to 1.5625 TiB | 0.4461 | `325.653` | all |
| `Production B3-32` | 8 | 32 GB per node × 2 nodes | From 640 GiB to 3.125 TiB | 0.8922 | `651.306` | all |
| `Production B3-64` | 16 | 64 GB per node × 2 nodes | From 1.25 TiB to 6.25 TiB | 1.7844 | `1302.612` | all |
| `Production B3-128` | 32 | 128 GB per node × 2 nodes | From 2.5 TiB to 12.5 TiB | 3.5688 | `2605.224` | all |
| `Production B3-256` | 64 | 256 GB per node × 2 nodes | From 5 TiB to 25 TiB | 7.1377 | `5210.521` | all |

- **Storage price:** the Business/Production table's "Price per GB of additional storage: €0.0006/hour/node", × 2 (2 nodes, since every node holds the data) × 730 = `0.876` per GiB of the service's data a month, for the whole service at its tier's node count, in every region of the offering; it is not multiplied by the resource's count. The databases pricing guide bills additional storage per GB, per hour and per node (*Prices*).

### Managed PostgreSQL Multi-node 3-AZ

Category `database.postgresql`: Public Cloud Databases for PostgreSQL on the Business/Production plan, Multi AZ
("Production Multi AZ"), in the 3-AZ regions. A tier variant of *Managed PostgreSQL* (tier-variant rule). Pages:
<https://www.ovhcloud.com/en/public-cloud/postgresql/>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/postgresql-capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/public-cloud-databases-regions-comparison>
(the deployment modes page),
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/migrate-from-1az-to-3az>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/backups>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/concepts-security-overview>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/faq>.

- `encryption-at-rest` and `point-in-time-restore`: as for *Managed PostgreSQL*; the pages state them for every
  plan, and per engine, not per region.
- `zone-high-availability`. **Needs:** the Business/Production plan, Multi AZ, the plan this offering is named
  after (the capabilities page's plan overview ticks "High availability" for it). **Scope: zone-level.**
  The deployment modes page says a multi-node service "is deployed across three independent
  availability zones, ensuring service continuity even if one zone experiences an outage"; its data is
  replicated synchronously across the three zones (1-AZ to 3-AZ migration page). The capabilities page
  gives 99.95% and 99.99% SLAs for the Multi AZ plans. The reference architecture page
  (<https://docs.ovhcloud.com/en/guides/public-cloud/cross-functional/deployments-modes-reference-architecture>,
  row "DBaaS") corroborates it: in an AZ failure "databases and data will remain available".
- `node-high-availability`: implied by zone level (ADR 0011); failover after a node failure is automatic
  (FAQ).
- No `read-replicas`: the product page names "Read replica included" on the Advanced plan only, so the
  capability is listed on *Managed PostgreSQL Multi-node Advanced* and its 3-AZ form (target rule). See *Left
  out on purpose*.
- Regions `MIL`, `PAR`: the regions page ticks PostgreSQL in "3-AZ Paris" and "3-AZ Milan"; the backups page lists
  EU-WEST-PAR and EU-SOUTH-MIL as service locations.

- **Prices** (prices page, *Databases*, table "Business/Production plan - Databases for PostgreSQL", read 2026-11-13). Plans: the Gen 3 Production flavours at the price the page gives with its *Locations* filter on Paris or Milan, the same in both. The page offers no Business (Gen 2) flavour there. vCPU and memory are per node, as the page gives them; the plan's price is the page's price per node (excl. VAT/hour), × 730, so an estimate multiplies it by the resource's count, its nodes (ADR 0025). The tier runs 2 nodes: the page's "Total price" is the per-node price × 2. Included storage is the lower bound of the page's usable storage range (TiB × 1024).

| Plan | vCPU | Memory | Storage on the page | EUR/hour per node | EUR/month per node (× 730) | Price regions |
|---|---|---|---|---|---|---|
| `Production B3-8` | 2 | 8 GB per node × 2 nodes | From 160 GiB to 800 GiB | 0.2362 | `172.426` | all |
| `Production B3-16` | 4 | 16 GB per node × 2 nodes | From 320 GiB to 1.5625 TiB | 0.4723 | `344.779` | all |
| `Production B3-32` | 8 | 32 GB per node × 2 nodes | From 640 GiB to 3.125 TiB | 0.9447 | `689.631` | all |
| `Production B3-64` | 16 | 64 GB per node × 2 nodes | From 1.25 TiB to 6.25 TiB | 1.8894 | `1379.262` | all |
| `Production B3-128` | 32 | 128 GB per node × 2 nodes | From 2.5 TiB to 12.5 TiB | 3.7788 | `2758.524` | all |
| `Production B3-256` | 64 | 256 GB per node × 2 nodes | From 5 TiB to 25 TiB | 7.5575 | `5516.975` | all |

- **Storage price:** the Business/Production table's "Price per GB of additional storage: €0.0006/hour/node", × 2 (2 nodes, since every node holds the data) × 730 = `0.876` per GiB of the service's data a month, for the whole service at its tier's node count, in every region of the offering; it is not multiplied by the resource's count. The databases pricing guide bills additional storage per GB, per hour and per node (*Prices*).

### Managed PostgreSQL Multi-node Advanced

Category `database.postgresql`: Public Cloud Databases for PostgreSQL on the Enterprise/Advanced plan (3 nodes)
in the 1-AZ regions. A tier variant of *Managed PostgreSQL* (tier-variant rule), apart from *Managed PostgreSQL
Multi-node* because it adds `read-replicas`. Pages: <https://www.ovhcloud.com/en/public-cloud/postgresql/>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/postgresql-capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/public-cloud-databases-regions-comparison>
(the deployment modes page),
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/backups>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/concepts-security-overview>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/faq>.

- `encryption-at-rest` and `point-in-time-restore`: as for *Managed PostgreSQL*; the pages state them for every
  plan.
- `node-high-availability`. **Needs:** the Enterprise/Advanced plan (3 nodes; PostgreSQL capabilities page, *Plans*), the plan this
  offering is named after; the capabilities page's plan overview ticks "High availability" for it in
  "Single AZ". **Scope: node-level only**, by the node-level bar (*Rules applied*). Data is replicated
  across nodes and "in case of a node failure, the failover mechanism is automatic" (FAQ). In a 1-AZ
  region "the nodes of a multi-node database service are deployed in different physical hosts offering
  resilience to host outage", and "this single AZ setup offers resilience against node and disk
  failures", but the nodes "may be deployed within a single data center" (deployment modes page). It
  survives the loss of a node or host, not of the zone, so `zone-high-availability` is not claimed.
- `read-replicas`. **Needs:** the Enterprise/Advanced plan, the plan this offering is named
  after. The product page names "Read replica included" on the Advanced plan (3 nodes) only. "Read-only
  nodes" for PostgreSQL (capabilities page); a replica node "can accept READ operations only" (FAQ), and the
  product page's key features give "Replication node access" as "Read-only".
- Regions `GRA`, `LIM`, `RBX`, `SBG`, `WAW`: as for *Managed PostgreSQL*.

- **Prices** (prices page, *Databases*, table "Enterprise/Advanced plan - Databases for PostgreSQL", read 2026-11-13). Plans: the Gen 2 Enterprise flavours and the Gen 3 Advanced flavours; the page says "Gen 2 instances will still be available under our Essential/Business/Enterprise plans" and "The new Gen 3 instances are now available under our Discovery/Production/Advanced plans". The Enterprise flavours are priced in `GRA`, `LIM`, `SBG` and `WAW` only (no Roubaix price), the Advanced flavours in all five 1-AZ regions at one price. vCPU and memory are per node, as the page gives them; the plan's price is the page's price per node (excl. VAT/hour), × 730, so an estimate multiplies it by the resource's count, its nodes (ADR 0025). The tier runs 3 nodes: the page's "Total price" is the per-node price × 3. Included storage is the lower bound of the page's usable storage range (TiB × 1024).

| Plan | vCPU | Memory | Storage on the page | EUR/hour per node | EUR/month per node (× 730) | Price regions |
|---|---|---|---|---|---|---|
| `Enterprise DB1-4` | 2 | 4 GB per node × 3 nodes | From 80 GiB to 240 GiB | 0.0964 | `70.372` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Enterprise DB1-7` | 2 | 7 GB per node × 3 nodes | From 160 GiB to 480 GiB | 0.1899 | `138.627` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Enterprise DB1-15` | 4 | 15 GB per node × 3 nodes | From 320 GiB to 960 GiB | 0.3797 | `277.181` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Enterprise DB1-30` | 8 | 30 GB per node × 3 nodes | From 640 GiB to 1.875 TiB | 0.761 | `555.53` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Enterprise DB1-60` | 16 | 60 GB per node × 3 nodes | From 1.25 TiB to 3.75 TiB | 1.5234 | `1112.082` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Enterprise DB1-120` | 32 | 120 GB per node × 3 nodes | From 2.5 TiB to 7.5 TiB | 3.0484 | `2225.332` | `GRA`, `LIM`, `SBG`, `WAW` |
| `Advanced B3-8` | 2 | 8 GB per node × 3 nodes | From 160 GiB to 800 GiB | 0.2404 | `175.492` | all |
| `Advanced B3-16` | 4 | 16 GB per node × 3 nodes | From 320 GiB to 1.5625 TiB | 0.4808 | `350.984` | all |
| `Advanced B3-32` | 8 | 32 GB per node × 3 nodes | From 640 GiB to 3.125 TiB | 0.9616 | `701.968` | all |
| `Advanced B3-64` | 16 | 64 GB per node × 3 nodes | From 1.25 TiB to 6.25 TiB | 1.9232 | `1403.936` | all |
| `Advanced B3-128` | 32 | 128 GB per node × 3 nodes | From 2.5 TiB to 12.5 TiB | 3.8464 | `2807.872` | all |
| `Advanced B3-256` | 64 | 256 GB per node × 3 nodes | From 5 TiB to 25 TiB | 7.6928 | `5615.744` | all |

- **Storage price:** the Enterprise/Advanced table's "Price per GB of additional storage: €0.0007/hour/node", × 3 (3 nodes, since every node holds the data) × 730 = `1.533` per GiB of the service's data a month, for the whole service at its tier's node count, in every region of the offering; it is not multiplied by the resource's count. The databases pricing guide bills additional storage per GB, per hour and per node (*Prices*).

### Managed PostgreSQL Multi-node Advanced 3-AZ

Category `database.postgresql`: Public Cloud Databases for PostgreSQL on the Enterprise/Advanced plan, Multi AZ
("Advanced Multi AZ"), in the 3-AZ regions. A tier variant of *Managed PostgreSQL* (tier-variant rule), apart
from *Managed PostgreSQL Multi-node 3-AZ* because it adds `read-replicas`. Pages:
<https://www.ovhcloud.com/en/public-cloud/postgresql/>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/postgresql-capabilities>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/public-cloud-databases-regions-comparison>
(the deployment modes page),
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/migrate-from-1az-to-3az>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/backups>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/concepts-security-overview>,
<https://docs.ovhcloud.com/en/guides/public-cloud/databases/faq>.

- `encryption-at-rest` and `point-in-time-restore`: as for *Managed PostgreSQL*; the pages state them for every
  plan, and per engine, not per region.
- `zone-high-availability`. **Needs:** the Enterprise/Advanced plan, Multi AZ, the plan this offering is named
  after (the capabilities page's plan overview ticks "High availability" for it). **Scope: zone-level.**
  The deployment modes page says a multi-node service "is deployed across three independent
  availability zones, ensuring service continuity even if one zone experiences an outage"; its data is
  replicated synchronously across the three zones (1-AZ to 3-AZ migration page). The capabilities page
  gives 99.95% and 99.99% SLAs for the Multi AZ plans. The reference architecture page
  (<https://docs.ovhcloud.com/en/guides/public-cloud/cross-functional/deployments-modes-reference-architecture>,
  row "DBaaS") corroborates it: in an AZ failure "databases and data will remain available".
- `node-high-availability`: implied by zone level (ADR 0011); failover after a node failure is automatic
  (FAQ).
- `read-replicas`. **Needs:** the Enterprise/Advanced plan, Multi AZ, the plan this offering is named
  after. The product page names "Read replica included" on the Advanced plan (3 nodes) only. "Read-only
  nodes" for PostgreSQL (capabilities page); a replica node "can accept READ operations only" (FAQ), and the
  product page's key features give "Replication node access" as "Read-only".
- Regions `MIL`, `PAR`: the regions page ticks PostgreSQL in "3-AZ Paris" and "3-AZ Milan"; the backups page lists
  EU-WEST-PAR and EU-SOUTH-MIL as service locations.

- **Prices** (prices page, *Databases*, table "Enterprise/Advanced plan - Databases for PostgreSQL", read 2026-11-13). Plans: the Gen 3 Advanced flavours at the price the page gives with its *Locations* filter on Paris or Milan, the same in both. The page offers no Enterprise (Gen 2) flavour there. vCPU and memory are per node, as the page gives them; the plan's price is the page's price per node (excl. VAT/hour), × 730, so an estimate multiplies it by the resource's count, its nodes (ADR 0025). The tier runs 3 nodes: the page's "Total price" is the per-node price × 3. Included storage is the lower bound of the page's usable storage range (TiB × 1024).

| Plan | vCPU | Memory | Storage on the page | EUR/hour per node | EUR/month per node (× 730) | Price regions |
|---|---|---|---|---|---|---|
| `Advanced B3-8` | 2 | 8 GB per node × 3 nodes | From 160 GiB to 800 GiB | 0.2545 | `185.785` | all |
| `Advanced B3-16` | 4 | 16 GB per node × 3 nodes | From 320 GiB to 1.5625 TiB | 0.5091 | `371.643` | all |
| `Advanced B3-32` | 8 | 32 GB per node × 3 nodes | From 640 GiB to 3.125 TiB | 1.0182 | `743.286` | all |
| `Advanced B3-64` | 16 | 64 GB per node × 3 nodes | From 1.25 TiB to 6.25 TiB | 2.0363 | `1486.499` | all |
| `Advanced B3-128` | 32 | 128 GB per node × 3 nodes | From 2.5 TiB to 12.5 TiB | 4.0727 | `2973.071` | all |
| `Advanced B3-256` | 64 | 256 GB per node × 3 nodes | From 5 TiB to 25 TiB | 8.1453 | `5946.069` | all |

- **Storage price:** the Enterprise/Advanced table's "Price per GB of additional storage: €0.0007/hour/node", × 3 (3 nodes, since every node holds the data) × 730 = `1.533` per GiB of the service's data a month, for the whole service at its tier's node count, in every region of the offering; it is not multiplied by the resource's count. The databases pricing guide bills additional storage per GB, per hour and per node (*Prices*).

### Metal Instances

Category `compute.baremetal`: Public Cloud instances on dedicated physical servers with no virtualisation
layer. Until issue #187 (ADR 0023) it served `compute.vm`; a bare-metal server is now its own category, so
it no longer competes with Instances for a virtual machine workload. Moving it changed no source fact.
Pages: <https://www.ovhcloud.com/en/public-cloud/metal-instances/>,
<https://docs.ovhcloud.com/en/guides/public-cloud/network-services/ipv6-configuration>,
<https://docs.ovhcloud.com/en/guides/public-cloud/compute/save-an-instance>.

- `ipv6`: the IPv6 page applies to "A Public Cloud instance (any model)", and every instance is delivered
  with an IPv6 address.
- `snapshots`: for Metal instances, snapshots are taken only as a "Cold Snapshot": the instance switches to
  rescue mode and reboots afterwards (snapshot page). The capability is there, with that downtime.
- Regions `GRA`, `LIM`, `SBG`, `WAW`: the regions page ticks Metal Instances in Strasbourg, Gravelines,
  London, Frankfurt and Warsaw, with the footnote "Metal Instances : SBG7, GRA11". London is not an EU member
  state.

- **Prices** (prices page, *Metal Instances*, read 2026-11-13): the three Metal flavours, one price in all four regions. Each row's storage, "2 x 960 GB SSD", is recorded as 1920 GiB, the total as published.

| Plan | vCPU | Memory | Storage on the page | EUR/hour | EUR/month (× 730) | Price regions |
|---|---|---|---|---|---|---|
| `bm-s1` | 4 | 32 GB | 2 x 960 GB SSD | 0.5 | `365.00` | all |
| `bm-m1` | 8 | 64 GB | 2 x 960 GB SSD | 0.85 | `620.50` | all |
| `bm-l1` | 16 | 128 GB | 2 x 960 GB SSD | 1.45 | `1058.50` | all |

- **No storage price.** Checked on 2026-11-14 (issue #219): a Metal Instance's storage is the fixed "2 x 960 GB SSD" of its flavour, and it cannot be extended with a volume. The product page's FAQ (<https://www.ovhcloud.com/en-ie/public-cloud/metal-instances/>) answers "Can I add block storage to a Metal Instance?" with "This feature is currently unavailable", and the prices page's *Metal Instances* table prices no storage option. A resource needing more than its plan's 1920 GiB is therefore "not estimated" on the storage, rather than priced at a volume it cannot attach.

### Object Storage

Category `storage.object`: S3-compatible Object Storage in the 1-AZ regions (High Performance, Standard and
Infrequent Access classes). Pages: <https://www.ovhcloud.com/en/public-cloud/object-storage/>,
<https://docs.ovhcloud.com/en/guides/storage-and-backup/object-storage/s3-location>,
<https://docs.ovhcloud.com/en/guides/storage-and-backup/object-storage/s3-regions-comparison>
(the deployment modes page),
<https://docs.ovhcloud.com/en/guides/storage-and-backup/object-storage/s3-versioning>,
<https://docs.ovhcloud.com/en/guides/storage-and-backup/object-storage/s3-encrypt-your-objects-with-sse-c>,
<https://docs.ovhcloud.com/en/guides/storage-and-backup/object-storage/s3-s3-compliancy>.

- `encryption-at-rest`: server-side encryption with OVHcloud-managed keys (SSE-OMK) or customer keys (SSE-C)
  (encryption page). The compliance page marks both as supported in every region (its "Regions" column, as
  opposed to Local Zones).
- `object-versioning`: bucket versioning, enabled per bucket and then only suspendable (versioning page);
  marked supported in every region on the compliance page.
- `node-high-availability`. **Needs:** nothing beyond the default class; the deployment modes page
  states it for the 1-AZ setup as a whole. **Scope: node-level only.** The 1-AZ setup "offers resilience
  against server rack and drive failures", erasure coding ensures "continuity in the event of hardware
  failures by splitting data across multiple disks and servers", and the page's comparison table gives
  *Data Availability* as "protected against server/disk failures". That is continued service through the
  loss of a server, with no customer action, not only a copy of data, and it meets the node-level bar
  (*Rules applied*): the page names the failures (server rack, drive, disk, server) the service keeps
  serving through. "Continuity in the event of
  hardware failures" is read as continued service (availability), not as recovery of lost data; that is
  what separates it from the Scaleway snapshot's Object Storage, whose FAQ says only that Scaleway can
  rebuild the data after a data-centre failure, a recovery, and states no continued service. The same table gives "Data center
  outage risk", and data "may become unavailable" in a data-centre outage, so `zone-high-availability` is
  not claimed.
- Regions `GRA`, `LIM`, `RBX`, `SBG`, `WAW`: the endpoints page lists `gra`, `de`, `rbx`, `sbg` and `waw` as
  "1-AZ region" endpoints, and the regions page ticks Object Storage there.

- **Storage price** (prices page, *Object Storage - One Zone*, read 2026-11-13): the Standard class, €0.00000972 per GiB and hour, × 730 = 0.0070956, rounded half up to `0.0071` per GiB a month, in every region. The High Performance and Infrequent Access classes are not recorded (one storage price per region; Standard is the default class).

### Object Storage 3-AZ

Category `storage.object`: S3-compatible Object Storage in the 3-AZ regions (Standard, Infrequent Access,
Active Archive, and Cold Archive in Paris only). Pages: <https://www.ovhcloud.com/en/public-cloud/object-storage/>,
<https://docs.ovhcloud.com/en/guides/storage-and-backup/object-storage/s3-location>,
<https://docs.ovhcloud.com/en/guides/storage-and-backup/object-storage/s3-regions-comparison>
(the deployment modes page),
<https://docs.ovhcloud.com/en/guides/storage-and-backup/object-storage/s3-choosing-the-right-storage-class-for-your-needs>,
<https://docs.ovhcloud.com/en/guides/storage-and-backup/object-storage/s3-versioning>,
<https://docs.ovhcloud.com/en/guides/storage-and-backup/object-storage/s3-encrypt-your-objects-with-sse-c>,
<https://docs.ovhcloud.com/en/guides/storage-and-backup/object-storage/s3-s3-compliancy>.

- `encryption-at-rest` and `object-versioning`: as for *Object Storage*; the compliance page states them for
  every region.
- `zone-high-availability`. **Needs:** the default class, Standard (Infrequent Access and Active
  Archive also qualify, each with a 99.99% SLA in 3-AZ regions on the storage class page; Cold Archive
  does not). **Scope: zone-level.** In a 3-AZ region, data "remains available for both read and write
  operations, even in the event of a zone failure" (deployment modes page), and the reference
  architecture page's "Object Storage ( Standard class )" row says the same. Cold Archive objects must be
  restored before they can be read (storage class page), so that class lacks the capability, and as a
  tier that only lacks capabilities it gets no offering (tier-variant rule).
- `node-high-availability`: implied by zone level (ADR 0011).
- Regions `MIL`, `PAR`: the endpoints page lists `eu-west-par` and `eu-south-mil` as "3-AZ region" endpoints,
  and the regions page ticks Object Storage there.

- **Storage price** (prices page, *Object Storage - Multi Zone*, read 2026-11-13): the Standard class, tier "First 50 TiB", €0.00001917 per GiB and hour, × 730 = 0.0139941, rounded half up to `0.014` (0.0140) per GiB a month, in Paris and Milan. The cheaper tiers above 50 TiB and the other classes are not recorded (*Left out on purpose*).

## Left out on purpose

- **`serverless.function`: no offering.** Checked on 2026-11-10:
  - The Public Cloud menu on `www.ovhcloud.com` (for example on the regions page) lists "Serverless" under
    Compute as "Coming soon", with no page behind it; <https://www.ovhcloud.com/en/public-cloud/serverless/>
    answers 404. It is not generally available.
  - AI Endpoints (<https://www.ovhcloud.com/en/public-cloud/ai-endpoints/>) gives API access to a catalogue of
    hosted generative AI models with token-based billing: model inference, not user code (PRD #11 decision).
  - AI Deploy (<https://docs.ovhcloud.com/en/guides/public-cloud/ai-machine-learning/ai-deploy-capabilities>)
    runs a customer's application or model on resources it keeps "as long as your app is running": a
    long-running deployment, not code run per invocation.
  - The OpenFaaS guide
    (<https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-kubernetes/install-openfaas>)
    shows customers how to install OpenFaaS on their own Kubernetes cluster; OVHcloud does not run it.
  - Workflow Management, ticked on the regions page, is not described as running customer code; the
    instance snapshot page uses it, and OpenStack Mistral workflows, to schedule instance snapshots.
- **`messaging.queue`: no offering.** The regions page lists no queue product in any Public Cloud table. The
  menu describes Kafka as a "Queuing solution for event-driven architectures", but the product is Apache
  Kafka (versions 3.9, 4.2 and 4.3:
  <https://docs.ovhcloud.com/en/guides/public-cloud/databases/kafka-capabilities>), a partitioned log whose
  consumers track offsets, with no per-message acknowledgement or dead-letter queue (PRD #11 decision).
  Apache Kafka 4.x also has share groups (KIP-932, "Queues for Kafka"), but OVHcloud's Kafka pages,
  including the advanced parameters reference
  (<https://docs.ovhcloud.com/en/guides/public-cloud/databases/kafka-advanced-parameters-references>), do not
  mention them, so they are not counted.
- **Managed Kubernetes Service (1-AZ) `node-high-availability` and `zone-high-availability` (PRD #14,
  target rule):** two pages claim high availability in general terms. The product page FAQ
  (<https://www.ovhcloud.com/en/public-cloud/kubernetes/>) says that in a Free cluster the control plane
  components "also have high availability, but within a single zone", and the architecture page's table
  of what OVHcloud manages
  (<https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-kubernetes/understanding-mks-architecture>)
  gives the API Server "Deployment, scaling, high availability, security patches", with no distinction
  between plans. Neither says how. The plans page
  (<https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-kubernetes/mks-plans>)
  calls the Free plan's control plane a "single zone managed control plane" with a 99.5% SLO, and the
  architecture page's diagram draws it as one shared control plane, against three control plane
  replicas, one per zone, for the Standard plan. The Standard plan in Roubaix (1-AZ) has a "99.9% SLA for
  1-AZ regions" (regions page,
  <https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-kubernetes/datacenters-nodes-storage-flavors>),
  but no page says whether its control plane is replicated there. No replica or failover mechanism is
  documented, so neither level is stated in full. Unlike *Object Storage* (1-AZ), these pages do not meet
  the node-level bar (*Rules applied*): they say "high availability" but name no failure (server, rack,
  disk or host) the control plane keeps serving through. This matches the Scaleway snapshot's Kapsule Mutualized
  tier.
- **Managed MySQL and Managed PostgreSQL (Essential/Discovery) `node-high-availability`,
  `zone-high-availability` and `read-replicas`:** the plan runs one node
  (<https://docs.ovhcloud.com/en/guides/public-cloud/databases/postgresql-capabilities>,
  <https://docs.ovhcloud.com/en/guides/public-cloud/databases/mysql-capabilities>, *Plans*), so it has no
  standby to fail over to and no replica to read from. The capabilities page's plan overview
  (<https://docs.ovhcloud.com/en/guides/public-cloud/databases/capabilities>) leaves "High availability"
  empty for it. These capabilities are on the Multi-node variants (read replicas on the Advanced ones
  only). Before PRD #14 the base offering listed them, a union of plans that no single plan has.
- **Managed MySQL and Managed PostgreSQL Multi-node (Business/Production) `read-replicas`:** the product
  pages name a read replica on Advanced only
  (<https://www.ovhcloud.com/en/public-cloud/mysql/>, <https://www.ovhcloud.com/en/public-cloud/postgresql/>:
  "Read replica included" on the Advanced plan), so the capability is not stated in full for the
  Business/Production plan (target rule). It is listed on *Multi-node Advanced* and *Multi-node Advanced
  3-AZ*.
- **The Essential/Discovery plan of MySQL and PostgreSQL in Paris and Milan:** no page read states that
  these engines offer Discovery in the 3-AZ regions. The capabilities page's plan overview
  (<https://docs.ovhcloud.com/en/guides/public-cloud/databases/capabilities>) has "Multi AZ" columns for
  Business/Production and Enterprise/Advanced only, and the deployment modes page
  (<https://docs.ovhcloud.com/en/guides/public-cloud/databases/public-cloud-databases-regions-comparison>)
  describes multi-node services there. So *Managed MySQL* and *Managed PostgreSQL* list the 1-AZ regions
  only, and the former *Managed MySQL 3-AZ* and *Managed PostgreSQL 3-AZ* became *Multi-node 3-AZ*
  variants. A Discovery service there would have exactly the base offering's capabilities, so a later
  refresh adds `MIL` and `PAR` to the base offering once a page states it, rather than a new offering.
- **Managed MySQL and Managed PostgreSQL Multi-node and Multi-node Advanced (1-AZ)
  `zone-high-availability`:** the nodes "may be
  deployed within a single data center"
  (<https://docs.ovhcloud.com/en/guides/public-cloud/databases/public-cloud-databases-regions-comparison>),
  so surviving a zone outage is not stated.
- **Block Storage `encryption-at-rest` in Warsaw, and on *Block Storage 3-AZ Regional Classic*:** not in
  Warsaw (rollout open, see *Block Storage LUKS*). In Paris and Milan, encrypted volumes are zonal and are
  listed as *Block Storage LUKS Zonal*. *Block Storage 3-AZ Regional Classic* does not have the
  capability: its class cannot be encrypted, and
  listing both capabilities on it would describe a volume that has both, which does not exist.
- **Block Storage `snapshots` in Paris and Milan:** the regions page ticks neither "Volume Snapshot" nor
  "Volume Backup" there.
- **Block Storage (1-AZ), Block Storage LUKS and Block Storage LUKS Zonal `node-high-availability` and
  `zone-high-availability`:** the storage class page
  (<https://docs.ovhcloud.com/en/guides/storage-and-backup/block-storage/block-storage-the-right-storage-class>)
  gives these classes "Zonal" replication, a copy of data within one zone, and states no continuity of an
  attached volume through the loss of a storage server (see *Rules applied*).
- **Object Storage (1-AZ) `zone-high-availability`:** the deployment modes page
  (<https://docs.ovhcloud.com/en/guides/storage-and-backup/object-storage/s3-regions-comparison>) gives
  "Data center outage risk" for the 1-AZ setup; only node level is listed.
- **Instances and Metal Instances `node-high-availability`, `zone-high-availability`, `autoscaling` and
  `encryption-at-rest`:** the pages
  read describe no automatic restart of an instance on another host, no autoscaling group for instances
  (autoscaling is documented for Kubernetes node pools), and no encryption of an instance's own disks
  (encrypted volumes are *Block Storage LUKS* and *Block Storage LUKS Zonal*).
- **Managed MySQL and Managed PostgreSQL `snapshots`, `autoscaling` and `ipv6`:** backups are automated
  (with PITR) and manual backups use the engines' own dump tools (FAQ), with no snapshot of the service's
  volume. Storage and node size are increased by hand
  (<https://docs.ovhcloud.com/en/guides/public-cloud/databases/update-cluster-plan>). IPv6 is not mentioned.
- **Managed Kubernetes `encryption-at-rest` and `ipv6`:** the architecture page lists etcd encryption at rest
  among OVHcloud's tasks, and its security features include "Secrets encryption in transit and at rest".
  Kubernetes keeps Secrets in etcd, and OVHcloud's roadmap still has "etcd encrypted at rest" open
  (<https://github.com/ovh/public-cloud-roadmap/issues/1035>), so neither line is counted. IPv6 for
  clusters is not documented.
- **Object Storage `ipv6`:** not stated on the pages read.
- **Features with no vocabulary entry**, left out under PRD #11 (*the taxonomy is unchanged*): Object
  Storage Object Lock (immutability) and asynchronous cross-region replication; customer-managed keys
  (SSE-C, Block Storage CMK in OKMS); Block Storage multi-attach; database forking and private networking
  (vRack); Managed Kubernetes custom gateways and disabled public exposure; instance remote snapshots in
  another region.
- **Products not in the snapshot:** Swift Object Storage and Cloud Archive (older Swift-API storage; S3
  Object Storage already serves the category); File Storage; MongoDB, Valkey, ClickHouse, OpenSearch and
  Kafka (no matching taxonomy category); Managed Private Registry, Load Balancer, Gateway and the other
  network services (no matching category); Local Zones (PRD #11).

Prices and plans left out (PRD #190 milestone 3, read 2026-11-13):

- **Block Storage LUKS and Block Storage LUKS Zonal: no storage price.** The prices page, read again on
  2026-11-14 (issue #219), lists Classic, High Speed and High Speed Gen2 volumes and no encrypted volume
  type, and neither the storage class page
  (<https://docs.ovhcloud.com/en/guides/storage-and-backup/block-storage/block-storage-the-right-storage-class>)
  nor the encryption page prices encrypted volumes. Reusing the unencrypted class's price would be an
  inferred price, so none is recorded, and an estimate for these volumes is "not estimated".
- **Metal Instances: no storage price.** Its flavours come with fixed disks and block storage cannot be
  attached (its section).
- **B3, C3 and R3 Kubernetes nodes in `RBX` and `SBG`.** The Kubernetes regions page's flavour table offers
  them in "DE1 / GRA11 / UK1 / WAW1" only, so *Managed Kubernetes Service* prices them in `GRA`, `LIM` and
  `WAW`; B2 nodes are priced in all five 1-AZ regions. The same table names no 3-AZ region at all, so
  *Managed Kubernetes Service 3-AZ* takes its flavours from the page's rule that worker nodes are the
  region's standard instances (its section).
- **Instance ranges not recorded:** Discovery (D2: shared resources, for "test, development and sandbox
  environments"), the older Compute and Memory ranges (C2, R2), Storage Optimized (I1) and the GPU
  instances. The B3, C3, R3 and B2 ranges cover general workloads; a workload that needs a GPU or NVMe
  storage is not matched by vCPU and memory alone.
- **Instance local storage:** the B3, C3 and R3 plans include no disk. Since issue #219 *Instances* and
  both Managed Kubernetes offerings price storage beyond a plan at the Classic Block Storage volume price.
  The *Local Storage* NVMe option for Gen 3 instances (€0.0001458 per GB and hour, sold in 50, 100, 200 and
  400 GB steps on the prices page) is ephemeral, so it is not recorded; a workload that keeps hot,
  temporary data on it pays more per GiB than the estimate shows.
- **Savings Plans and monthly billing:** commitment prices, not list prices (*Prices*).
- **Windows licences:** see *Prices*.
- **Block Storage High Speed and High Speed Gen2, Object Storage High Performance, Infrequent Access,
  Active Archive and Cold Archive:** an offering has one storage price per region, and the snapshot records
  its base class (Classic, Standard). A workload on a faster or colder class is estimated at the base
  class's price.
- **Object Storage 3-AZ volume tiers:** the Standard class costs less per GiB beyond 50 TiB ("Next 450 TiB"
  €0.00001712, "Over 500 TiB" €0.00001506 per GiB and hour). A storage price has one amount, so the first
  tier is recorded, and an estimate above 50 TiB is slightly high.
- **Discovery database prices in Paris and Milan.** The prices page lists the Discovery B3 flavours of
  MySQL and PostgreSQL in Paris and Milan too (`Discovery B3-8` at €0.2077 an hour there). *Managed MySQL*
  and *Managed PostgreSQL* still list the 1-AZ regions only (see the Essential/Discovery entry above): the
  price page is not a capability source, and adding `MIL` and `PAR` to the base offerings is a region
  change for a later refresh to check against the database pages. Their Paris and Milan prices are not
  recorded, since a price may name only regions of its offering.
- **Database backups:** included in the service price ("The database service itself (covering compute, base
  storage and backups)", databases pricing guide), so nothing is added.
