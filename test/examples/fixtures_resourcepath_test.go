package examples_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestResourcePathProblems pins which provider resource paths the fixture check
// reads for a customer's own name (issue #406, resourcePathProblems). A
// resource's path in GCP, Azure or AWS names a globally unique value of the
// customer's: a GCP project ID (projects/acme-corp-prod), a Cloud Storage bucket
// (buckets/acme-prod-data), an Azure storage account
// (storageAccounts/acmeprodcustomers) or an S3 access point, Outposts bucket or
// Object Lambda access point (accesspoint/acme-prod-ap). The host name rule
// reads none of them: a project ID and a storage account name carry no dot at
// all, and a bucket or an access point is a path segment after a host or inside
// an ARN. So this rule reports the segment that carries the name -
// projects/<id>, buckets/<name>, storageAccounts/<name>, accesspoint/<name>,
// bucket/<name> - and holds each to its provider's sample value: a GCP project
// ID that is example or example- and words, a bucket or an access point a sample
// bucket names, a storage account that is example and generic words
// (examplestorage, exampledata01), while a project number, a child-resource
// segment in a resource type (Microsoft.Storage/storageAccounts/blobServices),
// an RBAC action string's operation (Microsoft.Storage/storageAccounts/read) or
// an elided segment (projects/...) names nobody.
//
// The value is reported as the file writes it, since ARM IDs are
// case-insensitive and a lower-case resource type names the same account
// (microsoft.storage/storageaccounts/acmeprodcustomers), and so does a name
// written with capitals (projects/Acme-Corp-Prod). The other Azure names an ARM
// ID carries are globally unique too, each the DNS label of a public host of its
// own, so the same rule reads them (issue #424): a Key Vault
// (vaults/kv-contoso-prod), a container registry (registries/acrcontosoprod), a
// Cosmos DB account (databaseAccounts/cosmos-contoso-prod), a SQL, PostgreSQL or
// MySQL server (servers/sql-contoso-prod, servers/pg-contoso-prod,
// servers/mysql-contoso-prod, and a flexible server in flexibleServers/), a web
// app (sites/app-contoso-prod) and a Service Bus namespace
// (namespaces/sb-contoso-prod). Each is held to the shape its provider's naming
// rules give it, to the sample rule a storage account's name is held to
// (example and generic words, exampledata), and to an ARM ID's providers/
// segment before its namespace, so a resource type (a child type after a
// namespace, Microsoft.ServiceBus/namespaces/queues) and a placeholder too short
// for a name (namespaces/n in the guide's ARM ID comment) name nobody. An S3 ARN
// is read in every partition (arn:aws:, arn:aws-us-gov:), in the S3 Express service's own
// spelling (arn:aws:s3express:...) and with either delimiter the ARN may use
// (accesspoint/acme-prod-ap, accesspoint:acme-prod-ap), and an Outposts or
// Object Lambda ARN names its access point or bucket in a path of its own, after
// the outpost.
func TestResourcePathProblems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []string
	}{
		// Reported: a resource path that names the customer's own project,
		// bucket, storage account or access point.
		{
			// A Cloud Asset name opens with the service host, and the project
			// ID after it is globally unique.
			name: "customer project ID in a Cloud Asset name",
			text: `"name": "//compute.googleapis.com/projects/acme-corp-prod/zones/europe-west1-b/instances/vm-1"`,
			want: []string{"projects/acme-corp-prod"},
		},
		{
			name: "customer project ID in a compute selfLink",
			text: `"selfLink": "https://www.googleapis.com/compute/v1/projects/acme-corp-prod/global/networks/default"`,
			want: []string{"projects/acme-corp-prod"},
		},
		{
			// A resource name in the bare form, as the Cloud APIs return it and
			// as a jq path reads it.
			name: "customer project ID in a resource name",
			text: `"name": "projects/acme-corp-prod/locations/europe-west1"`,
			want: []string{"projects/acme-corp-prod"},
		},
		{
			// A Cloud Storage resource name carries the bucket after the
			// reserved projects/_ segment, which names no project.
			name: "customer bucket in a Cloud Storage resource name",
			text: `"name": "projects/_/buckets/acme-prod-data"`,
			want: []string{"buckets/acme-prod-data"},
		},
		{
			// The sample project is clean and the bucket beside it is not: the
			// two segments are judged separately.
			name: "customer bucket under the sample project",
			text: "the object is projects/example-project/buckets/acme-prod-data",
			want: []string{"buckets/acme-prod-data"},
		},
		{
			// The sample subscription GUID identifies nobody, and the storage
			// account in the ARM ID under it does: it is globally unique and the
			// customer's own name.
			name: "customer storage account in an ARM ID",
			text: "/subscriptions/" + dummyGUID + "/resourceGroups/rg-shop/providers/Microsoft.Storage/storageAccounts/acmeprodcustomers",
			want: []string{"storageAccounts/acmeprodcustomers"},
		},
		{
			// An ARM ID is case-insensitive, so a lower-case resource type names
			// the same customer: the value is reported as the file writes it.
			name: "customer storage account in a lower-case ARM ID",
			text: "/subscriptions/" + dummyGUID + "/resourceGroups/rg-shop/providers/microsoft.storage/storageaccounts/acmeprodcustomers",
			want: []string{"storageaccounts/acmeprodcustomers"},
		},
		{
			// An S3 access point is an ARN of its own shape, with a region and an
			// account the bucket ARN's ::: never carries.
			name: "customer access point in an S3 ARN",
			text: `"accessPoint": "arn:aws:s3:eu-central-1:` + dummyAccountID + `:accesspoint/acme-prod-ap"`,
			want: []string{"accesspoint/acme-prod-ap"},
		},
		{
			name: "customer access point in an S3 Object Lambda ARN",
			text: `"accessPoint": "arn:aws:s3-object-lambda:eu-central-1:` + dummyAccountID + `:accesspoint/acme-olap"`,
			want: []string{"accesspoint/acme-olap"},
		},
		{
			// An Outposts ARN names its access point after the outpost's own
			// segment, whose identifier is no customer's name.
			name: "customer access point in an S3 Outposts ARN",
			text: `"accessPoint": "arn:aws:s3-outposts:eu-central-1:` + dummyAccountID + `:outpost/op-01234567890abcdef/accesspoint/acme-op-ap"`,
			want: []string{"accesspoint/acme-op-ap"},
		},
		{
			name: "customer bucket in an S3 Outposts ARN",
			text: `"bucket": "arn:aws:s3-outposts:eu-central-1:` + dummyAccountID + `:outpost/op-01234567890abcdef/bucket/acme-op-bucket"`,
			want: []string{"bucket/acme-op-bucket"},
		},
		{
			// The ARN's partition varies (arn:aws:, arn:aws-cn:, arn:aws-us-gov:)
			// and the access point it names is the customer's in every one.
			name: "customer access point in an S3 ARN of another partition",
			text: `"accessPoint": "arn:aws-us-gov:s3:us-gov-west-1:` + dummyAccountID + `:accesspoint/acme-ap"`,
			want: []string{"accesspoint/acme-ap"},
		},
		{
			// A sample's storage account carries the example prefix a sample's
			// bucket's name starts with and then names generic words
			// (examplestorage), so the prefix alone is no sample: a name that
			// follows it without a generic word between (exampleacmeprod) is the
			// customer's own, the way the bucket example-acme-prod is.
			name: "customer storage account behind an example- prefix",
			text: "/subscriptions/" + dummyGUID + "/resourceGroups/rg-shop/providers/Microsoft.Storage/storageAccounts/exampleacmeprod",
			want: []string{"storageAccounts/exampleacmeprod"},
		},
		{
			name: "customer storage account behind an example- prefix and a customer word",
			text: "/subscriptions/" + dummyGUID + "/resourceGroups/rg-shop/providers/Microsoft.Storage/storageAccounts/exampleacmecustomers",
			want: []string{"storageAccounts/exampleacmecustomers"},
		},
		{
			// In an ARM ID the segment after storageAccounts/ is the account's
			// own name, whatever it spells: an account named after a child
			// resource's word (blobservices) is no child resource, and the child
			// type the ID writes after it (blobServices/default) is the one that
			// names a child.
			name: "customer storage account named like a child resource",
			text: "/subscriptions/" + dummyGUID + "/resourceGroups/rg-shop/providers/Microsoft.Storage/storageAccounts/blobservices/blobServices/default",
			want: []string{"storageAccounts/blobservices"},
		},
		// The other Azure names an ARM ID carries are globally unique too, each
		// the DNS label of a public host of its own (<name>.vault.azure.net,
		// <name>.azurecr.io, <name>.documents.azure.com,
		// <name>.database.windows.net, <name>.postgres.database.azure.com,
		// <name>.mysql.database.azure.com, <name>.azurewebsites.net,
		// <name>.servicebus.windows.net), so the segment after the resource type
		// names the customer as directly as a host would (issue #424). The type
		// segment and the name are reported as the file writes them, since an
		// ARM ID's provider and type are case-insensitive, and each type's name
		// is held to the shape its provider's naming rules give it.
		{
			name: "customer Key Vault in an ARM ID",
			text: testARMID("Microsoft.KeyVault", "vaults", "kv-contoso-prod"),
			want: []string{"vaults/kv-contoso-prod"},
		},
		{
			name: "customer Key Vault in a lower-case ARM ID",
			text: testARMID("microsoft.keyvault", "VAULTS", "kv-contoso-prod"),
			want: []string{"VAULTS/kv-contoso-prod"},
		},
		{
			// A secret is a child resource of the vault, and only the vault's own
			// name is the customer's: the child segment reports nothing.
			name: "customer Key Vault with a secret after its name",
			text: testARMID("Microsoft.KeyVault", "vaults", "kv-contoso-prod") + "/secrets/db-password",
			want: []string{"vaults/kv-contoso-prod"},
		},
		{
			name: "customer container registry in an ARM ID",
			text: testARMID("Microsoft.ContainerRegistry", "registries", "acrcontosoprod"),
			want: []string{"registries/acrcontosoprod"},
		},
		{
			name: "customer container registry in a lower-case ARM ID",
			text: testARMID("microsoft.containerregistry", "REGISTRIES", "acrcontosoprod"),
			want: []string{"REGISTRIES/acrcontosoprod"},
		},
		{
			// A registry task is a child resource of the registry.
			name: "customer container registry with a task after its name",
			text: testARMID("Microsoft.ContainerRegistry", "registries", "acrcontosoprod") + "/tasks/build",
			want: []string{"registries/acrcontosoprod"},
		},
		{
			name: "customer Cosmos DB account in an ARM ID",
			text: testARMID("Microsoft.DocumentDB", "databaseAccounts", "cosmos-contoso-prod"),
			want: []string{"databaseAccounts/cosmos-contoso-prod"},
		},
		{
			name: "customer Cosmos DB account in a lower-case ARM ID",
			text: testARMID("microsoft.documentdb", "DATABASEACCOUNTS", "cosmos-contoso-prod"),
			want: []string{"DATABASEACCOUNTS/cosmos-contoso-prod"},
		},
		{
			// A database is a child resource of the account.
			name: "customer Cosmos DB account with a database after its name",
			text: testARMID("Microsoft.DocumentDB", "databaseAccounts", "cosmos-contoso-prod") + "/sqlDatabases/shop",
			want: []string{"databaseAccounts/cosmos-contoso-prod"},
		},
		{
			name: "customer SQL server in an ARM ID",
			text: testARMID("Microsoft.Sql", "servers", "sql-contoso-prod"),
			want: []string{"servers/sql-contoso-prod"},
		},
		{
			name: "customer SQL server in a lower-case ARM ID",
			text: testARMID("microsoft.sql", "SERVERS", "sql-contoso-prod"),
			want: []string{"SERVERS/sql-contoso-prod"},
		},
		{
			// A database is a child resource of the server.
			name: "customer SQL server with a database after its name",
			text: testARMID("Microsoft.Sql", "servers", "sql-contoso-prod") + "/databases/shop",
			want: []string{"servers/sql-contoso-prod"},
		},
		{
			// A single server's name is global; the servers type is the same
			// segment under the PostgreSQL namespace as under the SQL one, and
			// the namespace is what tells the provider's name from SQL's.
			name: "customer PostgreSQL server in an ARM ID",
			text: testARMID("Microsoft.DBforPostgreSQL", "servers", "pg-contoso-prod"),
			want: []string{"servers/pg-contoso-prod"},
		},
		{
			name: "customer PostgreSQL server in a lower-case ARM ID",
			text: testARMID("microsoft.dbforpostgresql", "SERVERS", "pg-contoso-prod"),
			want: []string{"SERVERS/pg-contoso-prod"},
		},
		{
			name: "customer PostgreSQL server with a database after its name",
			text: testARMID("Microsoft.DBforPostgreSQL", "servers", "pg-contoso-prod") + "/databases/shop",
			want: []string{"servers/pg-contoso-prod"},
		},
		{
			// A flexible server is the same globally unique name in a type of
			// its own, whose segment ends in the single server's (flexibleServers
			// holds servers): the type before the name is what is reported.
			name: "customer PostgreSQL flexible server in an ARM ID",
			text: testARMID("Microsoft.DBforPostgreSQL", "flexibleServers", "pg-contoso-prod"),
			want: []string{"flexibleServers/pg-contoso-prod"},
		},
		{
			name: "customer PostgreSQL flexible server in a lower-case ARM ID",
			text: testARMID("microsoft.dbforpostgresql", "FLEXIBLESERVERS", "pg-contoso-prod"),
			want: []string{"FLEXIBLESERVERS/pg-contoso-prod"},
		},
		{
			name: "customer PostgreSQL flexible server with a database after its name",
			text: testARMID("Microsoft.DBforPostgreSQL", "flexibleServers", "pg-contoso-prod") + "/databases/shop",
			want: []string{"flexibleServers/pg-contoso-prod"},
		},
		{
			name: "customer MySQL server in an ARM ID",
			text: testARMID("Microsoft.DBforMySQL", "servers", "mysql-contoso-prod"),
			want: []string{"servers/mysql-contoso-prod"},
		},
		{
			name: "customer MySQL server in a lower-case ARM ID",
			text: testARMID("microsoft.dbformysql", "SERVERS", "mysql-contoso-prod"),
			want: []string{"SERVERS/mysql-contoso-prod"},
		},
		{
			name: "customer MySQL server with a database after its name",
			text: testARMID("Microsoft.DBforMySQL", "servers", "mysql-contoso-prod") + "/databases/shop",
			want: []string{"servers/mysql-contoso-prod"},
		},
		{
			name: "customer MySQL flexible server in an ARM ID",
			text: testARMID("Microsoft.DBforMySQL", "flexibleServers", "mysql-contoso-prod"),
			want: []string{"flexibleServers/mysql-contoso-prod"},
		},
		{
			name: "customer MySQL flexible server in a lower-case ARM ID",
			text: testARMID("microsoft.dbformysql", "FLEXIBLESERVERS", "mysql-contoso-prod"),
			want: []string{"FLEXIBLESERVERS/mysql-contoso-prod"},
		},
		{
			name: "customer MySQL flexible server with a database after its name",
			text: testARMID("Microsoft.DBforMySQL", "flexibleServers", "mysql-contoso-prod") + "/databases/shop",
			want: []string{"flexibleServers/mysql-contoso-prod"},
		},
		{
			name: "customer web app in an ARM ID",
			text: testARMID("Microsoft.Web", "sites", "app-contoso-prod"),
			want: []string{"sites/app-contoso-prod"},
		},
		{
			name: "customer web app in a lower-case ARM ID",
			text: testARMID("microsoft.web", "SITES", "app-contoso-prod"),
			want: []string{"SITES/app-contoso-prod"},
		},
		{
			// A slot is a child resource of the site, and a site's own name is
			// the one the DNS label carries.
			name: "customer web app with a slot after its name",
			text: testARMID("Microsoft.Web", "sites", "app-contoso-prod") + "/slots/staging",
			want: []string{"sites/app-contoso-prod"},
		},
		{
			name: "customer Service Bus namespace in an ARM ID",
			text: testARMID("Microsoft.ServiceBus", "namespaces", "sb-contoso-prod"),
			want: []string{"namespaces/sb-contoso-prod"},
		},
		{
			name: "customer Service Bus namespace in a lower-case ARM ID",
			text: testARMID("microsoft.servicebus", "NAMESPACES", "sb-contoso-prod"),
			want: []string{"NAMESPACES/sb-contoso-prod"},
		},
		{
			// A queue is a child resource of the namespace.
			name: "customer Service Bus namespace with a queue after its name",
			text: testARMID("Microsoft.ServiceBus", "namespaces", "sb-contoso-prod") + "/queues/orders",
			want: []string{"namespaces/sb-contoso-prod"},
		},
		{
			// The inventory guide's own shop names are excused by an exact-name
			// rule (guideShopNamePattern: app-shop-storefront), never by a shape
			// such as any words after shop, so a customer's web app named the
			// same way with their own words after them is still reported.
			name: "customer web app named like the guide's shop",
			text: testARMID("Microsoft.Web", "sites", "app-shop-acme-prod"),
			want: []string{"sites/app-shop-acme-prod"},
		},
		{
			// The same exact-name rule holds for the guide's PostgreSQL
			// flexible server: psql-shop is the guide's, and a customer's
			// psql-shop-prod is no name the guide writes.
			name: "customer flexible server named like the guide's shop",
			text: testARMID("Microsoft.DBforPostgreSQL", "flexibleServers", "psql-shop-prod"),
			want: []string{"flexibleServers/psql-shop-prod"},
		},
		{
			// The rule matches the guide's name to its end, so a customer's name
			// that only opens with it (app-shop-storefront2) is their own too:
			// the excused names are the guide's, not a prefix of them.
			name: "customer web app whose name opens with the guide's shop name",
			text: testARMID("Microsoft.Web", "sites", "app-shop-storefront2"),
			want: []string{"sites/app-shop-storefront2"},
		},
		{
			// A directory bucket's ARN is the S3 Express service's (s3express),
			// and the bucket it names carries the zone and the suffix the service
			// appends (...--euc1-az1--x-s3) after the customer's own words, which
			// are reported with it.
			name: "customer bucket in an S3 Express ARN",
			text: `"bucket": "arn:aws:s3express:eu-central-1:` + dummyAccountID + `:bucket/acme-prod--euc1-az1--x-s3"`,
			want: []string{"bucket/acme-prod--euc1-az1--x-s3"},
		},
		{
			// The zone suffix (...--euc1-az1--x-s3) is stripped from the end
			// of a directory bucket's name, and a customer's own words may
			// hold a double hyphen of their own before it
			// (example--acme-prod--euc1-az1--x-s3): the whole name is
			// reported, since only the one suffix at the end belongs to the
			// service and the words before it are no sample's.
			name: "customer directory bucket whose words hold a double hyphen",
			text: `"bucket": "arn:aws:s3express:eu-central-1:` + dummyAccountID + `:bucket/example--acme-prod--euc1-az1--x-s3"`,
			want: []string{"bucket/example--acme-prod--euc1-az1--x-s3"},
		},
		{
			// Only a zone's own ID sits in the zone slot of a directory
			// bucket's suffix, and a customer's words there
			// (example--acme-prod--x-s3) are no zone: they are stripped from
			// the end of no name, so the whole name is reported.
			name: "customer words in a directory bucket's zone slot",
			text: `"bucket": "arn:aws:s3express:eu-central-1:` + dummyAccountID + `:bucket/example--acme-prod--x-s3"`,
			want: []string{"bucket/example--acme-prod--x-s3"},
		},
		{
			// A single customer word in the zone slot (example--acme--x-s3) is
			// no availability or local zone's ID either, so nothing is stripped
			// and the name behind no sample's words is reported.
			name: "customer word in a directory bucket's zone slot",
			text: `"bucket": "arn:aws:s3express:eu-central-1:` + dummyAccountID + `:bucket/example--acme--x-s3"`,
			want: []string{"bucket/example--acme--x-s3"},
		},
		{
			// The zone suffix belongs to the s3express service's directory
			// buckets, never to an access point's name, which is the
			// customer's own in full even when it ends in the same letters
			// (example-ap--euc1-az1--x-s3): nothing is stripped from it.
			name: "access point whose name ends in the directory bucket suffix",
			text: `"accessPoint": "arn:aws:s3:eu-central-1:` + dummyAccountID + `:accesspoint/example-ap--euc1-az1--x-s3"`,
			want: []string{"accesspoint/example-ap--euc1-az1--x-s3"},
		},
		{
			// An ARN that delimits the segment with a colon rather than a slash
			// names the same access point, and the value is reported as the file
			// writes it, the way an ARM ID's account is.
			name: "customer access point in an S3 ARN written with a colon",
			text: `"accessPoint": "arn:aws:s3:eu-central-1:` + dummyAccountID + `:accesspoint:acme-prod-ap"`,
			want: []string{"accesspoint:acme-prod-ap"},
		},
		{
			// A GCP project ID is written in lower case, and a name written with
			// capitals (Acme-Corp-Prod) is the customer's all the same, so it is
			// reported as written rather than passed over.
			name: "customer project ID in capitals",
			text: `"name": "//compute.googleapis.com/projects/Acme-Corp-Prod/zones/z/instances/vm-1"`,
			want: []string{"projects/Acme-Corp-Prod"},
		},
		{
			// The sample names do not cross: example-ap is the sample access
			// point's name, and a project ID of that shape is the customer's
			// own, which the project rule reports.
			name: "project ID named like the sample access point",
			text: "projects/example-ap/zones/z",
			want: []string{"projects/example-ap"},
		},
		{
			// And the other way round: example-project is the sample project's
			// name, and an access point named after it is no sample's access
			// point (example-ap is), so the ARN's access point is reported.
			name: "access point named like the sample project",
			text: `"accessPoint": "arn:aws:s3:eu-central-1:` + dummyAccountID + `:accesspoint/example-project"`,
			want: []string{"accesspoint/example-project"},
		},

		// Not reported: a sample value, a segment that names no customer, or a
		// projects/ segment that is no project ID.
		{
			// The sample project every fixture uses is named the way a sample's
			// bucket is.
			name: "sample project in a Cloud Asset name",
			text: `"name": "//compute.googleapis.com/projects/example-project/zones/europe-west1-b/instances/shop-web-1"`,
			want: nil,
		},
		{
			name: "sample project with a number suffix",
			text: "the instance is projects/example-project-2/zones/europe-west1-b/instances/vm-1",
			want: nil,
		},
		{
			// A GCP project ID is six to thirty characters, opens with a letter
			// and holds letters, digits and hyphens without ending in one: a
			// one-letter segment is the placeholder docs/guide/inventory/gcp.jq
			// writes.
			name: "project segment too short for a project ID",
			text: "the asset name is //compute.googleapis.com/projects/p/zones/z/instances/n",
			want: nil,
		},
		{
			// A shell variable names the caller's project, not one of its own.
			name: "project segment that is a shell variable",
			text: `gcloud asset search-all-resources --scope="projects/$project"`,
			want: nil,
		},
		{
			name: "sample bucket in a Cloud Storage resource name",
			text: `"name": "projects/_/buckets/example-bucket"`,
			want: nil,
		},
		{
			// A GCP project number is a number, not a project ID, and the
			// sanitised one identifies nobody.
			name: "project number rather than a project ID",
			text: `"projectNumber": "projects/` + dummyAccountID + `"`,
			want: nil,
		},
		{
			name: "elided project segment in prose",
			text: `{"name": "projects/.../subscriptions/.."} is the shape of a Pub/Sub name`,
			want: nil,
		},
		{
			name: "elided project segment in a jq path",
			text: `test("projects/.") is the parent path`,
			want: nil,
		},
		{
			// blobServices and queueServices are child resources of a storage
			// account, and the guides name them in the resource type
			// (Microsoft.Storage/storageAccounts/blobServices).
			name: "child-resource segment after a storage account",
			text: `{"type": "Microsoft.Storage/storageAccounts/blobServices"}`,
			want: nil,
		},
		{
			name: "queue child-resource segment after a storage account",
			text: `{"type": "Microsoft.Storage/storageAccounts/queueServices"}`,
			want: nil,
		},
		{
			// The sample storage account carries the same example prefix a sample
			// bucket's name starts with.
			name: "sample storage account in an ARM ID",
			text: "/subscriptions/" + dummyGUID + "/resourceGroups/rg-shop/providers/Microsoft.Storage/storageAccounts/examplestorage",
			want: nil,
		},
		{
			name: "sample access point in an S3 ARN",
			text: `"accessPoint": "arn:aws:s3:eu-central-1:` + dummyAccountID + `:accesspoint/example-ap"`,
			want: nil,
		},
		{
			// A GitHub project is a number in a path, not a provider's resource
			// path: it is no name of anyone's.
			name: "GitHub project URL",
			text: "the board is at https://github.com/orgs/acme/projects/1",
			want: nil,
		},
		{
			// containers is blobServices' own child type, and the two name a
			// resource type in a guide rather than a resource in an ID: no
			// segment of it is an account's name.
			name: "container child-resource type after a storage account",
			text: `{"type": "Microsoft.Storage/storageAccounts/blobServices/containers"}`,
			want: nil,
		},
		{
			// An ARM RBAC action string names a resource type and the operation
			// to allow on it (Microsoft.Storage/storageAccounts/read), so the
			// operation sits where an account's name sits in an ARM ID and names
			// nobody. The type before it carries no /providers/ segment, and that
			// segment is what makes the one after storageAccounts/ an account.
			name: "ARM RBAC read action string",
			text: `"actions": ["Microsoft.Storage/storageAccounts/read"]`,
			want: nil,
		},
		{
			name: "ARM RBAC write action string",
			text: `"actions": ["Microsoft.Storage/storageAccounts/write"]`,
			want: nil,
		},
		{
			name: "ARM RBAC delete action string",
			text: `"actions": ["Microsoft.Storage/storageAccounts/delete"]`,
			want: nil,
		},
		{
			// A data-plane action writes the operation and an /action suffix
			// after the type (listKeys/action), and neither is an account's name.
			name: "ARM RBAC list-keys action string",
			text: `"actions": ["Microsoft.Storage/storageAccounts/listKeys/action"]`,
			want: nil,
		},
		{
			name: "ARM RBAC regenerate-key action string",
			text: `"actions": ["Microsoft.Storage/storageAccounts/regenerateKey/action"]`,
			want: nil,
		},
		{
			name: "ARM RBAC list-account-SAS action string",
			text: `"actions": ["Microsoft.Storage/storageAccounts/listAccountSas/action"]`,
			want: nil,
		},
		{
			// The generic word after the prefix is what names a sample's account:
			// example and storage, the way a storage account is named.
			name: "sample storage account named after its service",
			text: `"name": "storageAccounts/examplestorage"`,
			want: nil,
		},
		{
			// The digits after a generic word are a count (example-logs-2), so
			// generic words and a count of up to three stay clean.
			name: "sample storage account with a generic word and a count",
			text: `"name": "storageAccounts/exampledata01"`,
			want: nil,
		},
		{
			// An S3 Express bucket's name appends the zone and the suffix the
			// service writes (...--euc1-az1--x-s3) to the bucket's own words, so
			// the part before the suffix is what is held to the sample rule, and
			// a sample's words there keep the name clean.
			name: "sample directory bucket in an S3 Express ARN",
			text: `"bucket": "arn:aws:s3express:eu-central-1:` + dummyAccountID + `:bucket/example-bucket--euc1-az1--x-s3"`,
			want: nil,
		},
		{
			// A local zone's ID is longer than an availability zone's
			// (usw2-lax1-az1), and the suffix the service appends to it ends
			// the same way: a sample's bucket's words before it keep the name
			// clean.
			name: "sample directory bucket in a local zone",
			text: `"bucket": "arn:aws:s3express:eu-central-1:` + dummyAccountID + `:bucket/example-bucket--usw2-lax1-az1--x-s3"`,
			want: nil,
		},
		{
			// The sample rule a storage account is held to, example and generic
			// words (exampledata), names no customer for the other Azure types
			// either: it is the one rule every one of these names is held to.
			name: "sample Key Vault name in an ARM ID",
			text: testARMID("Microsoft.KeyVault", "vaults", "exampledata"),
			want: nil,
		},
		{
			name: "sample container registry name in an ARM ID",
			text: testARMID("Microsoft.ContainerRegistry", "registries", "exampledata"),
			want: nil,
		},
		{
			name: "sample Cosmos DB account name in an ARM ID",
			text: testARMID("Microsoft.DocumentDB", "databaseAccounts", "exampledata"),
			want: nil,
		},
		{
			name: "sample SQL server name in an ARM ID",
			text: testARMID("Microsoft.Sql", "servers", "exampledata"),
			want: nil,
		},
		{
			name: "sample PostgreSQL server name in an ARM ID",
			text: testARMID("Microsoft.DBforPostgreSQL", "servers", "exampledata"),
			want: nil,
		},
		{
			name: "sample PostgreSQL flexible server name in an ARM ID",
			text: testARMID("Microsoft.DBforPostgreSQL", "flexibleServers", "exampledata"),
			want: nil,
		},
		{
			name: "sample MySQL server name in an ARM ID",
			text: testARMID("Microsoft.DBforMySQL", "servers", "exampledata"),
			want: nil,
		},
		{
			name: "sample MySQL flexible server name in an ARM ID",
			text: testARMID("Microsoft.DBforMySQL", "flexibleServers", "exampledata"),
			want: nil,
		},
		{
			name: "sample web app name in an ARM ID",
			text: testARMID("Microsoft.Web", "sites", "exampledata"),
			want: nil,
		},
		{
			name: "sample Service Bus namespace name in an ARM ID",
			text: testARMID("Microsoft.ServiceBus", "namespaces", "exampledata"),
			want: nil,
		},
		{
			// A resource type written with a name after it in a path is no ARM
			// ID: with no providers/ segment before the namespace, the segment
			// after the type names no resource of anyone's, the way the
			// storage-account rows leave an RBAC action string alone.
			name: "Key Vault name outside an ARM ID",
			text: `"type": "Microsoft.KeyVault/vaults/kv-contoso-prod"`,
			want: nil,
		},
		{
			name: "container registry name outside an ARM ID",
			text: `"type": "Microsoft.ContainerRegistry/registries/acrcontosoprod"`,
			want: nil,
		},
		{
			name: "Cosmos DB account name outside an ARM ID",
			text: `"type": "Microsoft.DocumentDB/databaseAccounts/cosmos-contoso-prod"`,
			want: nil,
		},
		{
			name: "SQL server name outside an ARM ID",
			text: `"type": "Microsoft.Sql/servers/sql-contoso-prod"`,
			want: nil,
		},
		{
			name: "PostgreSQL server name outside an ARM ID",
			text: `"type": "Microsoft.DBforPostgreSQL/servers/pg-contoso-prod"`,
			want: nil,
		},
		{
			name: "PostgreSQL flexible server name outside an ARM ID",
			text: `"type": "Microsoft.DBforPostgreSQL/flexibleServers/pg-contoso-prod"`,
			want: nil,
		},
		{
			name: "MySQL server name outside an ARM ID",
			text: `"type": "Microsoft.DBforMySQL/servers/mysql-contoso-prod"`,
			want: nil,
		},
		{
			name: "MySQL flexible server name outside an ARM ID",
			text: `"type": "Microsoft.DBforMySQL/flexibleServers/mysql-contoso-prod"`,
			want: nil,
		},
		{
			name: "web app name outside an ARM ID",
			text: `"type": "Microsoft.Web/sites/app-contoso-prod"`,
			want: nil,
		},
		{
			name: "Service Bus namespace name outside an ARM ID",
			text: `"type": "Microsoft.ServiceBus/namespaces/sb-contoso-prod"`,
			want: nil,
		},
		{
			// The guides write the namespace's child-resource types after the
			// namespace segment (Microsoft.ServiceBus/namespaces/queues,
			// .../topics), the way they write a storage account's
			// (Microsoft.Storage/storageAccounts/blobServices). With no
			// providers/ segment before them, they name a type rather than a
			// namespace, so neither is a customer's name.
			name: "queue child-resource type after a Service Bus namespace",
			text: `"rawType": "Microsoft.ServiceBus/namespaces/queues"`,
			want: nil,
		},
		{
			name: "topic child-resource type after a Service Bus namespace",
			text: `"rawType": "Microsoft.ServiceBus/namespaces/topics"`,
			want: nil,
		},
		{
			// The one-letter placeholder docs/guide/inventory/azure.jq writes in
			// an ARM ID's shape is no namespace's name: a Service Bus namespace's
			// name is six to fifty characters, so the placeholder identifies
			// nobody and the guide's own page stays clean.
			name: "namespace segment too short for a namespace",
			text: "# /subscriptions/s/resourceGroups/g/providers/Microsoft.ServiceBus/namespaces/n/queues/q",
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, values(resourcePathProblems(tc.text))); diff != "" {
				t.Errorf("resourcePathProblems(%q) mismatch (-want +got):\n%s", tc.text, diff)
			}
		})
	}
}

