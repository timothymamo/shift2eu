package examples_test

import (
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
)

// checkedDirs are the directories below the module root whose every file the
// real-data check reads (issue #249), besides every directory named testdata:
// the samples a person copies into a demo and the guide pages that quote them.
var checkedDirs = []string{"examples", "docs/guide"}

// testdataDir is the name of a directory of test fixtures. Every one of them in
// the module is checked, however deep, nested ones included.
const testdataDir = "testdata"

// skippedDirs are the directories the walk never enters: git's own data and the
// other worktrees, which are other branches' copies of the module.
var skippedDirs = []string{".git", ".worktrees"}

// allowListEntry excuses one value the sanitisation check reports in one file:
// a value that is there on purpose and identifies nobody.
//
// For a name the check reads only in an ARM ID, the value (flexibleServers/,
// sites/, namespaces/ and the name) names the value's type segment as well as
// its file, not the provider namespace; a namespace that shares the segment is
// excused alike (a MySQL flexibleServers/psql-shop in the same file, or an
// Event Hubs or Relay namespaces/sb-shop once those are read), which is
// accepted because the names are synthetic and both namespaces' names are
// global.
type allowListEntry struct {
	// file is the slash-separated path of one regular file, relative to the
	// module root: never a directory or a glob.
	file string
	// value is the exact offending value the check reports: for an ARM ID's
	// name, its type segment and the name.
	value string
	// why says why the value is legitimate there.
	why string
}

// realDataAllowList holds the values the real-data check reports in a checked
// file although they are no real data. Each entry names one file and one value,
// and TestAllowListEntriesAreNarrowAndUsed fails an entry the check no longer
// needs.
var realDataAllowList = []allowListEntry{
	{
		file:  "test/architecture/internal/check/testdata/auditworkflow/valid.yml",
		value: "1.8.0.0",
		why:   "socat's Debian package version (1.8.0.0-4) in the audit workflow's accepted shape, no address",
	},
	{
		file:  "docs/guide/verify-releases.md",
		value: "www.shift2eu.com",
		why:   "the project's own site (PRD #297), which the guide tells users to type",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/aws/eu-central-1/autoscaling-groups.json",
		value: "00000000-0000-0000-0000-000000000001",
		why:   "a synthetic Auto Scaling group ID in an ARN, all zero but its last digit so the two groups differ",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/aws/eu-central-1/autoscaling-groups.json",
		value: "00000000-0000-0000-0000-000000000002",
		why:   "the second synthetic Auto Scaling group ID, all zero but its last digit",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/blob-services.json",
		value: "storageAccounts/stshopassets",
		why:   "the synthetic storage account of the guide's sample shop, named the way Azure's naming guide (st prefix) spells one; it names no one's account",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/file-shares.json",
		value: "storageAccounts/stshopfiles",
		why:   "the shares of the guide's sample shop's synthetic FileStorage account, whose row the Resource Graph export holds; it names no one's account",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/graph-1.json",
		value: "storageAccounts/stshopassets",
		why:   "the guide's sample shop's synthetic asset storage account in the Resource Graph export; it names no one's account",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/graph-1.json",
		value: "storageAccounts/stshopfiles",
		why:   "the guide's sample shop's synthetic file-share storage account in the Resource Graph export; it names no one's account",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/graph-2.json",
		value: "flexibleServers/psql-shop",
		why:   "the synthetic PostgreSQL flexible server of the inventory guide's sample shop in the Resource Graph export; it names no one's server",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/graph-2.json",
		value: "flexibleServers/psql-shop-replica",
		why:   "the synthetic read replica of the inventory guide's sample shop's PostgreSQL server; it names no one's server",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/graph-2.json",
		value: "flexibleServers/mysql-shop-catalog",
		why:   "the synthetic MySQL flexible server of the inventory guide's sample shop's catalog; it names no one's server",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/graph-2.json",
		value: "flexibleServers/mysql-shop-reports",
		why:   "the synthetic MySQL flexible server of the inventory guide's sample shop's reports; it names no one's server",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/graph-2.json",
		value: "sites/func-shop-thumbnails",
		why:   "the synthetic function app of the inventory guide's sample shop; it names no one's app",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/graph-2.json",
		value: "sites/app-shop-storefront",
		why:   "the synthetic web app of the inventory guide's sample shop's storefront; it names no one's app",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/graph-2.json",
		value: "namespaces/sb-shop",
		why:   "the synthetic Service Bus namespace of the inventory guide's sample shop in the Resource Graph export; it names no one's namespace",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/graph-2.json",
		value: "microsoft.app",
		why:   "the Resource Manager provider namespace of Container Apps, whose segment reads as a domain name because .app is a TLD; it names no host",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/graph-2.json",
		value: "databaseAccounts/cosmos-shop-catalog",
		why:   "the synthetic Cosmos DB account of the inventory guide's sample shop's catalog; it names no one's account",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/graph-2.json",
		value: "databaseAccounts/cosmos-shop-events",
		why:   "the synthetic Cosmos DB account of the inventory guide's sample shop's events; it names no one's account",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/graph-2.json",
		value: "registries/acrshop",
		why:   "the synthetic container registry of the inventory guide's sample shop; it names no one's registry",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/servicebus.json",
		value: "namespaces/sb-shop",
		why:   "the synthetic Service Bus namespace of the inventory guide's sample shop that holds its queues; it names no one's namespace",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/azure/storage-queues.json",
		value: "storageAccounts/stshopassets",
		why:   "the synthetic storage account of the guide's sample shop that holds its queues; it names no one's account",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/gcp/assets.json",
		value: "storage.googleapis.com/shop-assets-example",
		why:   "the Cloud Asset name of the guide's synthetic shop bucket, marked -example; it names no one's bucket",
	},
	{
		file:  "test/e2e/testdata/inventory-guide/gcp/assets.json",
		value: "storage.googleapis.com/shop-backups-example",
		why:   "the Cloud Asset name of the guide's synthetic backup bucket, marked -example; it names no one's bucket",
	},
	{
		file:  "test/lint/testdata/cloudreadonly/mod/internal/adapter/source/gcp/cgo.go",
		value: "C.int",
		why:   "cgo's C.int type in a Go-source probe, a selector that names no domain although int is a TLD",
	},
	{
		file:  "ci/auditpublish/internal/publish/testdata/report.golden.md",
		value: "github.com/example",
		why:   "the run URL of the audit fixtures' documented dummy repository, example/shift2eu, in the report the publish core renders; no account is named example, so it names no one's repository",
	},
}

