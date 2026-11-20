package examples_test

import (
	"regexp"
	"slices"
	"strings"
)

// projectPathPattern finds a GCP project ID in a resource path: the projects
// segment, matched case-insensitively, a slash and a project ID, which is six to
// thirty characters, opens with a lower-case letter and holds lower-case letters,
// digits and hyphens without ending in one. A Cloud Asset name
// (//compute.googleapis.com/projects/acme-corp-prod/zones/...), a selfLink
// (https://www.googleapis.com/compute/v1/projects/acme-corp-prod/...) and a bare
// resource name (projects/acme-corp-prod/locations/...) all carry it so. A
// project number (projects/123456789012), the reserved projects/_ of a Cloud
// Storage resource name, a one-letter jq placeholder (projects/p), an elided
// segment (projects/...) and a shell variable (projects/$project) are no project
// ID and never match. resourcePathProblems checks the bytes around the match, as
// Go's regexp has no look-around.
var projectPathPattern = regexp.MustCompile(`(?i:projects)/[a-z][a-z0-9-]{4,28}[a-z0-9]`)

// projectBucketPattern finds the bucket a Cloud Storage resource name names
// after its project segment, whatever that segment holds
// (projects/_/buckets/acme-prod-data, projects/example-project/buckets/...): the
// bucket segment and the bucket's name, as written, capitals included, since a
// name written with one is the customer's all the same. The submatch is the
// buckets segment and the name, the value reported.
var projectBucketPattern = regexp.MustCompile(`(?i:projects)/[^/\s"'` + "`" + `]+/((?i:buckets)/[A-Za-z0-9][A-Za-z0-9_.-]*)`)

// storageAccountPathPattern finds the Azure storage account an ARM ID names
// (/subscriptions/.../providers/Microsoft.Storage/storageAccounts/acmeprod): the
// storageAccounts segment and the account's name of three to twenty-four
// letters and digits. An ARM ID is case-insensitive, so the segment and the name
// are matched in any case (microsoft.storage/storageaccounts/acmeprod names the
// same account) and reported as written.
var storageAccountPathPattern = regexp.MustCompile(`(?i)storageAccounts/[a-z0-9]{3,24}`)

// storageAccountChildSegments are the child-resource types of a storage account
// that the segment after storageAccounts/ names in a resource type
// (Microsoft.Storage/storageAccounts/blobServices): each is the type's own word,
// no account's name. They are matched case-insensitively, as an ARM type is.
var storageAccountChildSegments = []string{
	"blobServices", "fileServices", "queueServices", "tableServices",
	"managementPolicies", "encryptionScopes", "inventoryPolicies",
	"localUsers", "privateEndpointConnections", "privateLinkResources",
	"objectReplicationPolicies",
}

// s3ResourceARNPattern finds the access point or the Outposts bucket an S3 ARN
// names, in any AWS partition (arn:aws:, arn:aws-cn:, arn:aws-us-gov:) and for
// any of the three services that name one: s3 (an access point,
// arn:aws:s3:eu-central-1:123456789012:accesspoint/acme-prod-ap), s3-object-lambda
// (an Object Lambda access point) and s3-outposts (an access point or a bucket
// after the outpost's own segment, .../outpost/op-0123.../bucket/acme-op-bucket).
// The submatch is the accesspoint or bucket segment and the name, the value
// reported; the outpost's identifier names no customer and is not reported.
var s3ResourceARNPattern = regexp.MustCompile(
	`(?i)\barn:aws[a-z-]*:s3(?:-object-lambda|-outposts)?:[a-z0-9-]*:[0-9]*:(?:outpost/[a-z0-9-]+/)?((?:accesspoint|bucket)/[a-z0-9][a-z0-9_.-]*)`)

// sampleResourcePattern matches the sample names a resource path holds that a
// sample's bucket name (sampleBucketPattern) does not cover, lowercased: the
// sample project, example-project, and the sample access point, example-ap,
// each optionally numbered (example-project-2).
var sampleResourcePattern = regexp.MustCompile(`^example-(?:project|ap)(?:-[0-9]{1,3})?$`)

// sampleResourceName reports whether name, as written, is a sample's name in a
// resource path: a sample bucket's (sampleBucket) or one of
// sampleResourcePattern's, matched case-insensitively.
func sampleResourceName(name string) bool {
	return sampleBucket(name) || sampleResourcePattern.MatchString(strings.ToLower(name))
}

// sampleStorageAccount reports whether account, a storage account's name as
// written, is a sample's: one that starts with example, the way a sample's
// bucket does (examplestorage), matched case-insensitively. A storage account's
// name holds no hyphen, so the generic words of a sample bucket cannot follow
// the prefix the way they do there.
func sampleStorageAccount(account string) bool {
	return strings.HasPrefix(strings.ToLower(account), "example")
}

