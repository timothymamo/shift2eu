# Sample inventories

Dummy inventories to try `shift2eu` on, or to show it in a demo, without a cloud account. Each
describes the same small web shop as it might run on one source:

| Directory | Source | Resources |
|---|---|---|
| [`aws/`](aws/) | AWS | 13 |
| [`gcp/`](gcp/) | GCP | 14 |
| [`azure/`](azure/) | Azure | 11 |

The shop has web VMs (one of them a scaling group), on GCP a bare-metal search server, a data
volume, an object storage bucket with versioning, a PostgreSQL database with point-in-time restore,
read replicas and node or zone high availability, a MySQL database, a managed Kubernetes cluster, a
function, an order queue with a dead-letter queue, and a CDN and a DNS zone that `shift2eu` does
not classify (`unclassified`, with their `rawType`). Together the samples use every taxonomy
category and every capability, and give every fit (direct, partial, none) on at least one target,
so a report shows all its sections.

Each resource that a target prices has a size (vCPU and memory, or stored GiB, and a count of
instances or nodes), so the reports show a plan and a monthly estimate per resource and a cost summary.
Resources belong to named workloads (`shop`, and `media` or `catalog`), so the reports group them; the
AWS sample leaves its legacy MySQL database in no workload. Relations of all four types link them: a
volume `attached-to` its server, a dead-letter queue `dead-letter-queue-of` its queue, a read replica
`replica-of` its database (GCP), and `depends-on`, which makes a blocker on the order queue show up
under the web server. In the AWS and GCP samples one resource runs only part of the month
(`hoursPerMonth` under `usage`), and some resources give a made-up `monthlyCost`, so the reports show the source's cost for
part of the estate next to the estimate.

**Everything here is made up.** The names are invented; the only account ID, address range and
domain a sample may use are the documentation values `123456789012`, `192.0.2.0/24` and
`example.com` (or `.example`). `test/examples` holds the samples to that, checks that each one is a
valid inventory that assesses against every target, and pins the reports the walkthrough quotes as
golden files.

Each file is an inventory in format version 2,
[ADR 0025](../docs/adr/0025-json-inventory-file-format-v2.md), which amends
[ADR 0006](../docs/adr/0006-json-inventory-file-format-v1.md). Its `rawType` is the source's own
resource type name: AWS CloudFormation types (`AWS::EC2::Instance`), GCP Cloud Asset Inventory asset
types (`compute.googleapis.com/Instance`) and Azure Resource Manager types
(`Microsoft.Compute/virtualMachines`).

Assess one from a clone:

```bash
make build
./bin/shift2eu assess --inventory examples/aws/inventory.json --target all
```

From an unpacked release archive, which holds `examples/` too, run `./shift2eu` (with an `.exe`
suffix on Windows) from the unpacked directory in place of `./bin/shift2eu`. The archives do not hold the
repository's tests, so links to `test/` from these pages work only in a clone.

[Try it](../docs/guide/try-it.md) walks through the CLI, the container image and the HTTP API with
these samples, and explains how to read the report.