// checkedFiles returns every file the real-data check reads, sorted by path:
// those under <root>/examples and <root>/docs/guide, and those under every
// directory named exactly testdata anywhere below root. A directory of fixtures
// under any other name (fixtures/, golden/) is not checked, so a new fixture
// directory must be named testdata. A sample's name is its slash-separated path
// relative to root; its source is empty, as a checked file need not be an
// inventory. A missing directory yields no file and no error.
//
// The walk reads the directory tree on disk, not git's index, so an untracked
// or git-ignored file under a checked directory is checked too: a real
// inventory dropped there locally fails the check before it can be committed.
//
// A symbolic link in a checked path (or one that stands for a checked
// directory) is listed as well but never followed: fixtureProblems reports it,
// since a fixture must be a regular file. The walk goes through an os.Root, so
// it cannot leave root.
func checkedFiles(root string) ([]sample, error) {
	dir, err := os.OpenRoot(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, err
	}

	defer func() { _ = dir.Close() }()

	fsys := dir.FS()

	var files []sample

	err = fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}

			return err
		}

		if entry.IsDir() {
			if name != "." && slices.Contains(skippedDirs, entry.Name()) {
				return fs.SkipDir
			}

			return nil
		}

		switch {
		case entry.Type().IsRegular() && checkedPath(name):
		case entry.Type()&fs.ModeSymlink != 0 && holdsCheckedPath(name):
		default:
			return nil
		}

		files = append(files, sample{path: filepath.Join(root, filepath.FromSlash(name)), name: name})

		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.SortFunc(files, func(a, b sample) int { return strings.Compare(a.name, b.name) })

	return files, nil
}

// checkedPath reports whether name, a slash-separated path relative to the
// module root, is a file the real-data check reads: one under a checked
// directory or under a directory named testdata.
func checkedPath(name string) bool {
	for _, dir := range checkedDirs {
		if strings.HasPrefix(name, dir+"/") {
			return true
		}
	}

	dirs := strings.Split(name, "/")

	return slices.Contains(dirs[:len(dirs)-1], testdataDir)
}

// holdsCheckedPath reports whether name, a slash-separated path relative to the
// module root, is or could stand for a checked path: a file in a checked
// directory, a directory named testdata, or a checked directory or one of its
// parents. A symbolic link at such a name would bring a file the check never
// reads into a checked path.
func holdsCheckedPath(name string) bool {
	if checkedPath(name + "/") {
		return true
	}

	return slices.ContainsFunc(checkedDirs, func(dir string) bool {
		return strings.HasPrefix(dir+"/", name+"/")
	})
}

// sanitisationProblems returns every value a checked file may not carry under
// CLAUDE.md rule 2, rule by rule: account IDs, GUIDs, IPv4 addresses (written
// with dots or with hyphens or underscores), IPv6 addresses, host names, the
// owners and buckets a vendor path or a bucket URL names, and the customer names
// a provider resource path carries. The first four rules are the samples' own;
// the host name rule is the narrower fixture one (fixtureHostnameProblems), and
// the hyphenated-address rule (hyphenatedIPv4Problems), the vendor-path rule
// (vendorPathProblems) and the resource-path rule (resourcePathProblems) cover
// forms a fixture holds, so the strict rule TestSamplesAreSanitised applies over
// examples/ is unchanged.
//
// The resource-path rule reads the globally unique names the others never see,
// since they carry no dot or sit in a path segment (issue #406): a GCP project ID
// (projects/acme-corp-prod/...), an Azure storage account in an ARM ID
// (.../storageAccounts/acmeprod) and the other globally unique Azure names an
// ARM ID carries (a Key Vault, a container registry, a Cosmos DB account, a
// SQL, PostgreSQL or MySQL server, a web app, a Service Bus namespace;
// armNameProblems), a Cloud
// Storage bucket in a resource name (projects/_/buckets/acme-prod-data) and an
// S3 access point (Object Lambda included), S3 Express or Outposts bucket in an
// ARN (...:accesspoint/acme-prod-ap).
func sanitisationProblems(text string) []problem {
	return slices.Concat(
		accountIDProblems(text),
		guidProblems(text),
		ipv4Problems(text),
		hyphenatedIPv4Problems(text),
		ipv6Problems(text),
		fixtureHostnameProblems(text),
		vendorPathProblems(text),
		resourcePathProblems(text),
	)
}

// publicTLDs are the top-level domains, lowercased, under which the fixture
// check reads a dotted name as a host name. The guide pages and the testdata
// directories are full of dotted values that are no host at all (file names,
// versions, jq and JSON paths, plan names, Go selectors), so a name counts as a
// host only when its last label is one of these. The list is curated, not
// complete: the generic TLDs a company runs a host under, the sponsored and
// restricted ones (gov, edu, mil, aero, coop, jobs, museum, travel), every EU,
// EEA and EFTA country code but Iceland's, the commonest other country codes,
// OVHcloud's own ovh, and the private DNS suffixes an internal name is built
// from (internal, the AWS and GCP private zone; corp; lan; localdomain;
// intranet; private; priv; home; consul, HashiCorp Consul's service zone). The
// home network zone of RFC 8375, home.arpa, is read through
// privateTwoLabelSuffixes, as its last label alone is the reverse-DNS one.
//
// It fails open, knowingly: because it is an allow list, a host under a TLD it
// does not name passes, so this is a filter for the likely value, never a
// guarantee. The known gaps are the rarer country codes (the list holds some
// forty of about 250), the newer generic TLDs and the internationalised ones,
// and Indonesia's id, left out on purpose: every jq and JSON field path a guide
// page quotes ends in .id (value.id, managedDisk.id), so the code would report
// those by the dozen while a customer host under it is rare. Additions are
// expected whenever a likely one turns up in a guide page or a fixture. These
// gaps are accepted (issue #406): a curated list cannot name every TLD without
// reading every dotted value as a host.
//
// The opposite choice is made for home, private, priv, jobs and travel: they
// match in any case, so a dotted value that is no host at all (workflow.jobs,
// user.home, cfg.private) is reported too. That is kept on purpose, as it fails
// closed: a real internal host under these suffixes is customer data, while a
// false positive costs one allow-list entry. TestSanitisationProblemsHostNameRule
// pins workflow.jobs being reported.
//
// The four suffixes a Go selector ends with would be a worse gap than that, since
// every Go-source fixture that imports the package carries one of them (Iceland's
// is in errors.Is, the private suffix local in time.Local, India's in in now.In,
// the international-organisation int in flag.Int), so they are matched as hosts
// too, but only when the name's last label is written in exactly this lower case
// (lowercaseOnlyTLDs): db01.corp.local, db01.Corp.local, shop.example.is and
// web.acme.in are reported, the selectors, whose last label a capital opens,
// are not. The gaps there are a name whose last label is capitals
// (DB01.CORP.LOCAL passes) and a lower-case unexported Go selector in a
// Go-source fixture (cfg.local, s.is, C.int), which would be reported; only
// cgo's C.int trips that one in the tree, excused on the allow list, and
// reporting a selector is the safe direction. Both are accepted (issue #406): a host
// is written in lower case far more often than in capitals, and reading a
// capital last label as a host would report every exported Go selector again.
//
// It deliberately leaves out the TLDs that clash with a file extension a
// fixture or a guide page names (md, sh, py, rs), so install.md or run.sh is
// never read as a host; json, yaml, go and golden are no TLDs at all. Poland's
// pl is kept although it is Perl's extension too: no fixture names a Perl
// script.
var publicTLDs = []string{
	// Generic.
	"com", "net", "org", "io", "dev", "app", "cloud", "ai", "co",
	"info", "biz", "tech", "online", "site", "xyz", "shop", "store",
	// OVHcloud's own TLD: a customer of the target hosts a site under it.
	"ovh",
	// The EU, EEA and EFTA country codes, but is, which lowercaseOnlyTLDs
	// matches only in lower case.
	"eu", "at", "be", "bg", "ch", "cy", "cz", "de", "dk", "ee", "es", "fi",
	"fr", "gr", "hr", "hu", "ie", "it", "li", "lt", "lu", "lv", "mt", "nl",
	"no", "pl", "pt", "ro", "se", "si", "sk", "uk",
	// The sponsored and restricted TLDs a public body, a school or a trade runs
	// a host under, but int, which lowercaseOnlyTLDs matches only in lower case.
	"gov", "edu", "mil", "aero", "coop", "jobs", "museum", "travel",
	// Other common country codes, but in, which lowercaseOnlyTLDs matches only
	// in lower case.
	"us", "ca", "au", "jp", "me", "tv", "ly",
	"br", "cn", "ru", "tr", "mx", "ar", "za", "sg", "hk", "kr", "tw",
	"il", "ae", "nz", "th", "vn", "my", "ph", "pk", "sa", "ua",
	// Private DNS suffixes: such a name identifies a customer's own network.
	"internal", "corp", "lan", "localdomain", "intranet", "private", "priv",
	"home", "consul",
}

