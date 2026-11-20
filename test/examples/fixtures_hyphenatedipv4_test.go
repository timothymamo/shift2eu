package examples_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestHyphenatedIPv4Problems pins which hyphenated numbers the fixture check
// reads as an address. A run is four groups of one to three digits joined by
// hyphens or underscores throughout, each group an octet read as a decimal
// number, so a zero-padded octet spells the address it looks like (ip-10-01-2-3
// is 10.1.2.3) and a group above 255 makes no address at all. The address must
// be outside the sample range 192.0.2.0/24, which every sample address is in:
//
//   - written into a name - after an ip- or ip_ that stands on its own, or
//     ending a label that another label follows after a dot, a documentation
//     domain included - any address outside that range is reported;
//   - otherwise - bare, after a label of its own, or ending a sentence - only a
//     private (10/8, 172.16/12, 192.168/16), shared (100.64/10) or link-local
//     (169.254/16) address is, because a public-looking run in prose
//     (5-10-20-30, 1-2-3-4) is a sequence of numbers as often as an address.
//
// A private address is reported in a CIDR suffix written into a name
// (vpc-10-0-0-0-16, subnet_10_0_0_0_24), and a fifth group after a
// public-looking run (build-1-2-3-4-5) leaves that run clean. A fifth group that
// ends the run is the prefix length (10-1-2-3-4 is 10.1.2.3/4), while one beyond
// the prefix range (10-1-2-3-45, vpc-10-0-0-0-33), one of three digits
// (vpc-10-0-0-0-016) or a further group after a bare run (10-1-2-3-4-5) leaves
// the run clean too; a name's prefix length that one more digit group follows
// (subnet-10-0-1-0-24-2, vpc_10_0_0_0_16_1) fails closed and the run is
// reported. The label before
// a bare private run excuses it only when the label is an English word ending in
// ip that stands on its own (zip-10-1-2-3, ship-1-2-3-4): the same word inside a
// compound name (edge-strip-10-0-0-5, node_tip_10_1_2_3) marks the address, as
// does any other word (node-, vm-, eip-, hostip-, nat-), a digit group
// (eu-central-1-10-1-2-3) excepted. A date, a version-like token, a sequence of
// numbers and a group that is no octet are not an address. A version-like token
// that a dotted extension follows (build-1-2-3-4.tar.gz) is the price of the
// dotted-label rule and is reported: the rule fails closed rather than let an
// address in a name through.
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
		{
			// A VPC and a subnet are named after the CIDR they carry, so the
			// four-group run is the address and the group after it the prefix
			// length: the fifth group is no reason to pass a private address.
			name: "private CIDR in a VPC name",
			text: "the vpc vpc-10-0-0-0-16 holds it",
			want: []string{"10-0-0-0"},
		},
		{
			name: "private CIDR in a subnet name",
			text: "the subnet subnet_10_0_0_0_24 holds it",
			want: []string{"10_0_0_0"},
		},
		{
			// The fifth group of a private run is the prefix length when nothing
			// follows it, so 10-1-2-3-4 is 10.1.2.3/4 and the address is
			// reported: the rule fails closed rather than pass a private address
			// for the sake of a trailing number.
			name: "private address followed by a prefix length",
			text: `"peer": "10-1-2-3-4"`,
			want: []string{"10-1-2-3"},
		},
		{
			// 45 is no prefix length of an IPv4 CIDR, so the run is the tail of
			// a longer sequence of numbers rather than an address of its own.
			name: "private address followed by a group beyond the prefix range",
			text: "the peer 10-1-2-3-45 answers",
			want: nil,
		},
		{
			name: "private address followed by two further groups",
			text: "the steps 10-1-2-3-4-5 are listed",
			want: nil,
		},
		{
			// A prefix length is 0 to 32, so 33 is no prefix and the run stays as
			// clean as any other fifth group beyond it.
			name: "prefix length beyond 32 in a VPC name",
			text: "the vpc vpc-10-0-0-0-33 holds it",
			want: nil,
		},
		{
			// 016 is three digits, and a prefix length is written with two at
			// most, so the group is no prefix.
			name: "prefix length of three digits in a VPC name",
			text: "the vpc vpc-10-0-0-0-016 holds it",
			want: nil,
		},
		{
			// The availability zone after the prefix length is no further digit
			// group, so the name spells a CIDR and the address is reported.
			name: "prefix length followed by an availability zone in a subnet name",
			text: "the subnet subnet-10-0-0-0-16-az1 holds it",
			want: []string{"10-0-0-0"},
		},
		{
			// A prefix length and one more digit group after it: the name may be a
			// subnet of a /24 and a node's index, or a private address written
			// behind a label, and the rule fails closed rather than pass one.
			name: "prefix length followed by a further digit group in a subnet name",
			text: "the subnet subnet-10-0-1-0-24-2 holds it",
			want: []string{"10-0-1-0"},
		},
		{
			name: "prefix length followed by a further digit group in an underscored VPC name",
			text: "the vpc vpc_10_0_0_0_16_1 holds it",
			want: []string{"10_0_0_0"},
		},
		{
			// The same fifth group after a public-looking run: no address the
			// rule reports, so the run stays as clean as 1-2-3-4 in prose.
			name: "public run followed by a fifth group",
			text: "the build build-1-2-3-4-5 is pinned",
			want: nil,
		},
		{
			// strip is an English word ending in ip, but here it is part of a
			// compound name rather than a word of its own, so the word no longer
			// marks prose and the address after it is reported.
			name: "private address after an English word inside a compound name",
			text: "the edge edge-strip-10-0-0-5 answers",
			want: []string{"10-0-0-5"},
		},
		{
			name: "private address after an English word inside an underscored compound name",
			text: "the node node_tip_10_1_2_3 answers",
			want: []string{"10_1_2_3"},
		},
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
