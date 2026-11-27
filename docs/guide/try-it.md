# Try it

A walkthrough that takes `shift2eu` from install to a report on a sample inventory, with no cloud
account: the CLI against one target, against every target and as JSON, in a region you choose and
with the estimates off, and the container image offline. It ends with how to
read the report: fits, plans and monthly estimates, required changes, blockers, workloads, the cost
summary and the comparison table.

## Get shift2eu and a sample

The samples are dummy inventories of a small web shop, one per source, in the repository's
[`examples/`](../../examples/) directory: [`examples/aws/inventory.json`](../../examples/aws/inventory.json),
[`examples/gcp/inventory.json`](../../examples/gcp/inventory.json) and
[`examples/azure/inventory.json`](../../examples/azure/inventory.json). They are made up and carry only
documentation values. They are inventory format version 2
([ADR 0025](../adr/0025-json-inventory-file-format-v2.md)): besides each resource's category and
capabilities, they give its size (vCPU, memory, storage, instance or node count), on one resource a
partial month of usage, the workload it belongs to, its relations to other resources, and on some
resources what the source charges for it today (`monthlyCost`). This page uses the AWS one; every
command works with the others too.

Clone the repository and build both binaries ([Install](install.md#build-from-source) lists the
tools):

```bash
git clone https://github.com/timothymamo/shift2eu.git
cd shift2eu
make build   # ./bin/shift2eu
```

Or download the `shift2eu` archive of a release for your platform
([Install](install.md#the-release-archives)) and unpack it into a directory. The archive holds the
samples too, in `examples/` beside `docs/`, so the commands below work from that directory as from the
clone's root: run `./shift2eu` in place of `./bin/shift2eu` (`shift2eu.exe` from the Windows zip). The archives do
not hold the repository's tests, so the links below to golden files under `test/` work only in a clone.

## Assess against one target

```bash
./bin/shift2eu assess --inventory examples/aws/inventory.json --target scaleway
```

`assess` reads the inventory, compares every resource with the embedded snapshot of Scaleway's catalog
and its list prices, and prints a Markdown report. It runs offline and needs no credentials. The report
starts with its inputs, the region its prices are for, and the fits counted:

```markdown
# Migration assessment: `Scaleway`

- Inventory file: `inventory.json`
- Source: `aws`
- Catalog: `Scaleway` snapshot from `https://www.scaleway.com/en/pricing/`, retrieved 2026-11-13
- Region: `fr-par` (the target's default for estimates)

## Summary

| Fit | Resources |
|---|---|
| direct | 6 |
| partial | 5 |
| none | 8 |
| total | 19 |
```

[Read the report](#read-the-report) explains the rest. `--output report.md` writes the report to a
file instead of standard output ([Assess an inventory](assess.md#usage)).

The excerpts on this page are quoted from golden files in the repository's
[`test/examples/testdata/`](../../test/examples/testdata/): the whole report is
[`aws-scaleway-estimates.md.golden`](../../test/examples/testdata/aws-scaleway-estimates.md.golden). A test renders them
from the sample through the same path as `assess`, so a change to a catalog snapshot shows up as a
change to those files; `go test ./test/examples -update` regenerates them. The output of an older
release can differ, since each release embeds the catalog snapshots of its day.

## Compare every target

```bash
./bin/shift2eu assess --inventory examples/aws/inventory.json --target all
```

`all` compares OVHcloud, Scaleway and UpCloud in one report (a list such as `--target ovhcloud,upcloud`
compares just those). It starts with a comparison, then each target's own report one heading level
lower ([`aws-all-estimates.md.golden`](../../test/examples/testdata/aws-all-estimates.md.golden)). The
summary counts the fits and gives each target's estimated total:

```markdown
### Summary

| Fit | `OVHcloud` | `Scaleway` | `UpCloud` |
|---|---|---|---|
| direct | 10 | 6 | 6 |
| partial | 3 | 5 | 2 |
| none | 6 | 8 | 11 |
| total | 19 | 19 | 19 |
| `OVHcloud` estimate | €2,508.85 per month for 11 of 19 resources (storage not included in 4 estimates) |  |  |
| `Scaleway` estimate |  | €587.58 per month for 8 of 19 resources (storage not included in 4 estimates) |  |
| `UpCloud` estimate |  |  | €359.31 per month for 8 of 19 resources |

Estimates are list prices on each catalog's retrieval date, excluding VAT and discounts; each target's own section gives its basis.
```

The totals are side by side, not ranked: each covers the resources that target can price, and a lower
total over different resources says little.

## Get the report as JSON

```bash
./bin/shift2eu assess --inventory examples/aws/inventory.json --target scaleway --format json
```

`--format json` writes the same content as a versioned document for scripts and other tools
([ADR 0018](../adr/0018-version-the-json-report-format-for-the-cli-and-the-api.md)): schema version 3
by default. `--schema-version 2` writes the older version 2 document. The two differ in one
rule: in version 3 a total that estimated nothing (`"estimated": 0`) has no `amount` key instead of
`"0.00"`.
Each resource carries its fit, best offering, reasons with a stable `code`, required changes,
relations, and its plan and estimate; the document ends with the workloads, the estimate's basis and
total, and the source's cost. The excerpt is the version 3 document, the command's default output
([`aws-scaleway-estimates.v3.json.golden`](../../test/examples/testdata/aws-scaleway-estimates.v3.json.golden)).
Every total in it estimated something, so `--schema-version 2` writes the same document but for
`"schemaVersion": 2`
([`aws-scaleway-estimates.json.golden`](../../test/examples/testdata/aws-scaleway-estimates.json.golden)).

```json
{
  "schemaVersion": 3,
  "kind": "assessment",
  "inventoryName": "inventory.json",
  "source": "aws",
  "target": {
    "name": "Scaleway",
    "snapshot": {
      "source": "https://www.scaleway.com/en/pricing/",
      "retrieved": "2026-11-13"
    }
  },
  "summary": {
    "direct": 6,
    "partial": 5,
    "none": 8,
    "total": 19
  },
  "estimate": {
    "region": "fr-par",
    "regionChosen": false,
    "priceDate": "2026-11-13",
    "currency": "EUR",
    "excluded": [
      "VAT",
      "discounts"
    ],
    "hoursBasis": 730,
    "total": {
      "amount": "587.58",
      "estimated": 8,
      "resources": 19,
      "storageNotIncluded": 4
    }
  },
```

With `jq`, for example, list the resources that cannot move as they are:

```bash
./bin/shift2eu assess --inventory examples/aws/inventory.json --target scaleway --format json |
  jq -r '.resources[] | select(.fit != "direct") | "\(.fit)\t\(.id)"'
```

## Choose a region, or turn the estimates off

Without `--region`, a target's fits are judged across all its regions, and its prices are those of
its default region (`fr-par` for Scaleway). `--region` names one region per target: only the offerings
available there count, and the prices are that region's
([`aws-scaleway-nl-ams.md.golden`](../../test/examples/testdata/aws-scaleway-nl-ams.md.golden)):

```bash
./bin/shift2eu assess --inventory examples/aws/inventory.json --target scaleway --region scaleway=nl-ams
```

```markdown
- Region: `nl-ams` (chosen with `--region`)
...
```

A resource whose only offerings are elsewhere gets fit `none`, "not offered in `<region>`". Every
Scaleway offering the sample needs is in `nl-ams`, so here only the region changes.

Estimates are on by default. `--no-estimate` leaves out the plan and estimate columns, the cost summary
and the source's cost; fits, workloads and relations are the same
([`aws-scaleway-no-estimate.md.golden`](../../test/examples/testdata/aws-scaleway-no-estimate.md.golden)):

```bash
./bin/shift2eu assess --inventory examples/aws/inventory.json --target scaleway --no-estimate
```

```markdown
| ID | Raw type | Category | Fit | Best offering | Relations | Reasons |
|---|---|---|---|---|---|---|
...
```

[Assess an inventory](assess.md#regions-and-estimates) has the rules.

## Run the container image

The image runs the same `assess` with no network, as your own user, with the sample mounted
read-only ([Run with Docker](docker.md) explains each flag; [Install](install.md#the-images) which tag
to pull). Its working directory is `/work`, so the sample is mounted there under the name the command
gives it:

```bash
docker run --rm --read-only --network none --user "$(id -u):$(id -g)" \
  --mount type=bind,src="$PWD/examples/aws/inventory.json",dst=/work/inventory.json,readonly \
  ghcr.io/timothymamo/shift2eu:v0.2.0 assess --inventory inventory.json --target scaleway
```

Add `--format json` or `--target all` as before. With a rootless runtime, pass `--user 0:0`
([Mounts, users and reports](docker.md#mounts-users-and-reports)). To build the image from the clone
instead, `make image` and use `shift2eu` in place of `ghcr.io/timothymamo/shift2eu:v0.2.0`.

A tag can be moved to another image. To verify the image first, see
[Verify releases](verify-releases.md): it resolves the tag to a digest and verifies that digest; run
`ghcr.io/timothymamo/shift2eu@<digest>` in place of the tag.

## Read the report

Every resource in the inventory gets a row in **Resources**: its taxonomy category, the best offering
the target has for that category, a **fit**, the **plan** it would run on, a **monthly estimate**, its
**relations** and the reasons for the fit. The fit is one of:

- **direct**: the best offering offers every capability the resource uses. It moves as it is.
- **partial**: the target has an offering in the resource's category, but not with every capability the
  resource uses. It moves with changes.
- **none**: the target has no offering in the category, or `shift2eu` could not classify the resource
  (`unclassified`). It does not move without a different design.

Without `--region`, regions are not compared: a fit says what the target offers, not where. A
resource's size never changes its fit.

A direct row, a direct row with a plan, and an unclassified row, from the Scaleway report:

```markdown
| ID | Raw type | Category | Fit | Best offering | Plan | Monthly estimate | Relations | Reasons |
|---|---|---|---|---|---|---|---|---|
| `shop-assets` | `AWS::S3::Bucket` | `storage.object` | direct | `Object Storage` |  | €4.03 |  | `Object Storage` offers every capability the resource uses |
| `shop-cdn` | `AWS::CloudFront::Distribution` | `unclassified` | none |  |  | not estimated: no fit | depends-on `shop-assets` | not classified |
| `shop-web-2` | `AWS::EC2::Instance` | `compute.vm` | direct | `Instances` | `PLAY2-MICRO` (4 vCPU, 8 GiB) | €19.83 (count assumed 1) (boot volume not included) |  | `Instances` offers every capability the resource uses |
```

The **plan** is the cheapest plan of the best offering whose vCPU and memory each cover the
resource's size: `shop-web-2` asks for 2 vCPU and 8 GiB, and the cheapest plan that covers both has 4
vCPU. The **monthly estimate** is the plan's monthly list price × count × hours / 730, plus storage
priced per GiB. `shop-web-2` runs 360 hours a month, so it costs about half its plan's €40.21; its
count is not given and is taken as 1, which the cell says. A Scaleway Instance's plan includes no
disk: its boot volume is Block Storage, priced per GiB, and `shop-web-2` gives no disk size, so the
cell says "(boot volume not included)" and the estimate is the plan alone. A resource that cannot be priced says
"not estimated" and why: no fit, a usage-priced category (functions and queues), size unknown, no
plan large enough, not offered in the region, or no price in the region. It is counted, never priced
at €0.
[Assess an inventory](assess.md#read-the-estimate) explains every rule.

Where several offerings fit equally well, the others are named as a tie:

```markdown
| `shop-eks` | `AWS::EKS::Cluster` | `kubernetes.managed` | direct | `Kubernetes Kapsule` (tie: `Kubernetes Kapsule Dedicated`) | `PRO2-XS` (4 vCPU, 16 GiB) | €245.72 (730 hours assumed) (boot volume not included) | depends-on `shop-db` | `Kubernetes Kapsule` offers every capability the resource uses |
```

Offerings tie when they have the same fit and lack as many of the resource's capabilities; among
them, the best offering is the first by name, and the plan and the estimate are that offering's.
Tied offerings lack the same number of capabilities, not always the same ones, and their prices
differ, so check a tie's own capabilities and prices before you pick it.

**Required changes** lists every partial resource with the capabilities its best offering lacks: what
you would have to replace, rebuild or do without on that target.

```markdown
## Required changes

- `shop-db`: `point-in-time-restore` (not offered by `Managed Database for PostgreSQL and MySQL High Availability`)
- `shop-legacy-mysql`: `point-in-time-restore` (not offered by `Managed Database for PostgreSQL and MySQL High Availability`)
- `shop-orders`: `encryption-at-rest` (not offered by `Queues`)
...
```

**Blockers** lists every resource whose fit is none, with the reason. Blockers are a result, not a
failure: `assess` still exits 0. Here the CDN and the DNS zone are unclassified; on OVHcloud the queues,
the function and the topic have no offering at all (OVHcloud does serve the cache, the MongoDB database,
the container registry and the two search clusters, which no other target's snapshot does yet). The web server and the topic `depends-on`
the order queue, so the blocker on the queue is listed under them too, as "affected by a blocker"; their
own fit does not change
([`aws-all-estimates.md.golden`](../../test/examples/testdata/aws-all-estimates.md.golden)):

```markdown
### Blockers

- `shop-cdn`: not classified
- `shop-order-events`: no offering in the `OVHcloud` catalog for `messaging.topic`
- `shop-orders`: no offering in the `OVHcloud` catalog for `messaging.queue`
- `shop-orders-dlq`: no offering in the `OVHcloud` catalog for `messaging.queue`
- `shop-thumbnailer`: no offering in the `OVHcloud` catalog for `serverless.function`
- `shop.example.com`: not classified
- `shop-order-events`: affected by a blocker on `shop-orders`
- `shop-web-1`: affected by a blocker on `shop-orders`
```

**Workloads** groups the resources by the `workload` the inventory names, each with its fits, its
estimate, the source's cost and its blockers. Resources that name none are under "No workload"; an
inventory that names no workload at all has no Workloads section. In the sample, the legacy MySQL
database belongs to no workload:

```markdown
## Workloads

### `media`

- Fit: 2 direct, 0 partial, 1 none
- Monthly estimate: €4.03 per month for 1 of 3 resources, list prices on 2026-11-13, excluding VAT and discounts
- Source cost: none in the inventory
- Blockers:
  - `shop-cdn`: not classified

### `shop`

- Fit: 4 direct, 4 partial, 7 none
- Monthly estimate: €517.16 per month for 6 of 15 resources (storage not included in 4 estimates), list prices on 2026-11-13, excluding VAT and discounts
- Source cost: `USD` $518.92 for 4 of 15 resources
- Blockers:
  - `shop-catalog-docs`: no offering in the `Scaleway` catalog for `database.mongodb`
  - `shop-images`: no offering in the `Scaleway` catalog for `registry.container`
  - `shop-log-search`: no offering in the `Scaleway` catalog for `search.opensearch`
  - `shop-order-events`: no offering in the `Scaleway` catalog for `messaging.topic`
  - `shop-product-search`: no offering in the `Scaleway` catalog for `search.opensearch`
  - `shop-sessions`: no offering in the `Scaleway` catalog for `cache.redis`
  - `shop.example.com`: not classified

### No workload

- Fit: 0 direct, 1 partial, 0 none
- Monthly estimate: €66.39 per month for 1 of 1 resource, list prices on 2026-11-13, excluding VAT and discounts
```

The **cost estimate** ends the report: the total of the estimated resources, how many of them there
are, the basis of the prices, and the region they are for. Where the inventory gives a `monthlyCost`,
the source's cost is shown beside it per currency, never converted, with how many resources it covers:

```markdown
## Cost estimate

- Total: €587.58 per month for 8 of 19 resources (storage not included in 4 estimates)
- Estimate: list prices on 2026-11-13, excluding VAT and discounts; a fixed fee is the offering's listed fee
- Prices in `fr-par`
- Hours: plan price × count × hours / 730, 730 hours for a resource that gives none
- Source cost: `USD` $518.92 for 4 of 19 resources
```

An estimate is not a quote: it uses list prices on the snapshot's date, without VAT, discounts,
network traffic or support, and it leaves out what it could not price. Compare it with the source's
cost only for the resources both cover.

The **comparison** of `--target all` puts the targets side by side: the fits counted and the estimates
per target (the summary above), one row per resource with its fit, best offering and estimate on each
target, and the resources that are blockers on every target. It states facts per target; it does not
rank the targets or recommend one.

```markdown
| ID | Category | `OVHcloud` fit | `OVHcloud` best offering | `OVHcloud` monthly estimate | `Scaleway` fit | `Scaleway` best offering | `Scaleway` monthly estimate | `UpCloud` fit | `UpCloud` best offering | `UpCloud` monthly estimate |
|---|---|---|---|---|---|---|---|---|---|---|
| `shop-db` | `database.postgresql` | direct | `Managed PostgreSQL Multi-node Advanced` | €236.81 (count assumed 1) (730 hours assumed) | partial | `Managed Database for PostgreSQL and MySQL High Availability` | €81.29 (count assumed 1) (730 hours assumed) | direct | `Managed PostgreSQL Multi-node` | €98.35 (count assumed 1) (730 hours assumed) |
| `shop-thumbnailer` | `serverless.function` | none |  | not estimated: no fit | direct | `Serverless Functions` | not estimated: usage-priced | none |  | not estimated: no fit |
```

Read across a row to see where a resource moves most easily, and down a column to see what one
target means for the whole estate. A blocker on every target, such as the CDN here, needs a plan
whichever target you choose.

## Next

- [Assess an inventory](assess.md): every flag, the report formats and the exit codes.
- [Build an inventory from your cloud](inventory.md): an inventory of your own AWS, GCP or Azure
  estate, from the provider's read-only CLI and `jq`.
- [ADR 0006](../adr/0006-json-inventory-file-format-v1.md) and
  [ADR 0025](../adr/0025-json-inventory-file-format-v2.md): the inventory format in full, version 1 and
  version 2 (sizes, usage, workloads, relations and the source's cost).