// resourcePathProblems returns a problem for every customer name a provider
// resource path carries, the globally unique names the host name rule never
// reads because they carry no dot or sit in a path segment (issue #406): a GCP
// project ID (projectPathPattern), the bucket of a Cloud Storage resource name
// (projectBucketPattern), the storage account of an Azure ARM ID
// (storageAccountPathPattern) and the access point or Outposts bucket of an S3
// ARN (s3ResourceARNPattern). Each is held to its sample value: a project, a
// bucket or an access point must be a sample's (sampleResourceName: example,
// example- and generic words, example-project or example-ap), a storage account
// must start with example (sampleStorageAccount), and a storage account's child
// segment (storageAccounts/blobServices) is no account at all. The value reported
// is the segment and the name as the file writes them (projects/acme-corp-prod,
// buckets/acme-prod-data, storageAccounts/acmeprodcustomers,
// accesspoint/acme-prod-ap, bucket/acme-op-bucket), so the failure names the
// value to change.
//
// It knowingly leaves these gaps, accepted when issue #406 closed:
//
//   - a project ID in a path segment other than projects/ (a bare
//     "projectId": "acme-corp-prod" field, a gcloud --project flag), since a
//     bare word of that shape is any hyphenated name and cannot be told from one;
//   - a projects/ segment glued to a letter, digit, hyphen or underscore before
//     it (subprojects/acme-corp-prod) or after the ID (projects/acme-corp-prod_x),
//     which is no resource path's segment;
//   - a storage account outside an ARM ID (a bare "name": "acmeprodcustomers"),
//     for the same reason as a bare project ID; the account in a storage host
//     (acmeprod.blob.core.windows.net) is the host name rule's value, and the one
//     in an abfss or wasbs URL the vendor-path rule's;
//   - the names below a bucket, an account or an access point (an object key, a
//     container, a blob), which no rule judges, the way vendorPathProblems leaves
//     an object key after a sample bucket.
func resourcePathProblems(text string) []problem {
	var problems []problem

	for _, match := range projectPathPattern.FindAllStringIndex(text, -1) {
		start, end := match[0], match[1]

		if !resourceSegmentBounded(text, start, end) {
			continue
		}

		value := text[start:end]
		if sampleResourceName(value[strings.Index(value, "/")+1:]) {
			continue
		}

		problems = append(problems, problem{
			line:  lineOf(text, start),
			value: value,
			rule:  "names a GCP project ID that is not example, example- and generic words or example-project (CLAUDE.md rule 2)",
		})
	}

	for _, match := range projectBucketPattern.FindAllStringSubmatchIndex(text, -1) {
		value := strings.TrimRight(text[match[2]:match[3]], ".")

		if sampleBucket(value[strings.Index(value, "/")+1:]) {
			continue
		}

		problems = append(problems, problem{
			line:  lineOf(text, match[2]),
			value: value,
			rule:  "names a bucket that is not example or example- and generic words (CLAUDE.md rule 2)",
		})
	}

	for _, match := range storageAccountPathPattern.FindAllStringIndex(text, -1) {
		start, end := match[0], match[1]

		if !resourceSegmentBounded(text, start, end) {
			continue
		}

		value := text[start:end]
		account := value[strings.Index(value, "/")+1:]

		if sampleStorageAccount(account) ||
			slices.ContainsFunc(storageAccountChildSegments, func(child string) bool { return strings.EqualFold(account, child) }) {
			continue
		}

		problems = append(problems, problem{
			line:  lineOf(text, start),
			value: value,
			rule:  "names an Azure storage account that does not start with example (CLAUDE.md rule 2)",
		})
	}

	for _, match := range s3ResourceARNPattern.FindAllStringSubmatchIndex(text, -1) {
		value := strings.TrimRight(text[match[2]:match[3]], ".")

		if sampleResourceName(value[strings.Index(value, "/")+1:]) {
			continue
		}

		problems = append(problems, problem{
			line:  lineOf(text, match[2]),
			value: value,
			rule:  "names an S3 access point or Outposts bucket that is not example or example- and generic words (CLAUDE.md rule 2)",
		})
	}

	return problems
}

// resourceSegmentBounded reports whether the resource segment text[start:end]
// stands on its own: no letter, digit, hyphen or underscore right before it
// (subprojects/, mystorageAccounts/) or right after it, so the match is the
// whole segment and the whole name, not the start of a longer one
// (projects/acme-corp-prod_x, storageAccounts/blobServicesX).
func resourceSegmentBounded(text string, start, end int) bool {
	glued := func(b byte) bool { return labelByte(b) || b == '-' || b == '_' }

	if start > 0 && glued(text[start-1]) {
		return false
	}

	return end == len(text) || !glued(text[end])
}
