package examples_test

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
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
// namespace (armIDProvidersSegment) come right before it, in any case. Only
// the bytes just before start are compared, so a long text holding many
// storageAccounts segments is not copied once per match.
func armIDAccount(text string, start int) bool {
	n := len(armIDProvidersSegment)
	return start >= n && strings.EqualFold(text[start-n:start], armIDProvidersSegment)
}

// armNameType is one Azure resource type whose name, in an ARM ID, is globally
// unique: the provider namespace and the resource types under it, written the
// way Azure documents them, and the shortest name the type allows.
type armNameType struct {
	provider string
	types    []string
	// minLength is the shortest name, in characters, the type's naming rules
	// allow. A shorter segment (the guide's placeholder namespaces/n) is no
	// name of the type and names nobody.
	minLength int
}

// armNameTypes are the Azure resource types besides a storage account whose
// name an ARM ID carries and which Azure's naming rules
// (learn.microsoft.com/azure/azure-resource-manager/management/resource-name-rules)
// make globally unique, each the DNS label of a public host of its own:
// <name>.vault.azure.net, <name>.azurecr.io, <name>.documents.azure.com,
// <name>.database.windows.net, <name>.azurewebsites.net and
// <name>.servicebus.windows.net. The naming rules' table lists the PostgreSQL
// and MySQL single servers as global but not the flexible servers; each
// flexible server gets the public DNS name <name>.postgres.database.azure.com
// or <name>.mysql.database.azure.com all the same, so it is treated as global
// too. The minimum lengths are the naming rules': a Key Vault three to
// twenty-four characters, a registry five to fifty, a Cosmos DB account three
// to forty-four, a SQL server one to sixty-three, a PostgreSQL or MySQL server
// three to sixty-three, a web app two to sixty and a Service Bus namespace six
// to fifty.
var armNameTypes = []armNameType{
	{provider: "Microsoft.KeyVault", types: []string{"vaults"}, minLength: 3},
	{provider: "Microsoft.ContainerRegistry", types: []string{"registries"}, minLength: 5},
	{provider: "Microsoft.DocumentDB", types: []string{"databaseAccounts"}, minLength: 3},
	{provider: "Microsoft.Sql", types: []string{"servers"}, minLength: 1},
	{provider: "Microsoft.DBforPostgreSQL", types: []string{"servers", "flexibleServers"}, minLength: 3},
	{provider: "Microsoft.DBforMySQL", types: []string{"servers", "flexibleServers"}, minLength: 3},
	{provider: "Microsoft.Web", types: []string{"sites"}, minLength: 2},
	{provider: "Microsoft.ServiceBus", types: []string{"namespaces"}, minLength: 6},
}

