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
// cloud, ai, co, eu and common country codes such as de, fr, nl, uk, ch, at, be,
// se, fi, no, dk, es, it, pl, us, plus info, biz, tech, online, site, xyz -
// deliberately not md, sh, py, rs or another file-extension clash), and it is
// not one of the exact public vendor hosts a page may link to (github.com,
// www.scaleway.com, ... ). The vendor list is exact, never a suffix match: the
// subdomain of a listed vendor host is a customer's name and is reported.
//
// The account-ID, GUID, IPv4 and IPv6 rules are unchanged, and the names the
// examples/ allow list already accepts (example.com and its subdomains, the
// reserved .example, .test and .invalid names) stay accepted.
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
			name: "public project host beside its path",
			text: "cloned from github.com/timothymamo/shift2eu",
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
