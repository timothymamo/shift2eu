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
// bucket names, a storage account whose name starts with example, while a
// project number, a child-resource segment (storageAccounts/blobServices) or an
// elided segment (projects/...) names nobody.
//
// The value is reported as the file writes it, since ARM IDs are
// case-insensitive and a lower-case resource type names the same account
// (microsoft.storage/storageaccounts/acmeprodcustomers). An S3 ARN is read in
// every partition (arn:aws:, arn:aws-us-gov:), and an Outposts or Object Lambda
// ARN names its access point or bucket in a path of its own, after the outpost.
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
			// A GCP project ID is six to thirty characters, starts with a
			// lower-case letter and holds lower-case letters, digits and hyphens
			// without ending in one: a one-letter segment is the jq placeholder
			// docs/guide/inventory/gcp.jq writes.
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
			// The sample storage account keeps the same example- prefix a sample
			// bucket does.
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
