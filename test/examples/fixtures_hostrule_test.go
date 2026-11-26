package examples_test

import (
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// hostRuleCase is one case of TestSanitisationProblemsHostNameRule: the text the
// fixture check reads and the values it reports in it.
type hostRuleCase struct {
	name string
	text string
	want []string
}

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

	tests := []hostRuleCase{
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
			// A company's internal zone is written under the private and the
			// home suffix as often as under .corp or .lan, and the rule fails
			// closed on both (see the note on the private suffixes in
			// publicTLDs): a name under either identifies the customer's own
			// network whether or not the text marks it as a host.
			name: "private name under the private suffix in a corporate zone",
			text: "the database is db01.corp.private",
			want: []string{"db01.corp.private"},
		},
		{
			name: "home network name under the home suffix",
			text: "the NAS is nas.acme.home",
			want: []string{"nas.acme.home"},
		},
		{
			// The home, private, priv, jobs and travel suffixes match in any
			// case on purpose, so a dotted value that is no host at all is
			// reported as well: a workflow's `workflow.jobs` field ends in a
			// TLD. That is an accepted false positive - the rule fails closed,
			// a customer's own host under these suffixes is real data, and a
			// value that is no host takes one allow-list entry.
			name: "accepted false positive: dotted field path under the jobs TLD",
			text: "the field `workflow.jobs` holds the jobs",
			want: []string{"workflow.jobs"},
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
			// name, and two of the country codes it left out (in, br), while us
			// was already listed and stays there.
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
			// A resource type names a kind of resource, never a host: the
			// provider namespace of Container Apps with "/" and its type
			// directly after it is Azure's own vocabulary (PRD #552, issue
			// #610), whatever case it is written in. The rule is the AI
			// reviewer's scan's (ci/aireview/internal/review/scan.go,
			// isAzureNamespace), so the fixture check needs no allow list
			// entry for it.
			name: "Azure Container Apps resource types",
			text: `{"type": "Microsoft.App/containerApps", "managed": "microsoft.app/managedenvironments"}`,
			want: nil,
		},
		{
			// A customer's own host under the .app TLD is still reported: the
			// rule excuses microsoft.<namespace>/ alone.
			name: "planted customer host under the app TLD",
			text: "the storefront answers at shop-corp.app",
			want: []string{"shop-corp.app"},
		},
		{
			// The rule needs "/" and a resource type right after the
			// namespace: a bare microsoft.app is the public host of the
			// provider namespace's owner, and stays reported.
			name: "Azure namespace with no resource type after it",
			text: "the type is written microsoft.app in prose",
			want: []string{"microsoft.app"},
		},
		{
			// So does a deeper name: only a name that is exactly
			// microsoft.<namespace> is no host, never a name below one.
			name: "name below the Azure namespace",
			text: "the type shop.microsoft.app/containerApps is no namespace",
			want: []string{"shop.microsoft.app"},
		},
		{
			// And so does a name that only ends in the namespace's shape.
			name: "name that only ends in the Azure namespace",
			text: "the host xmicrosoft.app/x is no namespace",
			want: []string{"xmicrosoft.app"},
		},
		{
			// A byte that is no part of a namespace's run right before the run
			// means the run is the tail of a name that byte belongs to (issue
			// #610): é is two UTF-8 bytes the run never holds, so
			// shopémicrosoft.app is a customer's own internationalised host and
			// the microsoft.app after é is its last labels, not a resource type.
			name: "Unicode host whose tail reads as an Azure namespace",
			text: "the orders are at https://shopémicrosoft.app/orders",
			want: []string{"microsoft.app"},
		},
		{
			// The "//" right before the run is a URL's authority marker, the
			// one a scheme and a protocol-relative URL write alike, so the name
			// after it is the URL's host and is reported (issue #610).
			name: "customer host right after a scheme's authority marker",
			text: "the payroll rules are at https://microsoft.internal/share/payroll",
			want: []string{"microsoft.internal"},
		},
		{
			name: "customer host right after a protocol-relative URL's authority marker",
			text: "the policy is at //Microsoft.corp/wiki",
			want: []string{"Microsoft.corp"},
		},
		{
			// The "@" right before the run ends a URL's userinfo, so the name
			// after it is the URL's host and is reported: userinfo is no
			// delimiter that introduces a resource type (issue #610).
			name: "customer host right after a URL's userinfo",
			text: "the payroll rules are at https://svc-user@microsoft.corp/share/payroll",
			want: []string{"microsoft.corp"},
		},
		{
			// An authority is written with a backslash as well as with a slash
			// (a request line, a path a Windows tool takes): the name after the
			// two backslashes is the host, not a resource type (issue #610).
			name: "customer host right after a backslash authority marker",
			text: `the wiki is at https:\\microsoft.corp/wiki`,
			want: []string{"microsoft.corp"},
		},
		{
			name: "customer host right after a bare backslash authority marker",
			text: `the share is at \\Microsoft.corp/share`,
			want: []string{"Microsoft.corp"},
		},
		{
			// A scheme the text writes with a single "/" still marks the name
			// after it as a URL's host: the slash follows the scheme's colon, so
			// it is no resource id's segment separator (issue #610).
			name: "customer host right after a single-slash scheme",
			text: "the payroll is at http:/microsoft.corp/x",
			want: []string{"microsoft.corp"},
		},
		{
			// A single "/" before the namespace is the separator of an ARM
			// resource id's segments, not a URL's authority marker, and the
			// namespace there still starts a resource type: nothing is
			// reported (issue #610).
			name: "Azure resource type inside an ARM resource id",
			text: "/subscriptions/" + dummyGUID + "/resourceGroups/rg-shop/providers/Microsoft.App/containerApps/app-shop",
			want: nil,
		},
		{
			// A resource type directly after its namespace in an inventory's
			// JSON is the form PRD #552 added the rule for, and the narrowing
			// leaves it quiet (issue #610).
			name: "Azure managed-environment resource type",
			text: `"type": "microsoft.app/managedenvironments"`,
			want: nil,
		},
		// Regression guards for the allow list of what may come right before
		// the run (issue #610): every delimiter that introduces a resource type
		// leaves the namespace quiet, in a guide page, a fixture and an
		// inventory's JSON alike.
		{
			// Start of the text: no delimiter at all before the namespace.
			name: "Azure resource type at the start of the text",
			text: "Microsoft.App/containerApps is the type",
			want: nil,
		},
		{
			// A JSON value's quote.
			name: "Azure resource type after a JSON value's quote",
			text: `"type": "Microsoft.App/containerApps"`,
			want: nil,
		},
		{
			// A Markdown code span's backtick.
			name: "Azure resource type in a Markdown code span",
			text: "the type is `Microsoft.App/containerApps` in the guide",
			want: nil,
		},
		{
			// A Markdown table cell's delimiters.
			name: "Azure resource type in a Markdown table cell",
			text: "| Microsoft.App/containerApps |",
			want: nil,
		},
		{
			// A settings assignment's "=".
			name: "Azure resource type after an assignment",
			text: "type=microsoft.app/managedenvironments",
			want: nil,
		},
		{
			// A single "/" after a path segment separates an ARM resource
			// id's segments, so the namespace after it still starts a resource
			// type and is no host name (issue #610).
			name: "Azure resource type after an ARM id's segment separator",
			text: "/providers/Microsoft.App/containerApps/app-shop",
			want: nil,
		},
		{
			// The namespace rule reads only a run that neither starts nor ends
			// with "." or "-": a run that does is no provider namespace, so the
			// name inside it is a host and stays reported (issue #610).
			name: "name that only reads as an Azure namespace after a trim",
			text: "the host -microsoft.app/x and the dotted Microsoft.App./x are no namespaces",
			want: []string{"microsoft.app", "Microsoft.App"},
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
		{
			// The fixture check reads a whole text rather than a line, so a
			// namespace that starts a line follows a newline, the one delimiter
			// this copy of the rule accepts besides the shared list's (issue
			// #630). The reviewer's scan needs no such case: it reads a diff one
			// line at a time, and the start of a line is the start of its text.
			name: "Azure resource type at the start of a line",
			text: "the types are:\nMicrosoft.App/containerApps",
			want: nil,
		},
	}

	// Issue #630: one case per delimiter the rule accepts, from the list both
	// copies of it read, so a copy that drops a delimiter fails here as well as
	// in the AI reviewer's scan test.
	tests = append(tests, namespaceMayFollowDelimiterCases(t)...)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, values(sanitisationProblems(tc.text))); diff != "" {
				t.Errorf("sanitisationProblems(%q) mismatch (-want +got):\n%s", tc.text, diff)
			}
		})
	}
}