// privateTwoLabelSuffixes are the private DNS suffixes of two labels under which
// the fixture check reads a name as a host: home.arpa, the zone RFC 8375 reserves
// for a home network. Its last label, arpa, is no TLD of publicTLDs, since a
// reverse name (3.2.1.10.in-addr.arpa) ends with it too and the dotted-quad rule
// already reports the address it spells.
var privateTwoLabelSuffixes = []string{"home.arpa"}

// lowercaseOnlyTLDs are the public suffixes under which the fixture check reads
// a name as a host only when the name's last label is written in exactly this
// lower case: a Go selector ends with one of them and capitalises that label
// (errors.Is, time.Local, flag.Int, now.In), and a Go-source fixture is full of
// those, while a host under them - the country codes of Iceland and India, the
// international-organisation TLD, or the .local zone mDNS and a customer's own
// internal DNS use - is written in lower case, whatever the labels before it are
// (db01.Corp.local). An all-caps name (DB01.CORP.LOCAL) passes, a gap of the
// same kind as the curated TLD list above, and a lower-case selector in a
// Go-source fixture (cfg.local, s.is, cgo's C.int) is reported, which fails
// closed: the one the tree holds, C.int, is excused on the allow list.
var lowercaseOnlyTLDs = []string{"local", "is", "in", "int"}

// publicVendorHosts are the exact public hosts of the vendors and projects a
// guide page or a fixture links to or names: they identify the vendor, never a
// customer. The match is exact, never a suffix: a name below one of them
// (evil.github.com) or one that merely contains one (ghcr.io.acme-corp.com) is
// a customer's name and is reported.
var publicVendorHosts = []string{
	// The code forge, its API, CLI download and raw-file hosts, its OIDC token
	// issuer and its container registry.
	"github.com",
	"api.github.com",
	"cli.github.com",
	"raw.githubusercontent.com",
	"token.actions.githubusercontent.com",
	"ghcr.io",
	// The targets' public sites, which the catalog snapshots are taken from.
	"www.scaleway.com",
	"www.ovhcloud.com",
	"upcloud.com",
	// Sources' public sites and endpoints the guide and the dev VM name.
	"cloud.google.com",
	"accounts.google.com",
	"dl.google.com",
	"management.azure.com",
	// Go's module and download hosts.
	"golang.org",
	"google.golang.org",
	// Sigstore's TUF repository and keyless-signing identity, used to verify
	// the release images.
	"tuf-repo-cdn.sigstore.dev",
	"projectsigstore.iam.gserviceaccount.com",
	// Other public projects a page or the dev VM links to.
	"kubernetes.io",
	"nodejs.org",
	// Ubuntu's primary archive and security hosts, which the dev VM's
	// provisioning names to rewrite the one and leave the other (issue #498).
	"archive.ubuntu.com",
	"security.ubuntu.com",
	"spdx.dev",
	"json-schema.org",
	"claude.ai",
}

// fixtureHostnameProblems returns the problems hostnameProblems reports that
// are host names in the fixture sense: names whose last label is a public TLD
// (publicTLDs) or, written in lower case exactly, one of the two suffixes a Go
// selector ends with (lowercaseOnlyTLDs), and that are not an exact public
// vendor host (publicVendorHosts). Every name hostnameProblems already accepts
// (example.com and its subdomains, the reserved .example name, the provider API
// hosts, the taxonomy vocabulary) stays accepted.
func fixtureHostnameProblems(text string) []problem {
	var problems []problem

	for _, p := range hostnameProblems(text) {
		lower := strings.ToLower(p.value)

		if slices.Contains(publicVendorHosts, lower) {
			continue
		}

		label := lastLabel(lower)

		switch {
		case slices.ContainsFunc(privateTwoLabelSuffixes, func(suffix string) bool { return strings.HasSuffix(lower, "."+suffix) }):
			// A name in a private zone of two labels (db01.home.arpa).
		case slices.Contains(lowercaseOnlyTLDs, label):
			// An exported Go selector ends with one of these suffixes and
			// writes its last label with a capital (errors.Is, time.Local,
			// time.Time.Local), while a host under them writes that label in
			// lower case, whatever the labels before it look like
			// (db01.Corp.local): only a last label that is exactly the
			// lower-case suffix is a host.
			if lastLabel(p.value) != label {
				continue
			}
		case !slices.Contains(publicTLDs, label):
			continue
		}

		problems = append(problems, p)
	}

	return problems
}

