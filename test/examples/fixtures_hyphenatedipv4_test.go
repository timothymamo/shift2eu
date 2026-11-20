package examples_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestHyphenatedIPv4Problems pins which hyphenated numbers the fixture check
// reads as an address: one written into a name - after an ip- that stands on its
// own, or as the first label of a dotted name, a documentation domain included -
// whose groups are octets and whose address is outside the sample range
// 192.0.2.0/24 is reported, while a sample address, a date, a version-like token,
// a sequence of numbers ending a sentence and a group that is no octet are not.
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
			name: "documentation address",
			text: "ip-192-0-2-10 is the sample's",
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
		{name: "sequence in prose", text: "the steps 1-2-3-4 in order", want: nil},
		{name: "sequence ending a sentence", text: "the steps are 1-2-3-4.", want: nil},
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
