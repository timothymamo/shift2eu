# Assess an inventory

`shift2eu assess` reads an inventory file and reports, per resource, what each EU target offers for it:
one target's assessment or a comparison of several, as Markdown or as versioned JSON, offline and
without credentials. With a sized inventory it also names the plan each resource would run on and
estimates its monthly price, per resource, per workload and in total. This page is the command's
manual: its flags, its report formats, the regions and estimates, the comparison and its exit codes.

## Usage

```text
shift2eu assess --inventory <file> --target <targets> [--format <format>] [--output <file>]
               [--region <target>=<region>,...] [--no-estimate] [--schema-version <version>]
```

`<targets>` is one of `ovhcloud`, `scaleway` or `upcloud`, a comma-separated list of them, or `all` for
every target; `shift2eu help` lists them. For example:

```bash
make build
./bin/shift2eu assess --inventory inventory.json --target ovhcloud
./bin/shift2eu assess --inventory inventory.json --target scaleway --output report.md
./bin/shift2eu assess --inventory inventory.json --target upcloud
./bin/shift2eu assess --inventory inventory.json --target ovhcloud,upcloud
./bin/shift2eu assess --inventory inventory.json --target all --output comparison.md
./bin/shift2eu assess --inventory inventory.json --target scaleway --format json --output report.json
./bin/shift2eu assess --inventory inventory.json --target scaleway --format json --schema-version 2
./bin/shift2eu assess --inventory inventory.json --target scaleway --region scaleway=nl-ams
./bin/shift2eu assess --inventory inventory.json --target all --region scaleway=fr-par,ovhcloud=GRA
./bin/shift2eu assess --inventory inventory.json --target all --no-estimate
```