// ownerPathPattern finds the owner a path names after a vendor host whose
// first path segment is an account (github.com/acme/repo,
// raw.githubusercontent.com/acme/repo/..., ghcr.io/acme/image): the host,
// matched case-insensitively, a slash and the owner's name, or a colon and the
// owner's name in the SSH clone form (git@github.com:acme/repo.git), which
// vendorPathProblems accepts only for github.com right after an @, so a port
// (ghcr.io:443) or a colon in prose is no owner. The host must stand on its own
// (vendorPathProblems), so a name below it (evil.github.com) is left to the host
// name rule, and so is api.github.com, whose first segment is an endpoint rather
// than an owner: apiOwnerPathPattern reads that host.
var ownerPathPattern = regexp.MustCompile(`(?i)(?:github\.com|raw\.githubusercontent\.com|ghcr\.io)[/:][a-z0-9][a-z0-9_.-]*`)

// apiOwnerPathPattern finds the owner a GitHub API path names behind its
// endpoint segment (api.github.com/repos/acme/infra, /users/acme, /orgs/acme,
// /enterprises/acme):
// the host, the endpoint and the owner, matched case-insensitively. The value
// reported is the host and the owner (api.github.com/acme), the endpoint left
// out, the way ownerPathPattern's value is.
var apiOwnerPathPattern = regexp.MustCompile(`(?i)api\.github\.com/(?:repos|users|orgs|enterprises)/[a-z0-9][a-z0-9_.-]*`)

// sshCloneHost is the one vendor host whose owner ownerPathPattern reads after
// a colon, in the SSH clone form user@host:owner/repo.
const sshCloneHost = "github.com"

// bucketPattern finds the bucket a bucket URL names (s3://acme-prod-bucket/key,
// s3a:// and s3n://, the Hadoop clients' spellings of the same bucket,
// gs://acme-prod-bucket/x, az://container/blob, and oci://acme-prod-bucket@tenancy/x,
// whose bucket comes before the @ and its namespace): the scheme and the
// bucket, the first segment after it. An oci:// reference whose first segment
// holds a dot and is not followed by an @ names a registry host
// (oci://ghcr.io/sigstore/charts/x), so vendorPathProblems passes it over and
// leaves the host and its owner path to the host name and owner rules. An OCI
// Object Storage bucket's name may hold dots too, but the @ and the namespace
// after it mark a bucket (oci://acme.prod.backups@tenancy/x), which is read.
var bucketPattern = regexp.MustCompile(`(?i)\b(?:s3[an]?|gs|az|oci)://[a-z0-9][a-z0-9_.-]*`)

// storageAccountURLPattern finds the Azure storage account an abfs, abfss, wasb
// or wasbs URL names after its container
// (abfss://data@acmeprodlake.dfs.core.windows.net/x): the submatch is the
// account, three to twenty-four letters and digits before the host's first dot.
// The host itself is the host name rule's value, and the account is reported on
// its own beside it, as it is the customer's globally unique name.
var storageAccountURLPattern = regexp.MustCompile(`(?i)\b(?:abfss?|wasbs?)://[a-z0-9-]+@([a-z0-9]{3,24})\.`)

// bucketARNPattern finds the bucket an S3 ARN names, in any AWS partition
// (arn:aws:s3:::acme-prod-bucket, arn:aws-cn:s3:::..., arn:aws-us-gov:s3:::...):
// the ARN up to the end of the bucket's name, an object key after a slash left
// out.
var bucketARNPattern = regexp.MustCompile(`(?i)\barn:aws[a-z-]*:s3:::[a-z0-9][a-z0-9_.-]*`)

// cloudStoragePathPattern finds the bucket a Cloud Storage path names after the
// service host (storage.googleapis.com, which the host name rule accepts as a
// provider API host like compute.googleapis.com and its siblings), in one of two
// forms, the first tried first at each position:
//
//   - a JSON API path, on www.googleapis.com or the service host, whose
//     storage/v1/b segment an upload or a download path may open
//     (https://www.googleapis.com/upload/storage/v1/b/acme-prod-data/o, the
//     asset name //storage.googleapis.com/storage/v1/b/acme-prod-data): the
//     host, the path and the bucket after b/;
//   - a URL (https://storage.googleapis.com/acme-prod-data/key) or a Cloud
//     Asset name (//storage.googleapis.com/acme-prod-data): the service host, a
//     slash and the bucket.
//
// The hosts and the JSON API's segments are matched case-insensitively. In a
// JSON API path the b/ segment makes what follows a bucket, so the bucket is
// matched in any case there (www.googleapis.com/storage/v1/b/Acme-Prod): a name
// written with a capital is the customer's all the same. After the service host
// alone a bucket is matched in lower case only, the case Cloud Storage names one
// in, so the asset type (storage.googleapis.com/Bucket), whose segment a capital
// opens, is not read. The bucket is the path's last segment in either form.
var cloudStoragePathPattern = regexp.MustCompile(
	`(?i:(?:www|storage)\.googleapis\.com/(?:upload/|download/)?storage/v1/b)/[A-Za-z0-9][A-Za-z0-9_.-]*` +
		`|(?i:storage\.googleapis\.com)/[a-z0-9][a-z0-9_.-]*`)

// sampleBucketPattern matches a sample's bucket name, lowercased: example, or
// example- and generic words joined by hyphens (example-bucket,
// example-logs-backup), each optionally ending in a group of one to three digits
// (example-logs-2), the count a sample numbers its buckets with. A name after
// the prefix (example-acme-prod) or a longer run of digits
// (example-logs-210987654321, an account ID or a customer's own suffix) does not
// match.
var sampleBucketPattern = regexp.MustCompile(`^example(?:-(?:bucket|logs|data|backup|assets|archive|media))*(?:-[0-9]{1,3})?$`)

// sampleBucket reports whether bucket, as written, is a sample's bucket name
// (sampleBucketPattern), matched case-insensitively.
func sampleBucket(bucket string) bool {
	return sampleBucketPattern.MatchString(strings.ToLower(bucket))
}

// sampleOwner is the owner of this project's own repository and images, the
// one account a path after a vendor host names that is no customer's.
const sampleOwner = "timothymamo"

// publicOpenSourceOwners are the public open-source organisations and
// maintainers whose repositories a guide page or a fixture links to after a
// vendor host: each is a well-known public project's account, never a
// customer's. The match is exact and case-insensitive, and the list fails open
// for exactly these names.
var publicOpenSourceOwners = []string{
	// The sources' official SDK and tool organisations (the AWS, Azure and
	// Google SDKs for Go).
	"aws", "Azure", "google", "GoogleContainerTools",
	// The project that signs and verifies the release images (cosign).
	"sigstore",
	// dot-agent-deck's maintainer, whose public tool drives the agents.
	"vfarcic",
	// ShellCheck's maintainer, the shell linter the dev VM installs.
	"koalaman",
	// The container runtime's organisation (nerdctl), which the dev VM installs.
	"containerd",
	// The GitHub CLI's organisation.
	"cli",
	// golangci-lint's organisation, whose install script is fetched raw.
	"golangci",
	// The scheduled audit's pinned tools (PRD #175, ADR 0040), whose releases
	// the audit workflow's accepted shape downloads: gitleaks, zizmor, age
	// (FiloSottile) and actionlint (rhysd).
	"gitleaks", "zizmorcore", "FiloSottile", "rhysd",
}

