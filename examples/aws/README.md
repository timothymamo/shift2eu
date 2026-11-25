# AWS sample

A made-up web shop on AWS in `eu-central-1`, as an inventory: dummy data only, from no real account.

| ID | Raw type | Category | Capabilities | Size | Workload | Relations | Monthly cost |
|---|---|---|---|---|---|---|---|
| `shop-web-1` | `AWS::EC2::Instance` | `compute.vm` | `ipv6`, `snapshots` | 2 vCPU, 8 GiB | `shop` | depends-on `shop-orders` | 84.10 USD |
| `shop-web-2` | `AWS::EC2::Instance` | `compute.vm` | `ipv6`, `snapshots` | 2 vCPU, 8 GiB, 360 h a month | `shop` |  | 42.05 USD |
| `shop-web-asg` | `AWS::AutoScaling::AutoScalingGroup` | `compute.vm` | `autoscaling`, `zone-high-availability` | 2 vCPU, 8 GiB × 3 | `shop` |  |  |
| `shop-web-1-data` | `AWS::EC2::Volume` | `storage.block` | `snapshots` | 100 GiB | `shop` | attached-to `shop-web-1` | 9.52 USD |
| `shop-assets` | `AWS::S3::Bucket` | `storage.object` | `object-versioning`, `encryption-at-rest` | 250 GiB stored | `media` |  |  |
| `shop-db` | `AWS::RDS::DBInstance` | `database.postgresql` | `point-in-time-restore`, `read-replicas`, `node-high-availability`, `encryption-at-rest` | 2 vCPU, 8 GiB, 200 GiB stored | `shop` |  | 383.25 USD |
| `shop-legacy-mysql` | `AWS::RDS::DBInstance` | `database.mysql` | `point-in-time-restore`, `node-high-availability` | 2 vCPU, 8 GiB, 50 GiB stored |  |  |  |
| `shop-eks` | `AWS::EKS::Cluster` | `kubernetes.managed` | `autoscaling` | 4 vCPU, 16 GiB × 3 | `shop` | depends-on `shop-db` |  |
| `shop-thumbnailer` | `AWS::Lambda::Function` | `serverless.function` |  |  | `media` | depends-on `shop-assets` |  |
| `shop-orders` | `AWS::SQS::Queue` | `messaging.queue` | `dead-letter-queue`, `encryption-at-rest` |  | `shop` |  |  |
| `shop-orders-dlq` | `AWS::SQS::Queue` | `messaging.queue` | `encryption-at-rest` |  | `shop` | dead-letter-queue-of `shop-orders` |  |
| `shop-sessions` | `AWS::ElastiCache::ReplicationGroup` | `cache.redis` | `node-high-availability`, `sharding`, `encryption-at-rest` | 13 GiB × 4 nodes | `shop` |  |  |
| `shop-catalog-docs` | `AWS::DocDB::DBCluster` | `database.mongodb` | `point-in-time-restore`, `read-replicas`, `node-high-availability`, `encryption-at-rest` | 2 vCPU, 16 GiB × 3 instances | `shop` |  |  |
| `shop-order-events` | `AWS::SNS::Topic` | `messaging.topic` | `encryption-at-rest` |  | `shop` | depends-on `shop-orders` |  |
| `shop-images` | `AWS::ECR::Repository` | `registry.container` | `encryption-at-rest` |  | `shop` |  |  |
| `shop-product-search` | `AWS::OpenSearchService::Domain` | `search.opensearch` | `node-high-availability`, `zone-high-availability`, `encryption-at-rest`, `snapshots` | 2 vCPU, 16 GiB, 100 GiB × 3 data nodes | `shop` |  |  |
| `shop-cdn` | `AWS::CloudFront::Distribution` | `unclassified` |  |  | `media` | depends-on `shop-assets` |  |
| `shop.example.com` | `AWS::Route53::HostedZone` | `unclassified` |  |  | `shop` |  |  |

The sample is inventory format version 2
([ADR 0025](../../docs/adr/0025-json-inventory-file-format-v2.md)): **Size** is what the estimate prices
(vCPU and memory per instance or node, × the count; GiB for storage), one resource runs part of the
month, **Workload** groups resources in the report, **Relations** link them (a blocker on a resource is
noted on what `depends-on` it), and **Monthly cost** is the made-up source cost the report shows next
to the estimate, on some resources only.

Assess it from a clone (from a release archive, run `./shift2eu` in place of `./bin/shift2eu`):

```bash
make build
./bin/shift2eu assess --inventory examples/aws/inventory.json --target scaleway
./bin/shift2eu assess --inventory examples/aws/inventory.json --target all
```

This is the sample [Try it](../../docs/guide/try-it.md) uses; in a clone, its reports are pinned in
[`test/examples/testdata/`](../../test/examples/testdata/).
