package examples_test

import (
	"regexp"
	"slices"
	"strings"
)

// projectPathPattern finds a GCP project ID in a resource path: the projects
// segment, matched case-insensitively, a slash and a project ID, which is six to
// thirty characters, opens with a letter and holds letters, digits and hyphens
// without ending in one. A project ID is written in lower case, yet one written
// with capitals (projects/Acme-Corp-Prod) names the customer all the same, so
// the ID is matched in any case and reported as written. A Cloud Asset name
// (//compute.googleapis.com/projects/acme-corp-prod/zones/...), a selfLink
// (https://www.googleapis.com/compute/v1/projects/acme-corp-prod/...) and a bare
// resource name (projects/acme-corp-prod/locations/...) all carry it so. A
// project number (projects/123456789012), the reserved projects/_ of a Cloud
// Storage resource name, a one-letter jq placeholder (projects/p), an elided
// segment (projects/...) and a shell variable (projects/$project) are no project
// ID and never match. resourcePathProblems checks the bytes around the match, as
// Go's regexp has no look-around.
var projectPathPattern = regexp.MustCompile(`(?i)projects/[a-z][a-z0-9-]{4,28}[a-z0-9]`)

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

// storageAccountTypeWords are the words the segment after storageAccounts/
// holds in a resource type or an RBAC action string rather than in an ARM ID:
// the child-resource types of a storage account
// (Microsoft.Storage/storageAccounts/blobServices) and the operations an action
// string allows on one (Microsoft.Storage/storageAccounts/read,
// .../listKeys/action). Each is the type's or the operation's own word, no
// account's name. They are matched case-insensitively, as an ARM type is, and
// only outside an ARM ID (armIDAccount): in an ID the segment is the account's
// name, whatever it spells.
var storageAccountTypeWords = []string{
	// Child-resource types.
	"blobServices", "fileServices", "queueServices", "tableServices",
	"managementPolicies", "encryptionScopes", "inventoryPolicies",
	"localUsers", "privateEndpointConnections", "privateLinkResources",
	"objectReplicationPolicies",
	// RBAC operations.
	"read", "write", "delete", "action", "join",
	"listKeys", "regenerateKey", "listAccountSas", "listServiceSas",
	"failover", "restoreBlobRanges", "revokeUserDelegationKeys",
}

// armIDProvidersSegment is the segment that opens the storage account's part
// of an ARM ID (/subscriptions/.../providers/Microsoft.Storage/storageAccounts/acmeprod),
// lowercased. A resource type or an RBAC action string carries no providers/
// segment before the namespace, and that segment is what makes the one after
// storageAccounts/ an account's name.
const armIDProvidersSegment = "providers/microsoft.storage/"

// armIDAccount reports whether the storageAccounts segment at text[start:]
// sits in an ARM ID: whether the providers segment and the Microsoft.Storage
// namespace (armIDProvidersSegment) come right before it, in any case.
func armIDAccount(text string, start int) bool {
	return strings.HasSuffix(strings.ToLower(text[:start]), armIDProvidersSegment)
}

// s3ResourceARNPattern finds the access point or the bucket an S3 ARN names
// after its region and account, in any AWS partition (arn:aws:, arn:aws-cn:,
// arn:aws-us-gov:) and for any of the four services that name one: s3 (an
// access point, arn:aws:s3:eu-central-1:123456789012:accesspoint/acme-prod-ap),
// s3-object-lambda (an Object Lambda access point), s3-outposts (an access point
// or a bucket after the outpost's own segment,
// .../outpost/op-0123.../bucket/acme-op-bucket) and s3express (a directory
// bucket, bucket/acme-prod--euc1-az1--x-s3). The segment and the name are
// delimited with a slash or a colon (accesspoint:acme-prod-ap), as an ARN may
// write either. The first submatch is the service's suffix after s3 (express
// for a directory bucket, empty for s3 itself); the second is the accesspoint
// or bucket segment and the name, the value reported as written. The outpost's
// identifier names no customer and is not reported.
var s3ResourceARNPattern = regexp.MustCompile(
	`(?i)\barn:aws[a-z-]*:s3(-object-lambda|-outposts|express)?:[a-z0-9-]*:[0-9]*:(?:outpost/[a-z0-9-]+/)?((?:accesspoint|bucket)[/:][a-z0-9][a-z0-9_.-]*)`)

// directoryBucketSuffixPattern matches the suffix S3 Express appends to a
// directory bucket's own words (acme-prod--euc1-az1--x-s3): the availability or
// local zone's ID between double hyphens and the --x-s3 marker, lowercased.
// Only a zone ID's shape is read there: a region code (euc1, usw2), an optional
// local zone (lax1) and the zone's number (az1), so customer words in that slot
// (example--acme-prod--x-s3, example--acme--x-s3) are never taken for a zone and
// stay in the name. A zone ID never holds a double hyphen, so the pattern takes
// only the one suffix at the end and leaves a double hyphen in the bucket's own
// words (example--acme-prod--euc1-az1--x-s3) to them. The words before it are
// what is held to the sample rule. It applies only to an s3express ARN's
// bucket, never to an access point's name.
var directoryBucketSuffixPattern = regexp.MustCompile(`--[a-z]{3,4}[0-9]+(?:-[a-z]+[0-9]+)?-az[0-9]+--x-s3$`)

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