// vendorPathProblems returns a problem for every owner a path names after a
// vendor host whose first segment is an account (ownerPathPattern) or behind the
// GitHub API's endpoint segment (apiOwnerPathPattern), for every bucket a bucket
// URL (bucketPattern), an S3 ARN (bucketARNPattern) or a Cloud Storage path
// (cloudStoragePathPattern) names, and for every storage account an abfs or wasb
// URL names (storageAccountURLPattern), when it is no sample value: an owner
// must be sampleOwner or one of publicOpenSourceOwners, a bucket must be a
// sample's (sampleBucket: example, or example- and generic words), the way a
// sample's domain is example.com, and a storage account must be example and
// generic words (sampleStorageAccount). The host name rule exempts the vendor
// host itself, yet the owner or the bucket after it names the customer just as
// a host would, so the value reported is the host and the owner
// (github.com/acme, from git@github.com:acme/x.git too, api.github.com/acme
// from api.github.com/repos/acme/x), the scheme and the bucket
// (s3://acme-prod-bucket, oci://acme-prod-bucket) or the ARN
// (arn:aws:s3:::acme-prod-bucket), each as written, the host and the Cloud
// Storage path up to the bucket, the host and the path in lower case and the
// bucket as written (storage.googleapis.com/acme-prod-data,
// www.googleapis.com/upload/storage/v1/b/acme-prod-data,
// www.googleapis.com/storage/v1/b/Acme-Prod), or the storage account alone
// (acmeprodlake, beside the host the host name rule reports).
//
// It knowingly leaves these gaps, accepted when issue #406 closed:
//
//   - an endpoint segment after github.com itself: github.com/orgs/acme
//     reports github.com/orgs, which fails closed but names the wrong value,
//     and the reader finds the owner next to it;
//   - a port in an SSH URL (ssh://git@github.com:22/acme/x), whose colon is no
//     SSH clone form: it reports github.com/22, which fails closed but names
//     the wrong value, for the same reason;
//   - contrived paths the owner pattern does not read past (github.com//acme,
//     a .. segment, an encoded slash %2F), which pass, and JSON-escaped slashes
//     (gs:\/\/acme-prod-data, storage.googleapis.com\/acme-prod-data), which no
//     pattern reads, so they pass: no tool writes a fixture that way, and
//     reading every spelling of a slash would make each pattern unreadable;
//   - an object key after a sample bucket (s3://example-bucket/customers/acme-corp/x):
//     only the bucket is judged, so a customer's name in the key passes, as a
//     key is free text the check cannot tell from a sample's own;
//   - a dotted OCI bucket without its namespace (oci://acme.prod.backups/x):
//     it is passed over as a registry host, and the host name rule passes it
//     when its last label is no listed TLD, as without an @ a dotted segment
//     cannot be told from a registry host; OCI Object Storage URIs carry
//     bucket@namespace;
//   - a Cloud Storage bucket written with capitals after the service host
//     alone: Cloud Storage names are lower case, so the URL and asset-name form
//     stops before the first capital (storage.googleapis.com/Acme-prod passes),
//     which keeps the asset type (storage.googleapis.com/Bucket) clean; the
//     JSON API path's b/ segment reads one in any case;
//   - the paths of every other vendor host publicVendorHosts lists
//     (golang.org/x/..., www.ovhcloud.com/en/...), whose segments are the
//     vendor's own and name no customer.
func vendorPathProblems(text string) []problem {
	var problems []problem

	ownerRule := "names an owner after a vendor host that is neither " + sampleOwner + " nor a listed public open-source owner (CLAUDE.md rule 2)"

	for _, match := range apiOwnerPathPattern.FindAllStringIndex(text, -1) {
		start, end := match[0], match[1]

		if start > 0 && (labelByte(text[start-1]) || text[start-1] == '.' || text[start-1] == '-') {
			// A name below the API host: the host name rule reads it.
			continue
		}

		value := text[start:end]
		owner := strings.TrimRight(value[strings.LastIndex(value, "/")+1:], ".")

		if publicOwner(owner) {
			continue
		}

		problems = append(problems, problem{line: lineOf(text, start), value: value[:len("api.github.com")] + "/" + owner, rule: ownerRule})
	}

	for _, match := range ownerPathPattern.FindAllStringIndex(text, -1) {
		start, end := match[0], match[1]

		if start > 0 && (labelByte(text[start-1]) || text[start-1] == '.' || text[start-1] == '-') {
			// A name below the host or one that merely ends with it: the host
			// name rule reads it.
			continue
		}

		value := text[start:end]
		sep := strings.IndexAny(value, "/:")
		host := value[:sep]

		if value[sep] == ':' && (!strings.EqualFold(host, sshCloneHost) || start == 0 || text[start-1] != '@') {
			// A colon after the host is the SSH clone form only right after
			// the user's @; anywhere else it is a port or prose.
			continue
		}

		owner := strings.TrimRight(value[sep+1:], ".")

		if publicOwner(owner) {
			continue
		}

		problems = append(problems, problem{line: lineOf(text, start), value: host + "/" + owner, rule: ownerRule})
	}

	const bucketRule = "names a bucket that is not example or example- and generic words (CLAUDE.md rule 2)"

	for _, match := range bucketPattern.FindAllStringIndex(text, -1) {
		value := strings.TrimRight(text[match[0]:match[1]], ".")
		sep := strings.Index(value, "://")
		bucket := value[sep+len("://"):]

		if strings.EqualFold(value[:sep], "oci") && strings.Contains(bucket, ".") &&
			(match[1] == len(text) || text[match[1]] != '@') {
			// A registry host, not a bucket: a bucket's dotted name is
			// followed by the @ of its namespace.
			continue
		}

		if sampleBucket(bucket) {
			continue
		}

		problems = append(problems, problem{line: lineOf(text, match[0]), value: value, rule: bucketRule})
	}

	for _, match := range bucketARNPattern.FindAllStringIndex(text, -1) {
		value := strings.TrimRight(text[match[0]:match[1]], ".")

		if sampleBucket(value[strings.Index(value, ":::")+len(":::"):]) {
			continue
		}

		problems = append(problems, problem{line: lineOf(text, match[0]), value: value, rule: bucketRule})
	}

	for _, match := range cloudStoragePathPattern.FindAllStringIndex(text, -1) {
		start := match[0]

		if start > 0 && (labelByte(text[start-1]) || text[start-1] == '.' || text[start-1] == '-') {
			// A name below the service host (acme.storage.googleapis.com) or
			// below www.googleapis.com is the host name rule's value.
			continue
		}

		// The host and the JSON API's segments are reported in lower case, the
		// bucket as written.
		value := strings.TrimRight(text[start:match[1]], ".")
		slash := strings.LastIndex(value, "/")
		bucket := value[slash+1:]

		if sampleBucket(bucket) {
			continue
		}

		problems = append(problems, problem{line: lineOf(text, match[0]), value: strings.ToLower(value[:slash+1]) + bucket, rule: bucketRule})
	}

	for _, match := range storageAccountURLPattern.FindAllStringSubmatchIndex(text, -1) {
		account := text[match[2]:match[3]]

		if sampleStorageAccount(account) {
			continue
		}

		problems = append(problems, problem{
			line:  lineOf(text, match[2]),
			value: account,
			rule:  "names an Azure storage account that is not example and generic words (CLAUDE.md rule 2)",
		})
	}

	return problems
}

