package examples_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestHyphenatedIPv4Problems pins which hyphenated numbers the fixture check
// reads as an address: one written into a name - after an ip- that stands on its
// own, or ending a label that another label follows after a dot, a documentation
// domain included - whose groups are octets and whose address is outside the
// sample range 192.0.2.0/24 is reported, while a sample address, a date, a
// version-like token, a sequence of numbers ending a sentence and a group that is
// no octet are not. A version-like token that a dotted extension follows
// (build-1-2-3-4.tar.gz) is the price of the dotted-label rule and is reported:
// the rule fails closed rather than let an address in a name through.
func TestHyphenatedIPv4Problems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "no address", text: "shop-web-1 in eu-central-1", want: nil},
		{name: "bare address after the ip prefix", text: "the instance ip-10-1-2-3 answers", want: []string{"10-1-2-3"}},
		{name: "address after the ip prefix in capitals", text: "the instance IP-10-1-2-3 answers", want: []string{"10-1-2-3"}},
		{
			name: "address in an AWS host name",
			text: `"hostname": "ip-10-1-2-3.eu-central-1.compute.internal"`,
			want: []string{"10-1-2-3"},
		},
		{
			name: "address in a named host without the ip prefix",
			text: "the node vm-172-16-4-9.acme-corp.internal serves it",
			want: []string{"172-16-4-9"},
		},
		{
			// ip- is no prefix when a letter or digit opens it - AWS names an
			// Elastic IP eip-10-1-2-3 and a host hostip-172-16-0-5 - so the
			// address after such a prefix is reported all the same.
			name: "private address after an Elastic IP prefix",
			text: "the instance eip-10-1-2-3 is attached",
			want: []string{"10-1-2-3"},
		},
		{
			name: "private address after a host ip prefix",
			text: "the interface hostip-172-16-0-5 answers",
			want: []string{"172-16-0-5"},
		},
		{
			// A source writes an address into a name behind a label of its own
			// (node-, vm-), which is no ip- prefix and still carries the
			// address, and the bare form in prose is the same address.
			name: "private address after a letter-hyphen prefix",
			text: "the node node-10-1-2-3 answers",
			want: []string{"10-1-2-3"},
		},
		{
			name: "private address after a vm prefix",
			text: "the instance vm-10-1-2-3 holds it",
			want: []string{"10-1-2-3"},
		},
		{
			name: "bare private address standing alone",
			text: `"peer": "10-1-2-3"`,
			want: []string{"10-1-2-3"},
		},
		{
			// No source writes an address into a name with underscores, but a
			// page that copies one may, so the run is read and reported as
			// written, without the ip_ before it.
			name: "address with underscore separators",
			text: "the peer ip_10_1_2_3 answers",
			want: []string{"10_1_2_3"},
		},
		{
			// A zero-padded octet is no octet to netip.ParseAddr, so the groups
			// are read as the address they spell (10.1.2.3) and the run is
			// reported as it is written.
			name: "address with a zero-padded octet",
			text: "the instance ip-10-01-2-3 answers",
			want: []string{"10-01-2-3"},
		},
		{
			// 100.64.0.0/10 is the shared address space (CGNAT) and
			// 169.254.0.0/16 the link-local one: neither is the documentation
			// range, and both identify a customer's own network.
			name: "shared address after a letter-hyphen prefix",
			text: "the gateway nat-100-64-1-2 serves it",
			want: []string{"100-64-1-2"},
		},
		{
			name: "link-local address after a letter-hyphen prefix",
			text: "the host vm-169-254-1-2 answers",
			want: []string{"169-254-1-2"},
		},
		{
			name: "documentation address",
			text: "ip-192-0-2-10 is the sample's",
			want: nil,
		},
		{
			// The documentation range holds in every shape the rule reads: the
			// address is no customer's value behind any of the prefixes, bare,
			// behind a zero-padded octet, with underscores or at the end of a
			// sentence.
			name: "documentation address after a letter-hyphen prefix",
			text: "the node node-192-0-2-10 answers",
			want: nil,
		},
		{
			name: "documentation address after a vm prefix",
			text: "the instance vm-192-0-2-10 holds it",
			want: nil,
		},
		{
			name: "documentation address after an Elastic IP prefix",
			text: "the instance eip-192-0-2-10 is attached",
			want: nil,
		},
		{
			name: "documentation address standing alone",
			text: `"peer": "192-0-2-10"`,
			want: nil,
		},
		{
			name: "documentation address with a zero-padded octet",
			text: "the instance ip-192-0-02-10 answers",
			want: nil,
		},
		{
			name: "documentation address with underscore separators",
			text: "the peer ip_192_0_2_10 answers",
			want: nil,
		},
		{
			name: "documentation address ending a sentence",
			text: "the request was answered from 192-0-2-10.",
			want: nil,
		},
		{
			name: "second RFC 5737 documentation range",
			text: "ip-198-51-100-7 is outside the sample's range",
			want: []string{"198-51-100-7"},
		},
		{
			name: "third RFC 5737 documentation range",
			text: "ip-203-0-113-9 is outside it too",
			want: []string{"203-0-113-9"},
		},
		{
			name: "documentation domain name holding a private address",
			text: `"endpoint": "10-1-2-3.example.com"`,
			want: []string{"10-1-2-3"},
		},
		{
			name: "address holding an ip prefix inside a longer word",
			text: "the ship-1-2-3-4 sails",
			want: nil,
		},
		{
			name: "address holding an ip prefix inside a longer word without a dot after it",
			text: "zip-10-1-2-3",
			want: nil,
		},
		{name: "date", text: "released on 2026-11-20", want: nil},
		{name: "date in the short form", text: "the window 26-11-20 stays", want: nil},
		{name: "version-like token", text: "the build-1-2-3-4 image is pinned", want: nil},
		{
			name: "version-like token with a dotted extension",
			text: "the build-1-2-3-4.tar.gz image is pinned",
			want: []string{"1-2-3-4"},
		},
		{name: "sequence in prose", text: "the steps 1-2-3-4 in order", want: nil},
		{name: "sequence ending a sentence", text: "the steps are 1-2-3-4.", want: nil},
		{
			// A routable sequence in prose is no address a reader acts on: it
			// stays clean, the way the numbered sequence above does.
			name: "public sequence in prose",
			text: "the steps 5-10-20-30 are listed",
			want: nil,
		},
		{
			// A customer's private address ending a sentence is still a private
			// address: the full stop opens no label, and the run stands on its
			// own.
			name: "private address ending a sentence",
			text: "the request was answered from 10-1-2-3.",
			want: []string{"10-1-2-3"},
		},
		{name: "group that is no octet", text: "ip-10-1-2-300 is no address", want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, values(hyphenatedIPv4Problems(tc.text))); diff != "" {
				t.Errorf("hyphenatedIPv4Problems(%q) mismatch (-want +got):\n%s", tc.text, diff)
			}
		})
	}
}
