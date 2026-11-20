package examples_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestSanitisationProblemsHostNameRule pins which dotted names the real-data
// check (issue #249) reads as a host name. Over examples/ every dotted name
// that is not a documentation name fails (TestSamplesAreSanitised), but the
// files the fixture check also reads - the guide pages under docs/guide/ and
// every testdata/ directory - are full of dotted values that identify no host:
// file names (inventory.json, tar.gz), versions (v0.2.0), jq and JSON field
// paths (sku.name, properties.storageProfile.dataDisks), plan names (m5.large,
// rdb.standard.2CPU-8GB), an Azure resource namespace (microsoft.compute), Go
// selectors in a Go-source fixture (testing.T, os.Getenv) and the public vendor
// and project hosts a page links to (github.com, www.scaleway.com). A rule that
// flagged those would report some sixteen hundred legitimate values, so the
// fixture check narrows it: a dotted name is a host name only if its last
// label, lowercased, is a public top-level domain (com, net, org, io, dev, app,
// cloud, ai, co, plus the generic info, biz, tech, online, site, xyz; every
// EU/EEA country code, so de, fr, nl, uk, ch, at, be, se, fi, no, dk, es, it, pl,
// us, ie, lu, cz, pt and the rest; and OVHcloud's own ovh - deliberately not md,
// sh, py, rs or another file-extension clash), or if it ends in one of the
// private DNS suffixes an internal name is built from (.internal, the AWS
// and GCP private zone; .corp; .lan), and it is not one of the exact public
// vendor hosts a page may link to (github.com, www.scaleway.com, ... ). The
// vendor list is exact, never a suffix match: the subdomain of a listed vendor
// host is a customer's name and is reported.
//
// Two suffixes are country code and private zone and Go selector at once:
// Iceland's is (errors.Is) and local (time.Local). They are read as hosts only
// when the name's last label is written in exactly that lower case, so
// db01.corp.local, db01.Corp.local and shop.example.is are reported and the
// selectors, whose last label a capital opens, are not (lowercaseOnlyTLDs).
//
// The account-ID, GUID, IPv4 and IPv6 rules are unchanged, and the names the
// examples/ allow list already accepts (example.com and its subdomains, the
// reserved .example, .test and .invalid names) stay accepted. An IPv4 address
// written into a name with hyphens (ip-10-0-1-23) is reported beside the name
// that holds it (hyphenatedIPv4Problems).
func TestSanitisationProblemsHostNameRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []string
	}{
		// Reported: a dotted name whose last label is a public top-level
		// domain, so it stands for a customer's own host.
		{
			name: "customer host in the .io TLD",
			text: "the shop runs on shop.acme.io",
			want: []string{"shop.acme.io"},
		},
		{
			name: "customer host in the .net TLD",
			text: `"endpoint": "api.acme-corp.net"`,
			want: []string{"api.acme-corp.net"},
		},
		{
			name: "customer host under a country-code TLD",
			text: `"host": "db.internal.contoso.de"`,
			want: []string{"db.internal.contoso.de"},
		},
		{
			name: "customer host in a URL",
			text: "the console is at https://portal.acme-corp.com/x",
			want: []string{"portal.acme-corp.com"},
		},
		{
			// The rule reports the dot-separated name it matches, so an e-mail
			// address reports its domain part, from the label after the "@" on.
			name: "customer host in an e-mail address",
			text: "write to ops@acme-corp.eu for access",
			want: []string{"acme-corp.eu"},
		},
		{
			name: "customer host with an uppercase TLD",
			text: "the shop runs on Shop.Acme.IO",
			want: []string{"Shop.Acme.IO"},
		},
		{
			// The vendor allow list is exact: a name that only ends in a listed
			// vendor host is a customer's name, not the vendor's.
			name: "subdomain of a listed vendor host that is itself unlisted",
			text: "the assets come from evil.ghcr.io.acme-corp.com",
			want: []string{"evil.ghcr.io.acme-corp.com"},
		},
		{
			// The same exactness, pinned where a suffix match would still flag
			// the lookalike above: the listed host itself sits beside the true
			// subdomain, so a rule that let any name ending in github.com pass
			// would report nothing at all here.
			name: "true subdomain of a listed vendor host beside the listed host",
			text: "cloned from github.com/timothymamo/shift2eu, and evil.github.com/u/x serves the assets",
			want: []string{"evil.github.com"},
		},
		{
			// .local is the suffix of a customer's own internal zone (and of
			// mDNS), and a Go selector reads as time.Local ends with it too, so
			// only a name whose last label is the lower-case suffix itself is
			// read as a host.
			name: "private name under the local suffix",
			text: "the file server is db01.corp.local",
			want: []string{"db01.corp.local"},
		},
		{
			// Only the last label has to be the exact lower-case suffix: a
			// customer's own internal name capitalises the labels before it all
			// the time (a Windows domain, a department), and none of them is a
			// Go selector.
			name: "mixed-case private name under the local suffix",
			text: "the file server is db01.Corp.local",
			want: []string{"db01.Corp.local"},
		},
		{
			// Iceland's is is a real country code and errors.Is ends with it just
			// as time.Local ends with local, so the match is the same one.
			name: "customer host in the Icelandic TLD",
			text: "the shop answers at shop.example.is",
			want: []string{"shop.example.is"},
		},
		{
			name: "mixed-case customer host in the Icelandic TLD",
			text: "the shop answers at Shop.Example.is",
			want: []string{"Shop.Example.is"},
		},
		{
			name: "customer host in the Luxembourg TLD",
			text: "the database is at db.acme.lu",
			want: []string{"db.acme.lu"},
		},
		{
			name: "customer host in the Irish TLD",
			text: "the shop answers at shop.acme.ie",
			want: []string{"shop.acme.ie"},
		},
		{
			name: "customer host in the Czech TLD",
			text: `"endpoint": "api.acme.cz"`,
			want: []string{"api.acme.cz"},
		},
		{
			name: "customer host in the Portuguese TLD",
			text: "the portal is portal.acme.pt",
			want: []string{"portal.acme.pt"},
		},
		{
			// OVHcloud's own TLD: a customer of the target hosts its site there
			// just as it does under any other TLD.
			name: "customer host in the OVHcloud TLD",
			text: "the site is on mysite.ovh",
			want: []string{"mysite.ovh"},
		},
		{
			// An AWS private DNS name: .internal is no public TLD, but the name
			// still identifies a customer's own network. The address inside it is
			// reported on its own as well (hyphenatedIPv4Problems).
			name: "AWS private DNS name",
			text: `"hostname": "ip-10-0-1-23.eu-central-1.compute.internal"`,
			want: []string{"10-0-1-23", "ip-10-0-1-23.eu-central-1.compute.internal"},
		},
		{
			name: "GCP internal DNS name",
			text: `"hostname": "vm-1.europe-west1-b.c.acme-prod.internal"`,
			want: []string{"vm-1.europe-west1-b.c.acme-prod.internal"},
		},
		{
			name: "private name under the corp suffix",
			text: "the file server is db01.acme.corp",
			want: []string{"db01.acme.corp"},
		},
		{
			name: "private name under the lan suffix",
			text: "the NAS is nas.home.lan",
			want: []string{"nas.home.lan"},
		},
		{
			// The private zones a company builds its internal naming from, and
			// the one RFC 8375 reserves for a home network: none is a public
			// TLD, and each names a host of the customer's own.
			name: "private name under the RFC 8375 home suffix",
			text: "the file server is db01.home.arpa",
			want: []string{"db01.home.arpa"},
		},
		{
			name: "private name under the localdomain suffix",
			text: "the file server is db01.acme.localdomain",
			want: []string{"db01.acme.localdomain"},
		},
		{
			name: "private name under the intranet suffix",
			text: "the portal is db01.acme.intranet",
			want: []string{"db01.acme.intranet"},
		},
		{
			name: "private name under the private suffix",
			text: "the database is db01.acme.private",
			want: []string{"db01.acme.private"},
		},
		{
			name: "private name under the priv suffix",
			text: "the database is db01.acme.priv",
			want: []string{"db01.acme.priv"},
		},
		{
			name: "private name under the home suffix",
			text: "the file server is db01.acme.home",
			want: []string{"db01.acme.home"},
		},
		{
			name: "private name under the consul suffix",
			text: "the service is db01.service.consul",
			want: []string{"db01.service.consul"},
		},
		{
			// arpa is the reverse-DNS suffix, and only home.arpa above is a
			// customer's own zone: a reverse name is left to the dotted-quad
			// rule, which reports the address it spells and not the name, so the
			// host rule never reads the name itself.
			name: "reverse DNS name",
			text: "the zone is 3.2.1.10.in-addr.arpa",
			want: []string{"3.2.1.10"},
		},
		{
			// The curated list of TLDs names its own gaps, and these are the
			// ones the audit found: a government, education or international
			// name, and the two common country codes it left out.
			name: "customer host under the government TLD",
			text: "the database is db.acme.gov",
			want: []string{"db.acme.gov"},
		},
		{
			name: "customer host under the education TLD",
			text: "the portal is lms.acme.edu",
			want: []string{"lms.acme.edu"},
		},
		{
			name: "customer host under the international-organisation TLD",
			text: "the fund answers at portal.acme.int",
			want: []string{"portal.acme.int"},
		},
		{
			name: "customer host under the Indian TLD",
			text: "the shop answers at web.acme.in",
			want: []string{"web.acme.in"},
		},
		{
			name: "customer host under the Brazilian TLD",
			text: "the shop is shop.acme.br",
			want: []string{"shop.acme.br"},
		},
		{
			// us is already on the list, so this row pins that it stays there.
			name: "customer host under the US TLD",
			text: "the shop is shop.acme.us",
			want: []string{"shop.acme.us"},
		},
		// Not reported: a dotted value with another meaning, or a public host
		// that identifies no customer.
		{
			name: "file name with an extension clash",
			text: "the sample file inventory.json holds the resources",
			want: nil,
		},
		{
			name: "another file name with an extension clash",
			text: "see install.md for the steps",
			want: nil,
		},
		{
			name: "archive extension",
			text: "the archived inventory is a tar.gz file",
			want: nil,
		},
		{
			name: "shell script name",
			text: "the script run.sh starts the demo",
			want: nil,
		},
		{
			name: "release version",
			text: "the binary is pinned to v0.2.0",
			want: nil,
		},
		{
			name: "TLS version",
			text: "the API speaks TLSv1.3",
			want: nil,
		},
		{
			name: "jq and JSON field path",
			text: `"path": "sku.name"`,
			want: nil,
		},
		{
			name: "Azure field path",
			text: "the Azure field properties.storageProfile.dataDisks holds them",
			want: nil,
		},
		{
			name: "AWS instance type",
			text: `"instanceType": "m5.large"`,
			want: nil,
		},
		{
			name: "AWS metal instance type",
			text: `"instanceType": "c7i.metal-24xl"`,
			want: nil,
		},
		{
			name: "OVHcloud managed-database plan",
			text: "the plan rdb.standard.2CPU-8GB is the cheapest one",
			want: nil,
		},
		{
			name: "Azure resource namespace in lowercase",
			text: "the namespace microsoft.compute is not a domain",
			want: nil,
		},
		{
			name: "Go selector in a Go-source fixture",
			text: "the test calls testing.T",
			want: nil,
		},
		{
			name: "Go package selector in a Go-source fixture",
			text: "the code calls os.Getenv",
			want: nil,
		},
		{
			// is is a real country code, but a name ending in it is a Go
			// selector far more often than a host, so it is never a host.
			name: "Go errors.Is selector",
			text: "the code checks errors.Is for the sentinel",
			want: nil,
		},
		{
			name: "Go time.Local value",
			text: "the deadline is measured in time.Local",
			want: nil,
		},
		{
			// A method expression ends with the same suffix as the exported
			// field it names, and the capital in its last label is what keeps it
			// out of the host rule.
			name: "Go method expression ending with the local suffix",
			text: "the code reads time.Time.Local from the struct",
			want: nil,
		},
		{
			// go is a file extension before it is a TLD, in a fixture as often
			// as in a guide page.
			name: "Go source file name",
			text: "the entry point is main.go",
			want: nil,
		},
		{
			name: "README file name",
			text: "see README.md for the steps",
			want: nil,
		},
		{
			// in and int are real TLDs and Go selector suffixes at once, so a
			// selector whose last label a capital opens stays clean.
			name: "Go selectors ending in the plain TLDs",
			text: "the probe calls flag.Int and now.In",
			want: nil,
		},
		{
			name: "public vendor host",
			text: "the prices are on www.scaleway.com",
			want: nil,
		},
		{
			name: "documentation domain",
			text: "the docs use example.com",
			want: nil,
		},
		{
			name: "name under the documentation domain",
			text: "the docs use api.example.com",
			want: nil,
		},
		{
			name: "legitimate dotted values side by side",
			text: "the inventory.json sample pins v0.2.0 and clones github.com/timothymamo/shift2eu",
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, values(sanitisationProblems(tc.text))); diff != "" {
				t.Errorf("sanitisationProblems(%q) mismatch (-want +got):\n%s", tc.text, diff)
			}
		})
	}
}