`assess` reads an inventory file, compares every resource with the target's catalog and writes a
Markdown report: per resource its taxonomy category, best offering, fit (direct, partial, none), plan,
monthly estimate, relations and the reasons, then the required changes, the blockers, the workloads
and the cost estimate ([Regions and estimates](#regions-and-estimates)). Without `--output` the report goes to standard
output. With it, the report is written to the file with mode 0600, and an existing regular file is
replaced atomically, only once the whole report is rendered; a failed or cancelled run leaves it as it
was. A symbolic link to a regular file, or a dangling link, at that path is replaced, not followed.
`--output` naming anything else (a directory, a named pipe, a socket, a device, or a symbolic link to
one of them, such as `/dev/stdout`) or the inventory file itself (the same path, a hard link or a
symbolic link to it) is refused with exit code 1 before the inventory is read; leave out `--output` to
write to standard output.

`--format` chooses the report format: `markdown` (the default) or `json`. The JSON report holds the same
content as the Markdown one, as a versioned document (`"schemaVersion": 3`, `"kind": "assessment"`, or
`"comparison"` for several targets) for scripts and other tools: per resource its plan and estimate,
then the workloads, the estimate's basis (region, price date, currency, what it excludes) and total,
and the source's cost. Its format, its JSON Schemas
(`internal/adapter/report/jsonreport/schema/`) and its versioning rules are in
[ADR 0018](../adr/0018-version-the-json-report-format-for-the-cli-and-the-api.md); version 2 is
[ADR 0030](../adr/0030-serve-the-version-2-report-on-v1.md). A whole-number quantity, such as a plan's
`vcpus` or `includedStorageGiB`, is a JSON number; an amount of money, such as `"587.58"`, and a
quantity that can be fractional, such as a plan's `memoryGiB` (`"0.5"`, `"1.875"`), are exact decimal
strings, so a script never reads a rounded float. Parse them as decimals, not as floating point.

`--schema-version` chooses the JSON report's version: `3`, the default, or `2`, the document earlier
releases wrote, byte for byte. The two differ in two rules: in version 3 a total that estimated nothing
(`"estimated": 0`), the report's, a workload's or a compared target's, has no `amount` key at all,
where version 2 writes `"0.00"`; its counts stay. Test for the key, or for `estimated` above 0, before
you read an amount. And version 3 marks a global offering, one not bound to a region you choose (see
[Global offerings](#global-offerings)): a resource carries `"bestOfferingGlobal": true` after
`bestOffering` when its best offering is global, and `globalTies`, the global ties' names in the
order of `ties`, after `ties`; each key is absent when it would be false or empty, and version 2
writes neither. Its schemas are `assessment-v3.schema.json` and `comparison-v3.schema.json`
beside the version 2 ones ([ADR 0018](../adr/0018-version-the-json-report-format-for-the-cli-and-the-api.md),
issue #307). `--schema-version` with any format but `json`, or a version other than 2 or 3, is a usage
error (exit code 2).

With two or more targets, `assess` reads the inventory once and writes one comparison report. The
targets are compared in sorted key order, whatever order they are given in. The report starts with a
comparison: the fits counted and the estimated total per target, one row per resource with its fit,
best offering and estimate on each target, and the resources that are blockers on every target. Each target's own report follows, one
heading level lower. The comparison states facts per target; it does not rank the targets or recommend
one. Its start looks like this:

```markdown
# Migration assessment: `OVHcloud`, `Scaleway`, `UpCloud`

- Inventory file: `inventory.json`
- Source: `aws`
- Catalog: `OVHcloud` snapshot from `https://www.ovhcloud.com/en-ie/public-cloud/prices/`, retrieved 2026-11-25
- Catalog: `Scaleway` snapshot from `https://www.scaleway.com/en/pricing/`, retrieved 2026-11-13
- Catalog: `UpCloud` snapshot from `https://upcloud.com/pricing/`, retrieved 2026-11-13
- Region: `OVHcloud`: `GRA` (the target's default for estimates)
- Region: `Scaleway`: `fr-par` (the target's default for estimates)
- Region: `UpCloud`: `de-fra1` (the target's default for estimates)

## Comparison

### Summary

| Fit | `OVHcloud` | `Scaleway` | `UpCloud` |
|---|---|---|---|
| direct | 6 | 5 | 5 |
| partial | 2 | 5 | 3 |
| none | 3 | 1 | 3 |
| total | 11 | 11 | 11 |
| `OVHcloud` estimate | €... per month for 7 of 11 resources (storage not included in ... estimates) |  |  |
| ... | | | |

### Resources

| ID | Category | `OVHcloud` fit | `OVHcloud` best offering | `OVHcloud` monthly estimate | `Scaleway` fit | ... |
|---|---|---|---|---|---|---|
| `fn-example` | `serverless.function` | none |  | not estimated: no fit | direct | ... |

### Blockers on every target

- `misc-example`
  - `OVHcloud`: not classified
  ...
```

A target named twice, an unknown target, an empty element (`ovhcloud,,scaleway`) or `all` together
with another target is a usage error. If any selected target's catalog fails, the run fails with exit
code 1 and writes no report.

The inventory file is versioned JSON, described in
[ADR 0006](../adr/0006-json-inventory-file-format-v1.md) (version 1) and
[ADR 0025](../adr/0025-json-inventory-file-format-v2.md) (version 2, which adds sizes, usage,
workloads, relations and the source's cost);
[Build an inventory from your cloud](inventory.md) makes one from AWS, GCP or Azure. `--inventory` takes a path; standard input
is not supported. The targets are `ovhcloud` (OVHcloud Public Cloud), `scaleway` and `upcloud`, each
served from a dated snapshot of the provider's EU regions (for UpCloud, its zones in EU member states),
embedded in the binary, so `assess` runs offline and needs no credentials.

## Regions and estimates

### `--region`

Without `--region`, a target's fits are judged across all its regions, as if every offering were
available everywhere, and its estimates use the target's default region, which its catalog snapshot
records (OVHcloud `GRA`, Scaleway `fr-par`, UpCloud `de-fra1`). The report header names the region per
target: "(the target's default for estimates)".

`--region` names one region per target, as comma-separated `<target>=<region>` pairs:
`--region scaleway=nl-ams` or `--region scaleway=fr-par,ovhcloud=GRA`. For a target with a region:

- only the offerings available in that region are candidates. A resource whose only offerings are
  elsewhere gets fit `none`, with the reason "not offered in `<region>`"; a resource with an offering
  there keeps the fit that offering's capabilities give it;
- its estimates use that region's prices;
- the report header says "(chosen with `--region`)".

So running one inventory against two regions of one target shows which resources each region can
serve. The tool never picks a region for you. A target without a pair is assessed as without
`--region`.

`--region` is checked before the inventory is read. A region the target does not list, a target that
`--target` does not select, a target named twice, or a malformed element is a usage error: exit code 2,
one line naming the bad element, and for an unknown region the target's regions, then the usage
text:

```text
shift2eu: --region: target "scaleway": region "xx" is not one of the target's regions: "fr-par", "it-mil", "nl-ams", "pl-waw"
```

### Global offerings

Some offerings, such as a public DNS zone (`network.dns`), are global: not bound to a region you
choose, the provider serves them from its own locations, which the target's `SOURCES.md` names. A
global offering is a candidate whatever `--region` says, and the reports mark it "(global)" after its
name wherever they name it as an offering: the best offering and each tie, a comparison's best-offering
cell, and a required change's "(not offered by X)". The Markdown and HTML reports say what the mark
means once, under the first table that shows one; the JSON report, version 3, carries
`bestOfferingGlobal` and `globalTies` instead
([ADR 0048](../adr/0048-record-global-offerings-in-target-snapshot-format-v2-and-add-network-dns.md)).
A report that shows no global offering has neither the mark nor the note. The report's region line is
unchanged.

### `--no-estimate`

Estimates are on by default. `--no-estimate` turns them off: the report has no plan or estimate
column, no cost summary and no estimate in the workloads, and says nothing about prices. The source's
`monthlyCost` is shown only with estimates on. Fits, required changes, blockers, workloads and
relations are the same with or without it, and `--region` still scopes availability.

### Read the estimate

An estimate is not a quote. It is the target's monthly list price in EUR, excluding VAT and discounts,
on the retrieval date of the catalog snapshot, which the report shows next to every total. A newer
release can embed newer prices. Network traffic, IP addresses, load balancers and support are not
priced.

- **Plan.** For a resource with a size and a fit other than `none`, the plan is the cheapest plan of
  its best offering whose vCPU and memory are each at least the resource's; ties go to fewer vCPU, then
  less memory, then the name. A `cache.redis` resource is matched on its memory alone, and a cache plan
  whose target gives no vCPU count is shown by its memory, "8 GiB"
  ([ADR 0046](../adr/0046-add-seven-taxonomy-categories-and-match-cache-plans-on-memory.md)). A size no
  plan covers shows "no plan large enough" with the largest plan's size. The size never changes the
  fit.
- **Monthly estimate** = plan price × `count` × `hoursPerMonth` / 730, plus storage above what the plan
  includes × the offering's per-GiB price. That storage is per node, × `count`, for compute
  (`compute.vm`, `compute.baremetal`), Kubernetes nodes (`kubernetes.managed`), search
  (`search.opensearch`) and Kafka (`messaging.kafka`); a managed database's (`database.*`) is the
  service's data, counted once
  ([ADR 0027](../adr/0027-match-plans-and-estimate-monthly-prices-in-a-domain-package.md),
  [ADR 0047](../adr/0047-add-search-opensearch-and-messaging-kafka-and-price-their-storage-per-node.md)).
  A cache (`cache.redis`) has no storage. Block, object and file storage are priced per GiB alone. A
  `count` that is not given is taken as 1, and an `hoursPerMonth` that is not given as 730; the cell
  says "(count assumed 1)" or "(730 hours assumed)". Amounts are rounded to the cent only when shown.
- **Storage not included.** Some plans include no disk: their storage is sold per GiB apart from the
  plan (OVHcloud Gen 3 Instances and Managed Kubernetes nodes, Scaleway Instances, Kubernetes Kapsule
  nodes and Managed Databases, UpCloud Cloud Native servers and Managed MySQL and PostgreSQL). For a
  resource that gives no `storageGiB` on such a plan, the estimate is the plan alone and the cell says
  so: "(boot volume not included)" for a server or Kubernetes node (`compute.vm`, `compute.baremetal`,
  `kubernetes.managed`), "(data storage not included)" for a managed database (`database.*`), a search
  cluster (`search.opensearch`) or a Kafka cluster (`messaging.kafka`). No size is guessed: give
  `storageGiB` and the storage is priced, and the note goes; `storageGiB: 0` says the resource needs
  none. A total counts the estimates that carry the note, "€587.58 per month for 8 of 13 resources
  (storage not included in 4 estimates)". In the JSON report the estimate carries
  `"storageNotIncluded": "boot-volume"` or `"data-storage"` (accept any other value too), and the
  total `"storageNotIncluded"` with the count; both are left out when there is nothing to say
  ([ADR 0032](../adr/0032-say-when-an-estimate-leaves-out-storage-its-plan-does-not-include.md)).
- **Hourly prices.** A target that publishes hourly prices has them converted at 730 hours a month in
  its snapshot. UpCloud bills a server at most 672 hours a month, so its plans are priced at no more
  than 672 hours however long they run
  ([ADR 0029](../adr/0029-cap-the-billed-hours-of-an-offerings-plans-in-target-snapshot-format-v2.md)).
- **Fixed fees.** An offering's fixed monthly fee, such as a paid Kubernetes control plane, is added
  once per resource (one cluster), scaled by hours / 730 and never multiplied by `count` (its nodes)
  ([ADR 0026](../adr/0026-add-an-optional-fixed-monthly-fee-per-offering-to-snapshot-format-v2.md)). A
  fee alone never makes an estimate.
- **Not estimated.** A resource that cannot be priced says "not estimated" and why: `no fit`,
  `usage-priced` (`serverless.function`, `serverless.container`, `messaging.queue`,
  `messaging.topic`), `not priced` (`network.loadbalancer`, `registry.container`: billed per size or
  plan, and `network.dns`: billed per zone and per query, none of which an inventory field records; `not-priced` in the JSON report), `size unknown`, `no plan` (the offering
  lists none), `no plan large enough`, `not offered in <region>` (the offering is not sold in that
  region), or `no price in <region>` / `no storage price in <region>` (the offering is sold there but
  the snapshot has no price for it). It is never priced at €0: totals add only the estimated resources and say how
  many they cover, "€587.58 per month for 8 of 13 resources". A total that covers none of its
  resources gives no amount, only why: "none of 2 resources could be estimated (usage-priced, not
  offered in `GRA`)", each reason once, in the order above.

The rules are in [ADR 0027](../adr/0027-match-plans-and-estimate-monthly-prices-in-a-domain-package.md).

### Workloads, relations and the source's cost

These come from a version 2 inventory
([ADR 0028](../adr/0028-group-workloads-note-blocked-relations-and-sum-the-source-cost-in-the-assessment.md)):

- **Workloads.** Resources with the same `workload` are reported together, each workload with its fits,
  blockers and estimate; resources without one are under "No workload". The Workloads section is shown
  only when some resource names a workload.
- **Relations.** Each resource's row lists its relations. A blocker on a resource is also listed under
  every resource that `depends-on` it or is a `replica-of` it, as "affected by a blocker on `<id>`"
  (direct relations only, never transitively). No fit changes.
- **Source cost.** Where resources give a `monthlyCost`, each workload and the total show the source's
  cost per currency, never converted, and how many resources it covers. It is the user's own figure,
  from their bill; the tool never reads billing data.

A version 1 inventory, or a resource without a size, gets no plan and no estimate: its row says "not
estimated: size unknown".

## Exit codes

| Exit code | Meaning |
|---|---|
| 0 | The assessment ran. Blockers are a result, not a failure. |
| 1 | The inventory, the catalog, the report or the output failed, or the run was interrupted (SIGINT, SIGTERM, SIGHUP): one `shift2eu: ...` line on standard error. |
| 2 | Usage error: missing or unknown flag, command or target, a malformed target list, or a bad `--region` (an unknown region, an unselected target, a target named twice). Usage text on standard error. |

SIGQUIT is not part of the exit-code contract: it is the Go runtime's debugging signal, which dumps
every goroutine to standard error and exits with code 2.

`shift2eu version` prints the version, `shift2eu help` the usage.
