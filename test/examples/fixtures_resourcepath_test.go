package examples_test

import (
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
// written with capitals (projects/Acme-Corp-Prod). An S3 ARN is read in every
// partition (arn:aws:, arn:aws-us-gov:), in the S3 Express service's own
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