// namespaceMayFollowDelimitersPath is the shared case list of the two copies of
// namespaceMayFollow (issue #630): this package's copy, and the AI reviewer's in
// ci/aireview/internal/review/scan.go, whose test,
// ci/aireview/internal/review/scan_test.go, reads the same file through its own
// parser. From this package's directory.
const namespaceMayFollowDelimitersPath = "testdata/namespace_may_follow_delimiters.txt"

// namespaceMarker is the resource type every record of the shared list holds:
// the provider namespace sits directly after the delimiter the record's case
// names, so a case reads the text as a checked file does.
const namespaceMarker = "Microsoft.App/containerApps"

// namespaceMayFollowDelimiter is one record of the shared list: the name of the
// case and the text it runs.
type namespaceMayFollowDelimiter struct {
	name string
	text string
}

// namespaceMayFollowDelimiters returns the records of the shared list, in its
// order. It fails the test when the file cannot be read, when a line is no
// record, and when the list names no delimiter, so no case passes by checking
// nothing.
func namespaceMayFollowDelimiters(t *testing.T) []namespaceMayFollowDelimiter {
	t.Helper()

	data, err := os.ReadFile(namespaceMayFollowDelimitersPath)
	if err != nil {
		t.Fatalf("read the shared namespace delimiter cases: %v", err)
	}

	var records []namespaceMayFollowDelimiter

	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		name, text, ok := strings.Cut(line, ": ")
		if !ok {
			t.Fatalf("%s: %q is no record of a case name, \": \" and a text", namespaceMayFollowDelimitersPath, line)
		}

		records = append(records, namespaceMayFollowDelimiter{name: name, text: text})
	}

	if len(records) == 0 {
		t.Fatalf("%s: the shared list names no delimiter, so no case guards the rule", namespaceMayFollowDelimitersPath)
	}

	return records
}