// testARMID returns the ARM ID a row plants: the sample subscription GUID, a
// resource group, and the provider namespace, resource type and name the row
// gives. The parts are written as given, so a row writes a lower-case provider
// and type where it pins that an ARM ID's case does not matter.
func testARMID(provider, resourceType, name string) string {
	return "/subscriptions/" + dummyGUID + "/resourceGroups/rg-acme/providers/" + provider + "/" + resourceType + "/" + name
}

// TestARMIDAccountDoesNotAllocate pins that reading the segment before an ARM
// ID's storageAccounts segment costs no copy: armIDAccount compares only the
// bytes just before the match (strings.EqualFold over the providers segment) and
// never lowercases the text that precedes it. Lowercasing it copies the whole
// prefix on every match, which makes a check over a Resource Graph export or a
// long guide page quadratic in its length (issue #424, from PR #418's review).
//
// The text is ten thousand bytes of the capitalised prose a guide page holds,
// with no storageAccounts segment in it and the one ARM ID at its end, so the
// per-match copy is as large as it gets. The test does not run in parallel with
// the others: AllocsPerRun counts the process's allocations, so another test's
// work during the measured runs would be counted here too.
func TestARMIDAccountDoesNotAllocate(t *testing.T) {
	text := strings.Repeat("Microsoft Azure Resource Group. ", 300) +
		"/subscriptions/" + dummyGUID + "/resourceGroups/rg-acme/providers/Microsoft.Storage/storageAccounts/acmeprod"

	start := strings.Index(text, "storageAccounts/")
	if start < len("providers/microsoft.storage/") {
		t.Fatalf("the text holds no storageAccounts segment with the providers segment before it (start %d), so it pins no ARM ID", start)
	}

	if !armIDAccount(text, start) {
		t.Fatalf("armIDAccount(0:%d, %d) is false for the storageAccounts segment of an ARM ID, so the case measured is not the one the reported copy costs", len(text), start)
	}

	// The result of each run is observed, since a call the compiler can drop is
	// no measurement of the copy that call makes.
	var misses int

	allocs := testing.AllocsPerRun(100, func() {
		if !armIDAccount(text, start) {
			misses++
		}
	})

	if misses != 0 {
		t.Fatalf("armIDAccount over the ARM ID at %d of %d bytes reports false in %d of the 100 measured runs, so no allocation is measured", start, len(text), misses)
	}

	if allocs != 0 {
		t.Errorf("armIDAccount over a %d-byte text with the match at %d allocates %.1f times per call, want 0: it copies and lowercases everything before the match, which is quadratic over a long file (issue #424)",
			len(text), start, allocs)
	}
}
