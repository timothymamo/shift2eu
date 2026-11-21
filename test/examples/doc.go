// Package examples holds the tests that keep the sample inventories of the
// top-level examples/ directory honest (issue #179). A person trying shift2eu
// or watching a demo reads those samples, so they may not rot: every
// examples/**/*.json file is decoded by the real inventory-file adapter and
// declares the source its directory names, every sample assesses against every
// target through the real application layer and the embedded catalog snapshots,
// the samples together use every taxonomy category, every capability and every
// fit, they carry documentation values only (an AWS account ID of 123456789012,
// addresses in 192.0.2.0/24, names under example.com or .example), and the
// reports the walkthrough quotes are golden files regenerated with -update so a
// catalog snapshot change shows up in the diff.
//
// It also holds the real-data check of issue #249 (CLAUDE.md rule 2): every
// text file under examples/, docs/guide/ and every testdata/ directory of the
// module is scanned for account IDs, GUIDs, IP addresses (dotted, or written
// with hyphens or underscores, in a name or a private one on its own), host
// names, and owners and buckets after a vendor host, a bucket scheme, in an S3
// ARN or in a Cloud Storage JSON API path (github.com/acme,
// git@github.com:acme, s3://acme-bucket, arn:aws:s3:::acme-bucket,
// storage.googleapis.com/acme-bucket,
// www.googleapis.com/storage/v1/b/acme-bucket, api.github.com/repos/acme,
// abfss://data@acmeaccount..., oci://acme-bucket), and the customer names a
// provider resource path carries (a GCP project ID in projects/acme-prod, a
// bucket in projects/_/buckets/acme-bucket, an Azure storage account in an ARM
// ID's storageAccounts/acmeprod and the other globally unique Azure names an
// ARM ID carries (a Key Vault, a container registry, a Cosmos DB account, a
// SQL, PostgreSQL or MySQL server, a web app, a Service Bus namespace), an S3
// access point (Object Lambda included), S3 Express or Outposts bucket in an
// ARN) that are not documentation values.
// Only directories named exactly testdata are checked besides examples/ and
// docs/guide/, so a new fixture directory must be named testdata. The walk
// reads the tree on disk, so an untracked or git-ignored file under a checked
// directory is checked too: a real inventory dropped there locally fails before
// it is committed. A file is read whatever its encoding (UTF-16 with a
// byte-order mark is decoded, an invalid byte cannot hide the rest), and a
// symbolic link in a checked path is reported, never followed. Outside
// examples/ a dotted name is a host name only when its last label is a listed
// public top-level domain or private DNS suffix (a list that fails open, so
// additions are expected) and it is not an exact public vendor host, and the
// few legitimate values left (loopback addresses, synthetic IDs) are excused
// one file and one value at a time by a commented allow list.
//
// The package holds this file and _test.go files only, under the strict
// test-docs-are-pure depguard rule, so it imports nothing. Its tests run in
// make test, need no network, credentials or clock, and are table-driven and
// black-box (package examples_test).
package examples