// armNamePattern is the pattern that finds an armNameType's name in an ARM ID:
// the providers segment, standing at the text's start or after a byte other
// than a letter, digit, hyphen or underscore (a glued xproviders/ is no ARM
// ID's segment and never matches), the namespace and one of the type's
// segments, matched in any case, as an ARM ID is case-insensitive, then the
// name. The first
// submatch is the type segment and the name, the value reported as written;
// the second is the name alone. The name is the whole run of letters, digits,
// hyphens, underscores and dots up to the next slash or other delimiter, wider
// than any one type's charset on purpose: a name holding a byte its type never
// allows is reported all the same rather than cut short and passed (it fails
// closed), and no maximum length is read for the same reason. Only the child
// resources after the name (/secrets/db-password, /queues/orders) are left out,
// since the slash ends the run. A web app's name may hold Unicode letters, so
// the run reads them too.
func armNamePattern(t armNameType) *regexp.Regexp {
	types := make([]string, len(t.types))
	for i, typ := range t.types {
		types[i] = regexp.QuoteMeta(typ)
	}

	return regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_-])providers/` + regexp.QuoteMeta(t.provider) + `/((?:` +
		strings.Join(types, "|") + `)/([\p{L}\p{N}_.-]+))`)
}

// armNamePatterns are armNameTypes' patterns, in the same order.
var armNamePatterns = func() []*regexp.Regexp {
	patterns := make([]*regexp.Regexp, len(armNameTypes))
	for i, t := range armNameTypes {
		patterns[i] = armNamePattern(t)
	}

	return patterns
}()

// sampleARMName reports whether name, an armNameType's name as written, is a
// sample's: the shape a sample storage account has (example and generic words
// run together, exampledata) or the one a sample bucket has (example- and
// generic words, example-data), as these types allow hyphens. Both are matched
// case-insensitively. The inventory guide's synthetic shop names (psql-shop,
// sb-shop) are no sample's shape: realDataAllowList excuses each in the guide's
// files that write it.
func sampleARMName(name string) bool {
	return sampleStorageAccount(name) || sampleBucket(name)
}

// armNameProblems returns a problem for every armNameType's name an ARM ID in
// text carries that is no sample's (sampleARMName), in the order the text
// writes them. A providers segment glued to a letter, digit, hyphen or
// underscore before it (xproviders/) never matches (armNamePattern), so it
// consumes no bytes and an ID after it is still read; a match whose name, its
// trailing dots trimmed, is shorter than its type's minimum (namespaces/n) or
// empty (sites/..., sites/$name) names nobody.
func armNameProblems(text string) []problem {
	type found struct {
		offset int
		problem
	}

	var all []found

	for i, pattern := range armNamePatterns {
		for _, match := range pattern.FindAllStringSubmatchIndex(text, -1) {
			name := strings.TrimRight(text[match[4]:match[5]], ".")
			if utf8.RuneCountInString(name) < armNameTypes[i].minLength || sampleARMName(name) {
				continue
			}

			all = append(all, found{offset: match[2], problem: problem{
				line:  lineOf(text, match[2]),
				value: text[match[2]:match[4]] + name,
				rule:  "names a globally unique Azure resource in an ARM ID that is not example and generic words (CLAUDE.md rule 2)",
			}})
		}
	}

	slices.SortStableFunc(all, func(a, b found) int { return a.offset - b.offset })

	problems := make([]problem, 0, len(all))
	for _, f := range all {
		problems = append(problems, f.problem)
	}

	return problems
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

// sampleProjectPattern matches the sample project ID a projects/ segment holds
// that a sample's bucket name (sampleBucketPattern) does not cover, lowercased:
// example-project, optionally numbered (example-project-2).
var sampleProjectPattern = regexp.MustCompile(`^example-project(?:-[0-9]{1,3})?$`)

// sampleAccessPointPattern matches the sample access point an S3 ARN holds that
// a sample's bucket name (sampleBucketPattern) does not cover, lowercased:
// example-ap, optionally numbered (example-ap-2).
var sampleAccessPointPattern = regexp.MustCompile(`^example-ap(?:-[0-9]{1,3})?$`)

// sampleProjectName reports whether name, a GCP project ID as written, is a
// sample's: a sample bucket's shape (sampleBucket) or sampleProjectPattern's,
// matched case-insensitively. A sample access point's name (example-ap) is no
// sample project.
func sampleProjectName(name string) bool {
	return sampleBucket(name) || sampleProjectPattern.MatchString(strings.ToLower(name))
}

// sampleAccessPointName reports whether name, the access point, Outposts
// bucket or directory bucket of an S3 ARN as written, is a sample's: a sample
// bucket's shape (sampleBucket) or sampleAccessPointPattern's, matched
// case-insensitively. A sample project's ID (example-project) is no sample
// access point.
func sampleAccessPointName(name string) bool {
	return sampleBucket(name) || sampleAccessPointPattern.MatchString(strings.ToLower(name))
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
// (storageAccountPathPattern), the other globally unique Azure names an ARM ID
// carries (armNameTypes: a Key Vault, a container registry, a Cosmos DB
// account, a SQL, PostgreSQL or MySQL server or flexible server, a web app and
// a Service Bus namespace, armNameProblems) and the access point, Outposts
// bucket or S3 Express directory bucket of an S3 ARN (s3ResourceARNPattern).
// Each is held to its sample value: a project, a bucket or an access point must
// be a sample's (example, or example- and generic words, and for a project
// example-project (sampleProjectName), for an access point example-ap
// (sampleAccessPointName); a directory bucket's words before its zone suffix),
// a storage account must be example and generic words (sampleStorageAccount),
// another Azure name a storage account's or a bucket's sample (sampleARMName),
// and outside an ARM ID a child-resource type or an RBAC operation after
// storageAccounts/ (Microsoft.Storage/storageAccounts/blobServices, .../read)
// is no account at all. The value reported is the segment and the name as the
// file writes them (projects/acme-corp-prod, buckets/acme-prod-data,
// storageAccounts/acmeprodcustomers, vaults/kv-contoso-prod,
// accesspoint/acme-prod-ap, accesspoint:acme-prod-ap, bucket/acme-op-bucket),
// so the failure names the value to change.
//
// It knowingly leaves these gaps, accepted when issues #406 and #424 closed:
//
//   - the globally unique Azure names an ARM ID carries beyond armNameTypes,
//     for example the siblings of a read type in its own namespace
//     (Microsoft.KeyVault/managedHSMs, Microsoft.Sql/managedInstances,
//     Microsoft.DocumentDB/mongoClusters,
//     Microsoft.DBforPostgreSQL/serverGroupsv2) and others
//     (Microsoft.Cache/redis, Microsoft.EventHub/namespaces,
//     Microsoft.Relay/namespaces, Microsoft.Search/searchServices,
//     Microsoft.ApiManagement/service,
//     Microsoft.AppConfiguration/configurationStores,
//     Microsoft.SignalRService/signalR, Microsoft.Cdn/profiles/endpoints,
//     Microsoft.Network/frontDoors, Microsoft.Network/trafficManagerProfiles,
//     Microsoft.Devices/IotHubs, Microsoft.DataFactory/factories,
//     Microsoft.Synapse/workspaces, Microsoft.NotificationHubs/namespaces,
//     Microsoft.Web/hostingEnvironments, a Cognitive Services account's custom
//     subdomain); the full list is the global-scope rows of Azure's naming
//     rules
//     (learn.microsoft.com/azure/azure-resource-manager/management/resource-name-rules).
//     Each is the DNS label of a public host too, and none is read: no fixture
//     or guide page writes one, and
//     each is one armNameTypes row, added test first with its own rows, when
//     the inventory guide covers its type (issue #424 scoped the types the
//     guide reads);
//   - an armNameType's name outside an ARM ID (a type string with a name after
//     it, Microsoft.Web/sites/app-contoso-prod, or a bare "name" field), for the
//     same reason as a bare storage account: without the providers/ segment the
//     segment after the type is a child-resource type's word as often as a
//     name (Microsoft.ServiceBus/namespaces/queues);
//   - an armNameType's name glued to a character outside the run it reads (a
//     web app's name holding a symbol or a space), which is reported only up to
//     that character, and a name shorter than its type's minimum, which names
//     nobody;
//   - JSON-escaped slashes (projects\/acme-corp-prod\/...) and percent-encoded
//     ones (an ARM ID in a URL's query or path, providers%2FMicrosoft.Storage%2FstorageAccounts%2Facmeprod,
//     .../Microsoft.KeyVault%2Fvaults%2Fkv-contoso-prod), which no pattern
//     reads, neither a storage account's nor an armNameType's, for the same
//     reason as vendorPathProblems' gap: no tool writes a fixture that way, and
//     reading every spelling of a slash would make each pattern unreadable;
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
		if sampleProjectName(value[strings.Index(value, "/")+1:]) {
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

		if sampleAccessPointName(name) {
			continue
		}

		problems = append(problems, problem{
			line:  lineOf(text, match[4]),
			value: value,
			rule:  "names an S3 access point, Outposts bucket or directory bucket that is not example or example- and generic words (CLAUDE.md rule 2)",
		})
	}

	return append(problems, armNameProblems(text)...)
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
