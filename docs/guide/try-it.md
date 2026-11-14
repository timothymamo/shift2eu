# Try it

A walkthrough that takes `shift2eu` from install to a report on a sample inventory, with no cloud
account: the CLI against one target, against every target and as JSON, the container image offline,
and the HTTP API with `curl`. It ends with how to read the report: fits, required changes, blockers
and the comparison table.

## Get shift2eu and a sample

The samples are dummy inventories of a small web shop, one per source, in the repository's
[`examples/`](../../examples/) directory: [`examples/aws/inventory.json`](../../examples/aws/inventory.json),
[`examples/gcp/inventory.json`](../../examples/gcp/inventory.json) and
[`examples/azure/inventory.json`](../../examples/azure/inventory.json). They are made up and carry only
documentation values. This page uses the AWS one; every command works with the others too.

Clone the repository and build both binaries ([Install](install.md#build-from-source) lists the
tools):

```bash
git clone https://github.com/timothymamo/shift2eu.git
cd shift2eu
make build   # ./bin/shift2eu and ./bin/shift2eu-server
```

Or download the `shift2eu` and `shift2eu-server` archives of a release for your platform
([Install](install.md#the-release-archives)) and unpack them into one directory. Each archive holds the
samples too, in `examples/` beside `docs/`, so the commands below work from that directory as from the
clone's root: run `./shift2eu` and `./shift2eu-server` in place of `./bin/shift2eu` and
`./bin/shift2eu-server` (`shift2eu.exe` and `shift2eu-server.exe` from the Windows zip). The archives do
not hold the repository's tests, so the links below to golden files under `test/` work only in a clone.

## Assess against one target

```bash
./bin/shift2eu assess --inventory examples/aws/inventory.json --target scaleway
```

`assess` reads the inventory, compares every resource with the embedded snapshot of Scaleway's catalog
and prints a Markdown report. It runs offline and needs no credentials. The report starts with its
inputs and the fits counted:

```markdown
# Migration assessment: `Scaleway`

- Inventory file: `inventory.json`
- Source: `aws`
- Catalog: `Scaleway` snapshot from `https://www.scaleway.com/en/pricing/`, retrieved 2026-11-13

## Summary

| Fit | Resources |
|---|---|
| direct | 5 |
| partial | 6 |
| none | 2 |
| total | 13 |
```

[Read the report](#read-the-report) explains the rest. `--output report.md` writes the report to a
file instead of standard output ([Assess an inventory](assess.md#usage)).

The excerpts on this page are quoted from golden files in the repository's
[`test/examples/testdata/`](../../test/examples/testdata/): the whole report is
[`aws-scaleway.md.golden`](../../test/examples/testdata/aws-scaleway.md.golden). A test renders them
from the sample through the same path as `assess`, so a change to a catalog snapshot shows up as a
change to those files; `go test ./test/examples -update` regenerates them. The output of an older
release can differ, since each release embeds the catalog snapshots of its day.

## Compare every target

```bash
./bin/shift2eu assess --inventory examples/aws/inventory.json --target all
```

`all` compares OVHcloud, Scaleway and UpCloud in one report (a list such as `--target ovhcloud,upcloud`
compares just those). It starts with a comparison, then each target's own report one heading level
lower ([`aws-all.md.golden`](../../test/examples/testdata/aws-all.md.golden)):

```markdown
### Summary

| Fit | `OVHcloud` | `Scaleway` | `UpCloud` |
|---|---|---|---|
| direct | 7 | 5 | 5 |
| partial | 1 | 6 | 3 |
| none | 5 | 2 | 5 |
| total | 13 | 13 | 13 |
```

## Get the report as JSON

```bash
./bin/shift2eu assess --inventory examples/aws/inventory.json --target scaleway --format json
```

`--format json` writes the same content as a versioned document for scripts and other tools
([ADR 0018](../adr/0018-version-the-json-report-format-for-the-cli-and-the-api.md), schema version 2 from [ADR 0030](../adr/0030-serve-the-version-2-report-on-v1.md)). Each resource
carries its fit, best offering, reasons with a stable `code`, and required changes
([`aws-scaleway.json.golden`](../../test/examples/testdata/aws-scaleway.json.golden)):

```json
{
  "schemaVersion": 2,
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
    "direct": 5,
    "partial": 6,
    "none": 2,
    "total": 13
  },
```

With `jq`, for example, list the resources that cannot move as they are:

```bash
./bin/shift2eu assess --inventory examples/aws/inventory.json --target scaleway --format json |
  jq -r '.resources[] | select(.fit != "direct") | "\(.fit)\t\(.id)"'
```

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

## Ask the HTTP API

`shift2eu-server` answers the same assessments over HTTP, byte for byte what `assess` prints for the
same inventory and targets ([Run the HTTP API](http-api.md)). Start it on its default loopback address
with a token, which the requests read from the same variable and hand curl on standard input, so it
stays out of your shell history and the process list:

```bash
export SHIFT2EU_SERVER_TOKEN="$(openssl rand -hex 32)"
./bin/shift2eu-server &   # listening on 127.0.0.1:8080
curl -sS http://127.0.0.1:8080/readyz   # {"status":"ready"} once the catalogs have loaded
```

A request holds the inventory document and the target keys; `jq` wraps the sample in one. The
request body goes to a temporary file, outside the clone, so a request made from a real inventory
cannot end up in a commit:

```bash
req="$(mktemp)"

# The JSON report against Scaleway, the same as --format json above.
jq '{inventory: ., targets: ["scaleway"]}' examples/aws/inventory.json > "$req"
printf 'Authorization: Bearer %s\n' "$SHIFT2EU_SERVER_TOKEN" |
  curl -sS -X POST http://127.0.0.1:8080/v1/assessments -H @- \
    -H 'Content-Type: application/json' \
    --data-binary @"$req"

# The Markdown comparison of every target, the same as --target all above.
jq '{inventory: ., targets: ["all"]}' examples/aws/inventory.json > "$req"
printf 'Authorization: Bearer %s\n' "$SHIFT2EU_SERVER_TOKEN" |
  curl -sS -X POST http://127.0.0.1:8080/v1/assessments -H @- \
    -H 'Content-Type: application/json' -H 'Accept: text/markdown' \
    --data-binary @"$req"

# The targets and their catalog snapshots.
printf 'Authorization: Bearer %s\n' "$SHIFT2EU_SERVER_TOKEN" | curl -sS -H @- http://127.0.0.1:8080/v1/targets

rm -f "$req"
```

Stop the server with `kill %1` (or `kill` and its process ID) when you are done.

## Read the report

Every resource in the inventory gets a row in **Resources**: its taxonomy category, the best offering
the target has for that category, a **fit** and the reasons for it. The fit is one of:

- **direct**: the best offering offers every capability the resource uses. It moves as it is.
- **partial**: the target has an offering in the resource's category, but not with every capability the
  resource uses. It moves with changes.
- **none**: the target has no offering in the category, or `shift2eu` could not classify the resource
  (`unclassified`). It does not move without a different design.

Regions are not compared: a fit says what the target offers, not where.

A direct row and an unclassified row, from the Scaleway report:

```markdown
| `shop-assets` | `AWS::S3::Bucket` | `storage.object` | direct | `Object Storage` | `Object Storage` offers every capability the resource uses |
| `shop-cdn` | `AWS::CloudFront::Distribution` | `unclassified` | none |  | not classified |
```

Where several offerings fit equally well, the others are named as a tie:

```markdown
| `shop-db` | `AWS::RDS::DBInstance` | `database.postgresql` | partial | `Managed Database for PostgreSQL and MySQL` (tie: `Managed Database for PostgreSQL and MySQL High Availability`) | `Managed Database for PostgreSQL and MySQL` does not offer `point-in-time-restore`; `Managed Database for PostgreSQL and MySQL` does not offer `zone-high-availability` |
```

Offerings tie when they have the same fit and lack as many of the resource's capabilities; among
them, the best offering is simply the first by name. Here the Standalone tier and its High
Availability variant both lack `point-in-time-restore` and `zone-high-availability`, so the reasons
and required changes name `Managed Database for PostgreSQL and MySQL` only because it sorts first:
either one needs the same changes. The variant still adds `node-high-availability`, which the
resource does not ask for but a zone-redundant database comes closer to. Tied offerings lack the same
number of capabilities, not always the same ones, so check a tie's own capabilities before you pick
it.

**Required changes** lists every partial resource with the capabilities its best offering lacks: what
you would have to replace, rebuild or do without on that target.

```markdown
## Required changes

- `shop-db`: `point-in-time-restore`, `zone-high-availability` (not offered by `Managed Database for PostgreSQL and MySQL`)
- `shop-legacy-mysql`: `point-in-time-restore` (not offered by `Managed Database for PostgreSQL and MySQL High Availability`)
- `shop-orders`: `encryption-at-rest` (not offered by `Queues`)
```

**Blockers** lists every resource whose fit is none, with the reason. Blockers are a result, not a
failure: `assess` still exits 0. Here the CDN and the DNS zone are unclassified; on OVHcloud the queues
and the function have no offering at all
([`aws-all.md.golden`](../../test/examples/testdata/aws-all.md.golden)):

```markdown
### Blockers

- `shop-cdn`: not classified
- `shop-orders`: no offering in the `OVHcloud` catalog for `messaging.queue`
- `shop-orders-dlq`: no offering in the `OVHcloud` catalog for `messaging.queue`
- `shop-thumbnailer`: no offering in the `OVHcloud` catalog for `serverless.function`
- `shop.example.com`: not classified
```

The **comparison** of `--target all` puts the targets side by side: the fits counted per target (the
summary above), one row per resource with its fit and best offering on each target, and the resources
that are blockers on every target. It states facts per target; it does not rank the targets or
recommend one.

```markdown
| ID | Category | `OVHcloud` fit | `OVHcloud` best offering | `Scaleway` fit | `Scaleway` best offering | `UpCloud` fit | `UpCloud` best offering |
|---|---|---|---|---|---|---|---|
| `shop-db` | `database.postgresql` | direct | `Managed PostgreSQL Multi-node Advanced 3-AZ` | partial | `Managed Database for PostgreSQL and MySQL` | partial | `Managed PostgreSQL Multi-node` |
| `shop-thumbnailer` | `serverless.function` | none |  | direct | `Serverless Functions` | none |  |
```

Read across a row to see where a resource moves most easily, and down a column to see what one
target means for the whole estate. A blocker on every target, such as the CDN here, needs a plan
whichever target you choose.

## Next

- [Assess an inventory](assess.md): every flag, the report formats and the exit codes.
- [Build an inventory from your cloud](inventory.md): an inventory of your own AWS, GCP or Azure
  estate, from the provider's read-only CLI and `jq`.
- [ADR 0006](../adr/0006-json-inventory-file-format-v1.md): the inventory format in full.
- [Run the HTTP API](http-api.md) and [Behind a reverse proxy](reverse-proxy.md): deploying the server.
