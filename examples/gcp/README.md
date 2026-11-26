# GCP sample

A made-up web shop on Google Cloud in `europe-west1`, with a read replica in `europe-west4` and a
Bare Metal Solution search server in `europe-west3`, as an inventory: dummy data only, from no real
project. The raw types are Cloud Asset Inventory asset types, as `gcloud asset search-all-resources`
and `gcloud asset export` report them.

| ID | Raw type | Category | Capabilities | Size | Workload | Relations | Monthly cost |
|---|---|---|---|---|---|---|---|
| `shop-web-1` | `compute.googleapis.com/Instance` | `compute.vm` | `ipv6` | 2 vCPU, 8 GiB | `shop` | depends-on `shop-orders` | 53.45 USD |
| `shop-web-mig` | `compute.googleapis.com/InstanceGroupManager` | `compute.vm` | `autoscaling`, `zone-high-availability` | 2 vCPU, 8 GiB × 2 | `shop` |  |  |
| `shop-search-bms` | `baremetalsolution.googleapis.com/Instance` | `compute.baremetal` |  | 16 vCPU, 64 GiB, 500 h a month | `catalog` |  |  |
| `shop-web-1-data` | `compute.googleapis.com/Disk` | `storage.block` | `snapshots` | 100 GiB | `shop` | attached-to `shop-web-1` |  |
| `shop-assets` | `storage.googleapis.com/Bucket` | `storage.object` | `object-versioning` | 500 GiB stored | `shop` |  | 11.50 USD |
| `shop-db` | `sqladmin.googleapis.com/Instance` | `database.postgresql` | `point-in-time-restore`, `zone-high-availability`, `read-replicas` | 4 vCPU, 16 GiB, 500 GiB stored | `shop` |  | 412.36 USD |
| `shop-db-replica` | `sqladmin.googleapis.com/Instance` | `database.postgresql` |  | 4 vCPU, 16 GiB, 500 GiB stored | `shop` | replica-of `shop-db` |  |
| `shop-catalog-mysql` | `sqladmin.googleapis.com/Instance` | `database.mysql` | `point-in-time-restore`, `read-replicas` | 2 vCPU, 8 GiB, 100 GiB stored | `catalog` |  |  |
| `shop-gke` | `container.googleapis.com/Cluster` | `kubernetes.managed` | `autoscaling` | 4 vCPU, 16 GiB × 3 | `shop` | depends-on `shop-catalog-mysql`, depends-on `shop-db` |  |
| `shop-resize-images` | `cloudfunctions.googleapis.com/Function` | `serverless.function` | `autoscaling` |  | `shop` | depends-on `shop-assets` |  |
| `shop-orders` | `pubsub.googleapis.com/Subscription` | `messaging.queue` | `dead-letter-queue` |  | `shop` |  |  |
| `shop-orders-dlq` | `pubsub.googleapis.com/Topic` | `messaging.queue` |  |  | `shop` | dead-letter-queue-of `shop-orders` |  |
| `shop-checkout` | `run.googleapis.com/Service` | `serverless.container` |  |  | `shop` | depends-on `shop-orders` |  |
| `shop-uploads` | `file.googleapis.com/Instance` | `storage.file` | `encryption-at-rest`, `snapshots` | 1024 GiB provisioned | `shop` |  |  |
| `shop-cdn-backend` | `compute.googleapis.com/BackendBucket` | `unclassified` |  |  | `shop` | depends-on `shop-assets` |  |
| `shop-zone` | `dns.googleapis.com/ManagedZone` | `network.dns` | `dnssec`, `dns-routing` |  | `shop` |  |  |

The sample is inventory format version 2
([ADR 0025](../../docs/adr/0025-json-inventory-file-format-v2.md)): **Size** is what the estimate prices
(vCPU and memory per instance or node, × the count; GiB for storage), one resource runs part of the
month, **Workload** groups resources in the report, **Relations** link them (a blocker on a resource is
noted on what `depends-on` it), and **Monthly cost** is the made-up source cost the report shows next
to the estimate, on some resources only.

Assess it from a clone (from a release archive, run `./shift2eu` in place of `./bin/shift2eu`):

```bash
make build
./bin/shift2eu assess --inventory examples/gcp/inventory.json --target all
```

[Try it](../../docs/guide/try-it.md) walks through the commands and the report with the AWS sample;
every command works with this one too.