// sampleStorageAccountPattern matches a sample's storage account name,
// lowercased: example and generic words run together, as a storage account's
// name holds no hyphen (examplestorage, exampledatalogs), optionally ending in a
// count of one to three digits (exampledata01), the way a sample's bucket is
// example- and generic words (sampleBucketPattern). A name after the prefix
// (exampleacmeprod) or a longer run of digits does not match.
var sampleStorageAccountPattern = regexp.MustCompile(
	`^example(?:storage|account|data|logs|backup|assets|archive|media|bucket|files|lake|blob)*[0-9]{0,3}$`)

// sampleStorageAccount reports whether account, a storage account's name as
// written, is a sample's (sampleStorageAccountPattern), matched
// case-insensitively.
func sampleStorageAccount(account string) bool {
	return sampleStorageAccountPattern.MatchString(strings.ToLower(account))
}

// resourcePathProblems returns a problem for every customer name a provider
// resource path carries, the globally unique names the host name rule never
// reads because they carry no dot or sit in a path segment (issue #406): a GCP
// project ID (projectPathPattern), the bucket of a Cloud Storage resource name
// (projectBucketPattern), the storage account of an Azure ARM ID
// (storageAccountPathPattern) and the access point, Outposts bucket or S3
// Express directory bucket of an S3 ARN (s3ResourceARNPattern). Each is held to
// its sample value: a project, a bucket or an access point must be a sample's
// (sampleResourceName: example, example- and generic words, example-project or
// example-ap; a directory bucket's words before its zone suffix), a storage
// account must be example and generic words (sampleStorageAccount), and outside
// an ARM ID a child-resource type or an RBAC operation after storageAccounts/
// (Microsoft.Storage/storageAccounts/blobServices, .../read) is no account at
// all. The value reported is the segment and the name as the file writes them
// (projects/acme-corp-prod, buckets/acme-prod-data,
// storageAccounts/acmeprodcustomers, accesspoint/acme-prod-ap,
// accesspoint:acme-prod-ap, bucket/acme-op-bucket), so the failure names the
// value to change.
//
// It knowingly leaves these gaps, accepted when issue #406 closed:
//
//   - the other globally unique Azure names an ARM ID carries
//     (Microsoft.KeyVault/vaults, Microsoft.ContainerRegistry/registries,
//     Microsoft.DocumentDB/databaseAccounts, Microsoft.Sql/servers,
//     Microsoft.Web/sites, Microsoft.ServiceBus/namespaces), each the DNS label
//     of a public host, are not read: reading them needs a sample rule for each
//     or allow-list entries for the guide's synthetic psql-shop,
//     app-shop-storefront and sb-shop, left for a follow-up;
//   - JSON-escaped slashes (projects\/acme-corp-prod\/...), which no pattern
//     reads, for the same reason as vendorPathProblems' gap: no tool writes a
//     fixture that way, and reading every spelling of a slash would make each
//     pattern unreadable;
//   - a legacy domain-scoped project ID (projects/example.com:acme-prod), whose
//     dot and colon the project pattern does not read past; a customer's own
//     domain there is the host name rule's value;
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

		if sampleStorageAccount(account) {
			continue
		}

		if !armIDAccount(text, start) &&
			slices.ContainsFunc(storageAccountTypeWords, func(word string) bool { return strings.EqualFold(account, word) }) {
			continue
		}

		problems = append(problems, problem{
			line:  lineOf(text, start),
			value: value,
			rule:  "names an Azure storage account that is not example and generic words (CLAUDE.md rule 2)",
		})
	}

	for _, match := range s3ResourceARNPattern.FindAllStringSubmatchIndex(text, -1) {
		value := strings.TrimRight(text[match[4]:match[5]], ".")
		sep := strings.IndexAny(value, "/:")
		name := strings.ToLower(value[sep+1:])

		directoryBucket := match[2] >= 0 && strings.EqualFold(text[match[2]:match[3]], "express") &&
			strings.EqualFold(value[:sep], "bucket")
		if directoryBucket {
			name = directoryBucketSuffixPattern.ReplaceAllString(name, "")
		}

		if sampleResourceName(name) {
			continue
		}

		problems = append(problems, problem{
			line:  lineOf(text, match[4]),
			value: value,
			rule:  "names an S3 access point, Outposts bucket or directory bucket that is not example or example- and generic words (CLAUDE.md rule 2)",
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
