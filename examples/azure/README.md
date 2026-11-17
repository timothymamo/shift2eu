# Azure sample

A made-up web shop on Azure in `westeurope`, as an inventory: dummy data only, from no real
subscription. The raw types are Azure Resource Manager resource types.

| ID | Raw type | Category | Capabilities | Size | Workload | Relations | Monthly cost |
|---|---|---|---|---|---|---|---|
| `vm-shop-web-1` | `Microsoft.Compute/virtualMachines` | `compute.vm` | `ipv6`, `snapshots` | 2 vCPU, 8 GiB | `shop` | depends-on `sb-shop-orders` | 80.59 EUR |
| `vmss-shop-web` | `Microsoft.Compute/virtualMachineScaleSets` | `compute.vm` | `autoscaling`, `zone-high-availability` | 2 vCPU, 8 GiB × 2 | `shop` |  |  |
| `disk-shop-web-1-data` | `Microsoft.Compute/disks` | `storage.block` | `snapshots` | 64 GiB | `shop` | attached-to `vm-shop-web-1` | 7.68 EUR |
| `stshopassets` | `Microsoft.Storage/storageAccounts` | `storage.object` | `object-versioning`, `encryption-at-rest` | 200 GiB stored | `shop` |  |  |
| `psql-shop` | `Microsoft.DBforPostgreSQL/flexibleServers` | `database.postgresql` | `point-in-time-restore`, `node-high-availability`, `read-replicas` | 2 vCPU, 8 GiB, 256 GiB stored | `shop` |  | 291.70 EUR |
| `mysql-shop-catalog` | `Microsoft.DBforMySQL/flexibleServers` | `database.mysql` | `point-in-time-restore`, `node-high-availability`, `read-replicas` | 2 vCPU, 8 GiB, 64 GiB stored | `catalog` |  |  |
| `aks-shop` | `Microsoft.ContainerService/managedClusters` | `kubernetes.managed` | `autoscaling` | 4 vCPU, 16 GiB × 3 | `shop` | depends-on `mysql-shop-catalog`, depends-on `psql-shop` |  |
| `func-shop-thumbnails` | `Microsoft.Web/sites` | `serverless.function` |  |  | `shop` | depends-on `stshopassets` |  |
| `sb-shop-orders` | `Microsoft.ServiceBus/namespaces/queues` | `messaging.queue` | `dead-letter-queue` |  | `shop` |  |  |
| `cdn-shop` | `Microsoft.Cdn/profiles` | `unclassified` |  |  | `shop` | depends-on `stshopassets` |  |
| `dns-shop` | `Microsoft.Network/dnsZones` | `unclassified` |  |  | `shop` |  |  |

The sample is inventory format version 2
([ADR 0025](../../docs/adr/0025-json-inventory-file-format-v2.md)): **Size** is what the estimate prices
(vCPU and memory per instance or node, × the count; GiB for storage), **Workload** groups resources in the report, **Relations** link them (a blocker on a resource is
noted on what `depends-on` it), and **Monthly cost** is the made-up source cost the report shows next
to the estimate, on some resources only.

Assess it from a clone (from a release archive, run `./shift2eu` in place of `./bin/shift2eu`):

```bash
make build
./bin/shift2eu assess --inventory examples/azure/inventory.json --target all
```

[Try it](../../docs/guide/try-it.md) walks through the commands and the report with the AWS sample;
every command works with this one too.