// publicOwner reports whether owner, as written, is an owner a vendor path may
// name: sampleOwner or one of publicOpenSourceOwners, matched
// case-insensitively.
func publicOwner(owner string) bool {
	return strings.EqualFold(owner, sampleOwner) ||
		slices.ContainsFunc(publicOpenSourceOwners, func(public string) bool { return strings.EqualFold(owner, public) })
}

// lastLabel returns the label after the last dot of name.
func lastLabel(name string) string {
	return name[strings.LastIndex(name, ".")+1:]
}

// hyphenatedIPv4Pattern finds an IPv4 address written with hyphens or
// underscores rather than dots: four runs of one to three digits joined by the
// same separator throughout, the way AWS and Azure write an address into a
// machine's name (ip-10-1-2-3, ip-10-1-2-3.eu-central-1.compute.internal) and a
// page that copies one may (ip_10_1_2_3). Go's regexp has no back-reference, so
// each separator has its own alternative. A run of four digits cannot match, so
// the year of a date (2026-11-20) never starts one, and a date has three groups
// rather than four; the shape alone is still no proof, so a candidate must stand
// apart from the letters and digits around it (hyphenatedIPv4Bounded), have
// groups that are octets (parseHyphenatedIPv4), and be an address written into
// a name (hyphenatedIPv4InName) or a private one (hyphenatedIPv4StandsAlone).
var hyphenatedIPv4Pattern = regexp.MustCompile(`[0-9]{1,3}(?:-[0-9]{1,3}){3}|[0-9]{1,3}(?:_[0-9]{1,3}){3}`)

// sharedAddressNet is the shared address space of RFC 6598 (carrier-grade NAT),
// which netip.Addr.IsPrivate does not count: a customer's network uses it as it
// uses a private range.
var sharedAddressNet = netip.MustParsePrefix("100.64.0.0/10")

// englishWordsEndingInIP are the English words a hyphen and a number may follow
// in prose (ship-1-2-3-4, zip-10-1-2-3): a word ending in ip is no ip prefix,
// and before a private-looking run it is no label that marks an address either,
// so hyphenatedIPv4StandsAlone leaves the run after one of them clean, when the
// word stands on its own (no - or _ before it: edge-strip- is a name). Any other
// word a hyphen follows (node-, vm-, eip-, hostip-, nat-) marks the run as the
// address of the thing it names. The list fails open for exactly these words.
var englishWordsEndingInIP = []string{
	"chip", "clip", "dip", "drip", "flip", "grip", "hip", "lip", "ship",
	"skip", "slip", "strip", "tip", "trip", "whip", "zip",
}

// hyphenatedIPv4Problems returns a problem for every IPv4 address the fixture
// check reads written with hyphens or underscores, when the address is outside
// the documentation range every sample address is in (documentationPrefix), the
// same one range the dotted rule holds a sample to. It reports the run as it is
// written, separators and leading zeros included, without any prefix before it.
// The .internal suffix already catches a full AWS host name
// (ip-10-1-2-3.eu-central-1.compute.internal) through the host name rule, but a
// bare ip-10-1-2-3 carries no dot at all, so without this rule a customer's
// private address written that way would pass.
//
// A run is read as an address in either of two ways. Written into a name - after
// an ip- or ip_ that stands on its own, or ending a label that another label
// follows after a dot (hyphenatedIPv4InName) - every address outside the
// documentation range is reported. Otherwise - bare (10-1-2-3), behind a label
// of its own (node-10-1-2-3, eip-10-1-2-3, hostip-172-16-0-5) or ending a
// sentence (10-1-2-3.) - only a private (10/8, 172.16/12, 192.168/16), shared
// (100.64/10) or link-local (169.254/16) address is reported
// (hyphenatedIPv4StandsAlone): a public-looking run in prose (5-10-20-30,
// 1-2-3-4) is a sequence of numbers as often as an address. A private run is
// reported in a CIDR written into a name too (vpc-10-0-0-0-16,
// subnet_10_0_0_0_24): a fifth group after it, with the same separator, of 0 to
// 32 and with nothing further, is a prefix length (cidrSuffix), and so is one a
// further digit group follows when a label stands before the run
// (subnet-10-0-1-0-24-2, vpc_10_0_0_0_16_1, which fails closed, as does prose
// such as steps-10-1-2-3-4-5, which reports 10-1-2-3), while any other fifth
// group (10-1-2-3-45, vpc-10-0-0-0-33, vpc-10-0-0-0-016, the bare 10-1-2-3-4-5)
// or one after a public-looking run (build-1-2-3-4-5) leaves the run clean. An
// English word ending in ip before a private run excuses it only when the word
// stands on its own (zip-10-1-2-3); inside a compound name (edge-strip-10-0-0-5,
// node_tip_10_1_2_3) it marks the address like any other label.
//
// It knowingly leaves these forms open rather than report a value it cannot tell
// from prose, gaps accepted when issue #406 closed (a follow-up may narrow one
// that a fixture turns out to hold):
//
//   - a public address in no name (vm-5-10-20-30, 8-8-8-8 in prose): it passes,
//     like the sequence of numbers it cannot be told from;
//   - a private address after one of the English words of
//     englishWordsEndingInIP standing on its own (zip-10-1-2-3), or after a
//     label that is a group of digits (eu-central-1_10-1-2-3, where the
//     underscore keeps the leftmost match from starting at the 1), unless a
//     dotted label follows; the English word counts as standing on its own
//     after a . or a / too, so x.zip-10-1-2-3 and a/zip-10-1-2-3 stay clean;
//   - a run a letter or digit is glued to (node10-1-2-3, ip-10-1-2-3a), which
//     hyphenatedIPv4Bounded keeps out;
//   - a hexadecimal address (ip-0a010203), which the pattern never matches, as
//     eight hex digits are any hash or identifier's shape as well;
//   - a run a leftmost match swallows: in x-5-10-1-2-3 the pattern matches
//     5-10-1-2 from the leftmost digit, and the address 10.1.2.3 inside the
//     five groups is never looked at again; so too in eu-central-1-10-1-2-3,
//     where the public-looking 1-10-1-2 is read and dropped, since rereading
//     every offset of a longer run would report version-like runs of numbers;
//   - a run with mixed separators (ip-10_1-2-3), which the pattern never
//     matches, as no provider writes an address that way.
func hyphenatedIPv4Problems(text string) []problem {
	var problems []problem

	for _, match := range hyphenatedIPv4Pattern.FindAllStringIndex(text, -1) {
		start, end := match[0], match[1]

		if !hyphenatedIPv4Bounded(text, start, end) {
			continue
		}

		candidate := text[start:end]

		addr, ok := parseHyphenatedIPv4(candidate)
		if !ok {
			continue
		}

		if documentationPrefixNet.Contains(addr) {
			continue
		}

		if !hyphenatedIPv4InName(text, start, end) && !hyphenatedIPv4StandsAlone(text, start, end, addr) {
			continue
		}

		problems = append(problems, problem{
			line:  lineOf(text, match[0]),
			value: candidate,
			rule:  "is an IPv4 address written with hyphens or underscores, outside the documentation range " + documentationPrefix + " (CLAUDE.md rule 2)",
		})
	}

	return problems
}