// namespaceMayFollowAcceptedBytes returns the bytes the rule accepts right
// before a namespace, from the shared list: the byte each record's text puts
// directly before the namespace, and the newline, which a line of the list
// cannot carry, since the fixture check reads a whole text. The start-of-text
// record names no byte, the empty text TestNamespaceMayFollow's table takes.
func namespaceMayFollowAcceptedBytes(t *testing.T) map[byte]bool {
	t.Helper()

	accepted := map[byte]bool{'\n': true}

	for _, record := range namespaceMayFollowDelimiters(t) {
		before, _, ok := strings.Cut(record.text, namespaceMarker)
		if !ok {
			t.Fatalf("%s: the record %q names no %s, so no case reads its delimiter", namespaceMayFollowDelimitersPath, record.text, namespaceMarker)
		}

		if before != "" {
			accepted[before[len(before)-1]] = true
		}
	}

	return accepted
}

// namespaceMayFollowDelimiterCases returns one case per delimiter the shared
// list names, in its order: the text of the record, which the fixture check must
// report nothing in, and no reported value, since a copy that stops accepting a
// delimiter reads the namespace of the resource type as a host name and fails
// the case. A record's name carries its delimiter, so the failing case says
// which one the copy lost.
func namespaceMayFollowDelimiterCases(t *testing.T) []hostRuleCase {
	t.Helper()

	records := namespaceMayFollowDelimiters(t)

	cases := make([]hostRuleCase, 0, len(records))
	for _, record := range records {
		cases = append(cases, hostRuleCase{name: "Azure resource type after " + record.name, text: record.text})
	}

	return cases
}

// namespaceMayFollowCase is one case of TestNamespaceMayFollow: the text before
// an Azure provider namespace and whether the rule accepts it.
type namespaceMayFollowCase struct {
	name   string
	before string
	want   bool
}