// TestVendorPathProblems pins which owner and which bucket the fixture check
// reads after a vendor host or a bucket scheme. The host name rule exempts the
// public vendor hosts a page links to (publicVendorHosts), yet the path after one
// names the customer just as a host would, so an owner a path names after
// github.com, raw.githubusercontent.com or ghcr.io is reported as the host and
// the owner (github.com/acme), in a URL and in the SSH clone form a reader copies
// (git@github.com:acme/private-repo.git) alike. Only this project's own account
// and the listed public open-source owners pass, so github.com/timothymamo/shift2eu
// and github.com/sigstore/cosign are clean and the same host written in another
// case (GitHub.com/TimothyMamo/shift2eu) is clean too.
//
// A bucket is reported with the scheme that gives it its shape (s3://,
// s3a://, s3n://, gs://, az://) or, in an ARN, whole
// (arn:aws:s3:::acme-prod-bucket). A Cloud Storage path is read by this rule,
// not the owner one, and it names the bucket after the service host
// (storage.googleapis.com), in a URL (storage.googleapis.com/acme-prod-data), in a
// Cloud Asset name (//storage.googleapis.com/acme-prod-data) and in a JSON API path,
// whose www.googleapis.com host and storage/v1/b segment an upload, a download
// or a plain call writes alike: the value reported is the host and the path after
// it. A bucket is a sample value only when it is example or starts with example-
// and generic words follow (example-bucket, example-logs-2, example), the digits
// after the last word one to three of them, never when a name follows the prefix
// (example-acme-prod) or a long run of digits does
// (example-logs-210987654321).
//
// A name below a listed vendor host is no path of that host: evil.github.com is
// the host name rule's value, not this rule's.
func TestVendorPathProblems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			// The vendor allow list excuses the host, and the path after it
			// names the customer just as the host would: only the vendor's own
			// owners are exempt. The reported value is the host and the owner it
			// carries, up to the repository's name.
			name: "customer owner path after a listed vendor host",
			text: "the module is github.com/acme/private-repo",
			want: []string{"github.com/acme"},
		},
		{
			// The form a reader copies out of a clone command names the owner
			// after a colon rather than a slash, and it is the same account.
			name: "customer owner in an SSH clone URL",
			text: "the module is git@github.com:acme/private-repo.git",
			want: []string{"github.com/acme"},
		},
		{
			// ghcr.io is a listed vendor host, and the image path names the
			// customer's own registry account.
			name: "customer owner path after the container registry",
			text: "the image is ghcr.io/acme/app",
			want: []string{"ghcr.io/acme"},
		},
		{
			name: "customer owner path after the raw-file host",
			text: "the script is raw.githubusercontent.com/acme/repo/main/x",
			want: []string{"raw.githubusercontent.com/acme"},
		},
		{
			// A Cloud Storage URL names the customer's bucket after the
			// service host, which the host rule accepts as a provider host.
			name: "customer bucket after the Cloud Storage host",
			text: "the objects are at https://storage.googleapis.com/acme-prod-data/key",
			want: []string{"storage.googleapis.com/acme-prod-data"},
		},
		{
			// A bucket scheme names the customer's storage account, so the
			// bucket is reported with the scheme that gives it its shape.
			name: "customer bucket in an s3 URL",
			text: "the objects are in s3://acme-prod-bucket/key",
			want: []string{"s3://acme-prod-bucket"},
		},
		{
			name: "customer bucket in a gs URL",
			text: "the objects are in gs://acme-prod-bucket/key",
			want: []string{"gs://acme-prod-bucket"},
		},
		{
			// The Hadoop and legacy S3 clients spell the scheme s3a and s3n,
			// and the bucket after them is the same customer's.
			name: "customer bucket in an s3a URL",
			text: "the objects are in s3a://acme-prod-data/key",
			want: []string{"s3a://acme-prod-data"},
		},
		{
			name: "customer bucket in an s3n URL",
			text: "the objects are in s3n://acme-prod-data/key",
			want: []string{"s3n://acme-prod-data"},
		},
		{
			name: "customer container in an az URL",
			text: "the blobs are in az://acme-container/x",
			want: []string{"az://acme-container"},
		},
		{
			// An ARN names a bucket with no scheme, and it carries the account's
			// own name: the ARN is reported whole, so the failure says which
			// value to change.
			name: "customer bucket in an ARN",
			text: `"bucket": "arn:aws:s3:::acme-prod-bucket"`,
			want: []string{"arn:aws:s3:::acme-prod-bucket"},
		},
		{
			// An example- prefix excuses a sample's bucket only when the words
			// after it are generic: a name after it is the customer's.
			name: "customer bucket behind an example- prefix",
			text: "the objects are in s3://example-acme-prod/key",
			want: []string{"s3://example-acme-prod"},
		},
		{
			// A Cloud Storage JSON API call names the bucket after the resource
			// path's segment, and www.googleapis.com is the host it is called on:
			// the host rule's exemption of the service host is no defence, and the
			// value reported is the host and the path after it.
			name: "customer bucket in a Cloud Storage JSON API path",
			text: `"selfLink": "https://www.googleapis.com/storage/v1/b/acme-prod-data"`,
			want: []string{"www.googleapis.com/storage/v1/b/acme-prod-data"},
		},
		{
			name: "customer bucket in a Cloud Storage JSON API upload path",
			text: "the upload goes to https://www.googleapis.com/upload/storage/v1/b/acme-prod-data/o",
			want: []string{"www.googleapis.com/upload/storage/v1/b/acme-prod-data"},
		},
		{
			name: "customer bucket in a Cloud Storage JSON API download path",
			text: "the download comes from https://www.googleapis.com/download/storage/v1/b/acme-prod-data/o",
			want: []string{"www.googleapis.com/download/storage/v1/b/acme-prod-data"},
		},
		{
			// The same path as a Cloud Asset name starts with the double slash an
			// asset name uses, and the value is reported from the host on.
			name: "customer bucket in a Cloud Storage asset name",
			text: `"name": "//storage.googleapis.com/storage/v1/b/acme-prod-data"`,
			want: []string{"storage.googleapis.com/storage/v1/b/acme-prod-data"},
		},
		{
			// A bucket's own letters are lower case in Cloud Storage, so the
			// path's bucket segment stops at the capital and the JSON API path
			// names no bucket: www.googleapis.com is a listed vendor host and
			// reports nothing either, so the value passes. The rule fails open
			// here, a known gap left to the follow-up issue, because a bucket is
			// only ever written in lower case and a capital is no bucket.
			name: "known gap: upper-case bucket in a Cloud Storage JSON API path",
			text: `"selfLink": "https://www.googleapis.com/storage/v1/b/Acme-prod"`,
			want: nil,
		},
		{
			// After the service host itself, the same upper-case bucket is no
			// bucket to the path pattern either, and the pattern's host-only
			// alternative reads the lower-case storage/ segment of the JSON API
			// path instead: the value reported is the host and that segment, so
			// the rule fails closed on what it cannot read rather than pass it.
			name: "fails closed: upper-case bucket in a Cloud Storage path",
			text: "the asset is storage.googleapis.com/storage/v1/b/Acme-prod",
			want: []string{"storage.googleapis.com/storage"},
		},
		{
			// The digits after a generic word are a count (example-logs-2) and a
			// sample writes one to three of them, so a run of twelve is an account
			// ID or another customer's own suffix.
			name: "account-ID digits after a sample bucket prefix in an s3 URL",
			text: "the objects are in s3://example-logs-210987654321/key",
			want: []string{"s3://example-logs-210987654321"},
		},
		{
			name: "account-ID digits after a sample bucket prefix in an ARN",
			text: `"bucket": "arn:aws:s3:::example-backup-210987654321"`,
			want: []string{"arn:aws:s3:::example-backup-210987654321"},
		},
		{
			name: "account-ID digits after a sample bucket prefix in a Cloud Storage path",
			text: "the asset is storage.googleapis.com/example-data-210987654321",
			want: []string{"storage.googleapis.com/example-data-210987654321"},
		},

		// Not reported: the project's own account, a listed public open-source
		// owner, and a sample's bucket.
		{
			name: "public project host beside its path",
			text: "cloned from github.com/timothymamo/shift2eu",
			want: nil,
		},
		{
			name: "project host with the owner in another case",
			text: "cloned from GitHub.com/TimothyMamo/shift2eu",
			want: nil,
		},
		{
			name: "public open-source owner path",
			text: "the images are verified with github.com/sigstore/cosign",
			want: nil,
		},
		{
			// A sample's bucket is named the way its domain is: a documentation
			// value that identifies nobody.
			name: "public bucket in an s3 URL",
			text: "the sample objects are in s3://example-bucket/key",
			want: nil,
		},
		{
			name: "public bucket in a gs URL",
			text: "the sample objects are in gs://example-bucket/x",
			want: nil,
		},
		{
			// Generic words and optional digits follow example-, so the bucket
			// is a sample's own.
			name: "public bucket with digits behind the example- prefix",
			text: "the sample objects are in s3://example-logs-2/key",
			want: nil,
		},
		{
			name: "public bucket named example itself",
			text: "the sample objects are in s3://example/key",
			want: nil,
		},
		{
			name: "public bucket in a Cloud Storage URL",
			text: "the sample objects are at https://storage.googleapis.com/example-bucket/key",
			want: nil,
		},
		{
			// The JSON API's own path names a sample's bucket too, so it
			// identifies nobody.
			name: "public bucket in a Cloud Storage JSON API path",
			text: `"selfLink": "https://www.googleapis.com/storage/v1/b/example-bucket"`,
			want: nil,
		},
		{
			name: "public bucket in an ARN",
			text: `"bucket": "arn:aws:s3:::example-bucket"`,
			want: nil,
		},
		{
			// A name below a listed vendor host is the host name rule's value,
			// not a path of the vendor's: this rule reports nothing for it, and
			// the host it is below is the one that names the customer.
			name: "name below a listed vendor host",
			text: "the assets come from evil.github.com/acme/x",
			want: []string{"evil.github.com"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, values(sanitisationProblems(tc.text))); diff != "" {
				t.Errorf("sanitisationProblems(%q) mismatch (-want +got):\n%s", tc.text, diff)
			}
		})
	}
}