// hyphenatedIPv4Bounded reports whether the run text[start:end] stands apart
// from the letters and digits around it: no letter or digit right before it
// (a10-1-2-3, 2026-1-2-3 is no run of its own) or right after it, so the
// pattern's match is the whole run. A hyphen, an underscore, a dot or any other
// byte may border it.
func hyphenatedIPv4Bounded(text string, start, end int) bool {
	if start > 0 && labelByte(text[start-1]) {
		return false
	}

	return end == len(text) || !labelByte(text[end])
}

// hyphenatedIPv4StandsAlone reports whether the run text[start:end], read as
// addr, is a private, shared or link-local address written where nothing marks
// it as a name: bare (10-1-2-3), behind a label of its own that letters spell
// (node-10-1-2-3, eip-10-1-2-3, hostip-172-16-0-5, nat-100-64-1-2) or ending a
// sentence (10-1-2-3.). The run must end there, with no further group after a
// separator but a CIDR prefix length (cidrSuffix: vpc-10-0-0-0-16, and so
// 10-1-2-3-4 is read as 10.1.2.3/4, failing closed, while 10-1-2-3-45 and
// 10-1-2-3-4-5 are no address) and, behind a label, one more digit group after
// that prefix length (subnet-10-0-1-0-24-2, failing closed, and so prose such
// as steps-10-1-2-3-4-5 too), and a label before it must be no word of
// englishWordsEndingInIP that stands on its own (zip-10-1-2-3, but not the
// strip of edge-strip-10-0-0-5) and no group of digits (the 1 of
// eu-central-1_10-1-2-3; in eu-central-1-10-1-2-3 the leftmost match is
// 1-10-1-2, so this run is never read).
func hyphenatedIPv4StandsAlone(text string, start, end int, addr netip.Addr) bool {
	if !addr.IsPrivate() && !sharedAddressNet.Contains(addr) && !addr.IsLinkLocalUnicast() {
		return false
	}

	sep := text[start+strings.IndexAny(text[start:end], "-_")]
	labelled := start > 0 && (text[start-1] == '-' || text[start-1] == '_')

	if end+1 < len(text) && text[end] == sep && isDigit(text[end+1]) && !cidrSuffix(text, end, sep, labelled) {
		return false
	}

	if !labelled {
		return true
	}

	word := start - 1
	for word > 0 && labelByte(text[word-1]) {
		word--
	}

	label := strings.ToLower(text[word : start-1])
	if label != "" && strings.Trim(label, "0123456789") == "" {
		// A group of digits before the separator: the run is the tail of a
		// longer run of numbers, not an address of its own.
		return false
	}

	if word > 0 && (text[word-1] == '-' || text[word-1] == '_') {
		// The word is part of a compound name (edge-strip-, node_tip_), no
		// English word of its own: the name marks the address.
		return true
	}

	return !slices.Contains(englishWordsEndingInIP, label)
}

// cidrSuffix reports whether text[end:] opens with the prefix length of a CIDR
// written into a name after a run (vpc-10-0-0-0-16, subnet_10_0_0_0_24): the
// run's separator sep, one group of one or two digits from 0 to 32, and nothing
// further, that is the end of the text or a byte that is neither a letter or
// digit nor sep before another digit. When a label precedes the run (labelled),
// a further digit group after the prefix length is accepted as well
// (subnet-10-0-1-0-24-2, vpc_10_0_0_0_16_1): it may be a node's index after a
// subnet's CIDR as well as a sequence of numbers, and the rule fails closed. A
// bare run (10-1-2-3-4-5) has no label that marks it as a name, so there the
// further group still makes it no CIDR.
func cidrSuffix(text string, end int, sep byte, labelled bool) bool {
	group := end + 1
	for group < len(text) && isDigit(text[group]) {
		group++
	}

	if group-(end+1) > 2 {
		return false
	}

	if n, err := strconv.Atoi(text[end+1 : group]); err != nil || n > 32 {
		return false
	}

	if group == len(text) {
		return true
	}

	if labelByte(text[group]) {
		return false
	}

	return labelled || text[group] != sep || group+1 == len(text) || !isDigit(text[group+1])
}

// isDigit reports whether b is an ASCII digit.
func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

// hyphenatedIPv4InName reports whether the four-group run text[start:end] is an
// address written into a name rather than a run of numbers that merely has its
// shape: the run either follows an ip- or ip_ that stands on its own, the prefix
// AWS and Azure give an address in a machine's name (ip-10-1-2-3), or it ends a label
// that another label follows after a dot (10-1-2-3.eu-central-1.compute.internal,
// and so vm-10-1-2-3.acme.corp and a.10-1-2-3.b too). A documentation domain
// name can hold one as well: 10-1-2-3.example.com names a private address
// although its domain identifies nobody, so the address alone is reported. A run
// of four numbers in prose
// (1-2-3-4) or at the end of a version-like token (build-1-2-3-4) is neither: the
// ip- inside ship- or zip- is no prefix (hyphenatedIPv4AfterIPPrefix), and the
// full stop that ends a sentence opens no label, so the rule stays off values that
// only look like an address; parseHyphenatedIPv4 then keeps out every run whose
// groups are not octets and every run of digits with a leading zero.
//
// Version-like tokens are clean only while they end the label. The same token
// with a dotted extension (build-1-2-3-4.tar.gz, release-1-2-3-4.md) ends a
// label that a dotted one follows, so it is read as the address 1.2.3.4 and
// reported: the rule cannot tell such a version from an address in a name, and
// it fails closed, so TestHyphenatedIPv4Problems pins the report.
func hyphenatedIPv4InName(text string, start, end int) bool {
	if hyphenatedIPv4AfterIPPrefix(text, start) {
		return true
	}

	return strings.HasPrefix(text[end:], ".") && end+1 < len(text) && labelByte(text[end+1])
}