// TestNamespaceMayFollow pins this package's copy of the delimiter rule itself,
// byte by byte, not only through a checked text (issue #630). The behavioural
// cases of TestSanitisationProblemsHostNameRule cover every delimiter but one:
// a name right after a single "/" is no host name to hostnameProblems whatever
// the rule says (insidePath), so the fixture check cannot see whether the copy
// still accepts the resource id's separator. This test can.
//
// Its accepted entries come from the shared list both copies' tests read, so the
// file is the one list of the bytes the rule takes, and the loop after the table
// pins that set exactly: every one of the 256 single-byte values before a
// namespace, after another byte and as the first of the text, is accepted
// exactly when the list names it. A copy that drops a delimiter the list names
// therefore fails a case of the table, and a copy that widens the rule to a byte
// the list does not name fails the loop, so neither can happen unseen. The copy
// has the reviewer's set plus the newline, the one byte a line of that list
// cannot carry, since the fixture check reads a whole text.
func TestNamespaceMayFollow(t *testing.T) {
	t.Parallel()

	tests := []namespaceMayFollowCase{
		{name: "a newline", before: "the types are:\n", want: true},
		// Everything else keeps the run a host name: a URL's authority marker,
		// the mistyped one a request line writes, a scheme written with a single
		// slash, userinfo, the tail of an internationalised host and a letter the
		// run continues.
		{name: "a URL's authority marker", before: "https://", want: false},
		{name: "a protocol-relative URL's authority marker", before: "//", want: false},
		{name: "a single-slash scheme", before: "http:/", want: false},
		{name: "a backslash authority marker", before: `https:\\`, want: false},
		{name: "a single backslash", before: `\`, want: false},
		{name: "a URL's userinfo", before: "https://svc-user@", want: false},
		{name: "the tail of an internationalised host", before: "https://shopém", want: false},
		{name: "a letter the run continues", before: "x", want: false},
	}

	for _, record := range namespaceMayFollowDelimiters(t) {
		before, _, ok := strings.Cut(record.text, namespaceMarker)
		if !ok {
			t.Fatalf("%s: the record %q names no %s, so no case reads its delimiter", namespaceMayFollowDelimitersPath, record.text, namespaceMarker)
		}

		tests = append(tests, namespaceMayFollowCase{name: record.name, before: before, want: true})
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := namespaceMayFollow(tc.before); got != tc.want {
				t.Errorf("namespaceMayFollow(%q) = %t, want %t", tc.before, got, tc.want)
			}
		})
	}

	// The table names the delimiters the list names; the loop makes that set the
	// whole of what the copy accepts. Each byte runs twice: after another byte,
	// where the copy's "/" branch sees a preceding byte that is no "/", "\" or
	// ":", and as the first byte of the text, where it sees none.
	accepted := namespaceMayFollowAcceptedBytes(t)

	for value := range 256 {
		b := byte(value)

		for _, before := range []string{"x" + string(b), string(b)} {
			if got := namespaceMayFollow(before); got != accepted[b] {
				t.Errorf("namespaceMayFollow(%q) = %t, want %t, from the shared list's bytes a namespace may follow (byte %#02x)",
					before, got, accepted[b], b)
			}
		}
	}
}

// TestVendorPathProblems pins which owner and which bucket the fixture check
// reads after a vendor host or a bucket scheme. The host name rule exempts the
// public vendor hosts a page links to (publicVendorHosts), yet the path after
// one names the customer just as a host would, so an owner a path names after
// github.com, api.github.com, raw.githubusercontent.com or ghcr.io is reported
// as the host and the owner (github.com/acme), in a URL and in the SSH clone
// form a reader copies (git@github.com:acme/private-repo.git) alike, with the
// GitHub API's endpoint segment (repos, users, orgs, enterprises) left out.
// Only this project's own account and the listed public open-source owners
// pass, so github.com/timothymamo/shift2eu and github.com/sigstore/cosign are
// clean and the same host written in another case
// (GitHub.com/TimothyMamo/shift2eu) is clean too.
//
// A bucket is reported with the scheme that gives it its shape (s3://, s3a://,
// s3n://, gs://, az://, oci://) or, in an ARN, whole
// (arn:aws:s3:::acme-prod-bucket). An abfss or wasbs URL names a storage
// account, which is reported on its own (acmeprodlake), beside the host the
// host name rule reports. An oci:// reference names a registry host
// with a dot in its first segment (ghcr.io, registry.example.com) rather than a
// bucket, so it is read only when that segment is a bucket's name, as it is in
// oci://acme-prod-bucket@acmetenancy. A Cloud Storage path is read by this
// rule, not the owner one, and it names the bucket after the service host
// (storage.googleapis.com), in a URL (storage.googleapis.com/acme-prod-data),
// in a Cloud Asset name (//storage.googleapis.com/acme-prod-data) and in a JSON
// API path, whose www.googleapis.com host and storage/v1/b segment an upload, a
// download or a plain call writes alike: the value reported is the host and the
// path after it. A bucket is a sample value only when it is example or starts
// with example- and generic words follow (example-bucket, example-logs-2,
// example), the digits after the last word one to three of them, never when a
// name follows the prefix (example-acme-prod) or a long run of digits does
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
			// A bucket's own letters are lower case in Cloud Storage, yet a
			// bucket written with a capital is the customer's all the same, and
			// the JSON API path's b/ segment is what makes it a bucket path. The
			// value reported is the host and the path up to the bucket as the
			// file writes them, so the failure names the value to change.
			name: "customer bucket in capitals in a Cloud Storage JSON API path",
			text: `"selfLink": "https://www.googleapis.com/storage/v1/b/Acme-Prod/o"`,
			want: []string{"www.googleapis.com/storage/v1/b/Acme-Prod"},
		},
		{
			// The same JSON API path called on the service host itself, which the
			// asset name uses: the bucket is read with its capitals here too, so
			// the rule no longer reports the storage/ segment it read instead.
			name: "customer bucket in capitals after the Cloud Storage service host",
			text: "the asset is storage.googleapis.com/storage/v1/b/Acme-Prod",
			want: []string{"storage.googleapis.com/storage/v1/b/Acme-Prod"},
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
		{
			// api.github.com is a listed vendor host, and the path after it names
			// the customer's account behind an endpoint segment: the value
			// reported is the host and the owner, the way the github.com rows
			// report it, with the endpoint (repos, orgs) left out.
			name: "customer owner path after the GitHub API host",
			text: "the file is at https://api.github.com/repos/acme-corp/infra/contents/x",
			want: []string{"api.github.com/acme-corp"},
		},
		{
			// The API has more endpoints than the repository one, and the
			// enterprise endpoint names the customer's own account just as the
			// repository endpoint does: reported with the endpoint left out, the
			// way the repos, users and orgs rows are.
			name: "customer owner path after the GitHub API's enterprise endpoint",
			text: "the account is at https://api.github.com/enterprises/acme-corp",
			want: []string{"api.github.com/acme-corp"},
		},
		{
			// A user account is an owner like an organisation, and the users
			// endpoint names it the same way: the endpoint is left out of the
			// value, which is the host and the account.
			name: "customer owner path after the GitHub API's user endpoint",
			text: "the user is at https://api.github.com/users/acme-corp",
			want: []string{"api.github.com/acme-corp"},
		},
		{
			// An organisation is the account a customer's repositories sit
			// under, and the orgs endpoint names it after the same host.
			name: "customer owner path after the GitHub API's organisation endpoint",
			text: "the org is at https://api.github.com/orgs/acme-corp",
			want: []string{"api.github.com/acme-corp"},
		},
		{
			// The API host only holds on its own, as the other vendor hosts do:
			// a name below it (evil.api.github.com) is the host name rule's
			// value, which reads the whole dotted name, and this rule reports
			// nothing for the owner path after it.
			name: "name below the GitHub API host",
			text: "the file is at https://evil.api.github.com/repos/acme-corp/x",
			want: []string{"evil.api.github.com"},
		},
		{
			// abfss names the container before an @ and the customer's storage
			// account in the host after it. The host is the host name rule's
			// value and the account is this rule's, so both are reported: the
			// account is globally unique and the customer's own name.
			name: "customer storage account in an abfss URL",
			text: "the data lake is at abfss://data@acmeprodlake.dfs.core.windows.net/x",
			want: []string{"acmeprodlake.dfs.core.windows.net", "acmeprodlake"},
		},
		{
			name: "customer storage account in a wasbs URL",
			text: "the blobs are at wasbs://data@acmeprodblob.blob.core.windows.net/x",
			want: []string{"acmeprodblob.blob.core.windows.net", "acmeprodblob"},
		},
		{
			// An oci URL names the customer's bucket before the tenancy, and the
			// bucket is reported with the scheme that gives it its shape, the way
			// the s3 and az rows are.
			name: "customer bucket in an oci URL",
			text: "the objects are in oci://acme-prod-bucket@acmetenancy/x",
			want: []string{"oci://acme-prod-bucket"},
		},
		{
			// An OCI Object Storage bucket's name may hold dots, and the @
			// that carries the namespace after it marks a bucket rather than a
			// registry host, whose path carries no @ at all: the dotted name
			// before it is the customer's bucket and is reported with the
			// scheme that gives it its shape.
			name: "customer bucket with dots in an oci URL",
			text: "the objects are in oci://acme.prod.backups@acmetenancy/x",
			want: []string{"oci://acme.prod.backups"},
		},
		{
			name: "customer bucket with a dotted word in an oci URL",
			text: "the objects are in oci://acme-prod.backup@ns/obj",
			want: []string{"oci://acme-prod.backup"},
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
			name: "sample bucket in an s3 URL",
			text: "the sample objects are in s3://example-bucket/key",
			want: nil,
		},
		{
			name: "sample bucket in a gs URL",
			text: "the sample objects are in gs://example-bucket/x",
			want: nil,
		},
		{
			// Generic words and optional digits follow example-, so the bucket
			// is a sample's own.
			name: "sample bucket with digits behind the example- prefix",
			text: "the sample objects are in s3://example-logs-2/key",
			want: nil,
		},
		{
			name: "sample bucket named example itself",
			text: "the sample objects are in s3://example/key",
			want: nil,
		},
		{
			name: "sample bucket in a Cloud Storage URL",
			text: "the sample objects are at https://storage.googleapis.com/example-bucket/key",
			want: nil,
		},
		{
			// The JSON API's own path names a sample's bucket too, so it
			// identifies nobody.
			name: "sample bucket in a Cloud Storage JSON API path",
			text: `"selfLink": "https://www.googleapis.com/storage/v1/b/example-bucket"`,
			want: nil,
		},
		{
			name: "sample bucket in an ARN",
			text: `"bucket": "arn:aws:s3:::example-bucket"`,
			want: nil,
		},
		{
			// The GitHub API host names the public project's repository here,
			// and the owner is one of the listed public open-source ones.
			name: "public open-source owner path after the GitHub API host",
			text: "the module is https://api.github.com/repos/aws/aws-sdk-go-v2",
			want: nil,
		},
		{
			// A sample's storage account starts with example, the way a sample's
			// bucket is named, so this rule reports nothing for it. The host the
			// account sits in is no documentation name, so the host name rule
			// reports it: the row pins that no account value joins it in the
			// report.
			name: "sample storage account in an abfss URL",
			text: "the data lake is at abfss://data@examplestorage.dfs.core.windows.net/x",
			want: []string{"examplestorage.dfs.core.windows.net"},
		},
		{
			name: "sample bucket in an oci URL",
			text: "the sample objects are in oci://example-bucket@example/x",
			want: nil,
		},
		{
			// An OCI artifact reference names a registry host rather than a
			// bucket: the segment after oci:// holds a dot (ghcr.io) and no @
			// namespace follows it. The owner path after the host is a listed
			// public open-source one.
			name: "public open-source OCI registry reference",
			text: "helm install x oci://ghcr.io/sigstore/charts/x",
			want: nil,
		},
		{
			// The same shape on a customer's own registry: the dotted segment is
			// a host, not a bucket, so this rule reports nothing. The host is a
			// documentation name, so the host name rule reports nothing either.
			name: "OCI registry reference on a dotted host rather than a bucket",
			text: "helm install x oci://registry.example.com/acme/chart",
			want: nil,
		},
		{
			// An accepted gap, and one vendorPathProblems names in its gap
			// list: a dotted first segment and no @namespace after it cannot be
			// told from a registry host (oci://registry.example.com/acme/chart),
			// so a dotted bucket written that way passes.
			name: "known gap: dotted oci bucket without a namespace",
			text: "the objects are in oci://acme.prod.backups/x",
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