// hyphenatedIPv4AfterIPPrefix reports whether text[:start] ends with an ip-
// (or ip_) prefix that stands on its own: one that starts the text, or one whose
// byte before is no letter or digit, so the ip- inside ship- or zip- is no
// prefix. The match is case-insensitive, since a name is written in capitals as
// often as not.
func hyphenatedIPv4AfterIPPrefix(text string, start int) bool {
	prefix := start - len("ip-")
	if prefix < 0 || !strings.EqualFold(text[prefix:start-1], "ip") || (text[start-1] != '-' && text[start-1] != '_') {
		return false
	}

	if prefix == 0 {
		return true
	}

	return !labelByte(text[prefix-1])
}

// labelByte reports whether b is a byte a dotted name's label is built from: an
// ASCII letter or digit, the characters hostnamePattern builds a label from.
func labelByte(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// parseHyphenatedIPv4 parses candidate, four groups of one to three digits
// separated by hyphens or underscores, as an IPv4 address: each group is read as
// a decimal number, leading zeros included (10-01-2-3 is 10.1.2.3, which
// netip.ParseAddr would refuse), and is an octet, so a group above 255 makes the
// candidate no address at all, and it is then not reported.
func parseHyphenatedIPv4(candidate string) (netip.Addr, bool) {
	groups := strings.FieldsFunc(candidate, func(r rune) bool { return r == '-' || r == '_' })
	if len(groups) != 4 {
		return netip.Addr{}, false
	}

	var octets [4]byte

	for i, group := range groups {
		n, err := strconv.ParseUint(group, 10, 8)
		if err != nil {
			return netip.Addr{}, false
		}

		octets[i] = byte(n)
	}

	return netip.AddrFrom4(octets), true
}

// fixtureProblems runs the sanitisation check over every checked file below
// root, leaving out the values realDataAllowList excuses in that file. The
// problems are keyed by the file's name; a clean file has no key. Each file is
// read once, through an os.Root, as text (fixtureText), so no encoding hides a
// value. A symbolic link is not followed: it is a problem in itself, whose value
// is the link's name.
func fixtureProblems(root string) (map[string][]problem, error) {
	files, err := checkedFiles(root)
	if err != nil {
		return nil, err
	}

	problems := make(map[string][]problem)

	if len(files) == 0 {
		return problems, nil
	}

	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}

	defer func() { _ = dir.Close() }()

	for _, file := range files {
		info, err := dir.Lstat(file.name)
		if err != nil {
			return nil, err
		}

		if info.Mode()&fs.ModeSymlink != 0 {
			problems[file.name] = append(problems[file.name], problem{
				value: file.name,
				rule:  "is a symbolic link; fixtures must be regular files",
			})

			continue
		}

		data, err := dir.ReadFile(file.name)
		if err != nil {
			return nil, err
		}

		for _, p := range sanitisationProblems(fixtureText(data)) {
			if allowListed(file.name, p.value) {
				continue
			}

			problems[file.name] = append(problems[file.name], p)
		}
	}

	return problems, nil
}

// fixtureText returns the text a checked file holds, whatever encoding the tool
// that wrote it chose. A file that starts with a UTF-16 byte-order mark (the way
// a Windows editor or PowerShell writes one) is decoded from UTF-16, little- or
// big-endian; any other file is read as UTF-8 with every invalid byte (a stray
// Latin-1 letter, a binary run) replaced by a space, so one bad byte cannot hide
// the values in the rest of the file.
func fixtureText(data []byte) string {
	if len(data) >= 2 {
		switch {
		case data[0] == 0xff && data[1] == 0xfe:
			return decodeUTF16(data[2:], func(b []byte) uint16 { return uint16(b[0]) | uint16(b[1])<<8 })
		case data[0] == 0xfe && data[1] == 0xff:
			return decodeUTF16(data[2:], func(b []byte) uint16 { return uint16(b[0])<<8 | uint16(b[1]) })
		}
	}

	return strings.ToValidUTF8(string(data), " ")
}

// decodeUTF16 returns data, UTF-16 code units in the byte order unit reads,
// as UTF-8. A trailing odd byte is dropped.
func decodeUTF16(data []byte, unit func([]byte) uint16) string {
	units := make([]uint16, 0, len(data)/2)

	for i := 0; i+1 < len(data); i += 2 {
		units = append(units, unit(data[i:i+2]))
	}

	return string(utf16.Decode(units))
}

// allowListed reports whether realDataAllowList excuses value in the file name.
func allowListed(name, value string) bool {
	return slices.ContainsFunc(realDataAllowList, func(entry allowListEntry) bool {
		return entry.file == name && entry.value == value
	})
}

// staleAllowListEntry is an allow-list entry the check no longer needs, with the
// values the check does report in its file: the entry excuses none of them.
type staleAllowListEntry struct {
	// entry is the entry as the allow list holds it.
	entry allowListEntry
	// reported are the values sanitisationProblems reports in the entry's file.
	reported []string
}

// staleAllowListEntries returns every entry of list that the fixture check in
// root no longer needs: the entries whose file holds no value
// sanitisationProblems reports. root is the directory the entries' paths are
// relative to, so a test tree is judged the way the module is, and each result
// carries the values the check does report in that file, so a failure says what
// the entry no longer covers. The file is decoded as the check decodes it
// (fixtureText), so an encoding cannot make an entry that is still in use look
// stale.
//
// An entry whose file cannot be read is not judged, and it does not end the
// pass: its read error is collected and the entries after it are judged too, so
// one broken entry cannot hide a stale one. The returned error names every such
// entry, one error each (errors.Join), and a pass whose files all read returns
// none. A missing or non-regular file is what the allow list's structural checks
// report already, so the caller reports this error beside them rather than
// stopping at it.
func staleAllowListEntries(root string, list []allowListEntry) ([]staleAllowListEntry, error) {
	var (
		stale []staleAllowListEntry
		read  []error
	)

	for _, entry := range list {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entry.file)))
		if err != nil {
			read = append(read, fmt.Errorf("read %s for its allow-list entry: %w", entry.file, err))

			continue
		}

		reported := problemValues(sanitisationProblems(fixtureText(data)))
		if slices.Contains(reported, entry.value) {
			continue
		}

		stale = append(stale, staleAllowListEntry{entry: entry, reported: reported})
	}

	return stale, errors.Join(read...)
}
