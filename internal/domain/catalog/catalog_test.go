package catalog_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// The target is the human-readable display name a report shows, not a slug:
// Target() returns exactly what was passed, with no case mapping.
const targetName = "Scaleway"

// validSnapshot is the dated, sourced snapshot every real catalog must carry,
// with the default region estimates use when the user names none (ADR 0024).
func validSnapshot() catalog.Snapshot {
	return catalog.Snapshot{
		SourceURL:     "https://www.scaleway.com/en/pricing/",
		RetrievedOn:   time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		DefaultRegion: "fr-par",
	}
}

// validOfferings is deliberately not in name order, and its inner slices are
// deliberately unsorted and duplicated: New must sort and de-duplicate them.
// Elastic Metal, Instances, Kapsule, Managed Database for PostgreSQL, Managed
// Database for PostgreSQL and MySQL, Object Storage is the expected order.
func validOfferings() []catalog.Offering {
	return []catalog.Offering{
		{
			Name:         "Object Storage",
			Categories:   []taxonomy.Category{taxonomy.StorageObject, taxonomy.StorageObject},
			Capabilities: []taxonomy.Capability{taxonomy.IPv6, taxonomy.ObjectVersioning, taxonomy.IPv6},
			Regions:      []string{"pl-waw", "fr-par", "fr-par"},
		},
		{
			Name:         "Instances",
			Categories:   []taxonomy.Category{taxonomy.ComputeVM},
			Capabilities: []taxonomy.Capability{taxonomy.Autoscaling, taxonomy.IPv6, taxonomy.Autoscaling},
			Regions:      []string{"nl-ams", "fr-par", "nl-ams", "pl-waw"},
		},
		{
			// Elastic Metal is a dedicated physical server: compute.baremetal
			// only, never compute.vm, and no block storage (issue #187, ADR 0023).
			Name:         "Elastic Metal",
			Categories:   []taxonomy.Category{taxonomy.ComputeBareMetal},
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			Regions:      []string{"fr-par", "nl-ams"},
		},
		{
			Name:         "Kapsule",
			Categories:   []taxonomy.Category{taxonomy.KubernetesManaged},
			Capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability, taxonomy.Autoscaling},
			Regions:      []string{"pl-waw", "nl-ams", "fr-par"},
		},
		{
			Name:         "Managed Database for PostgreSQL",
			Categories:   []taxonomy.Category{taxonomy.DatabasePostgreSQL},
			Capabilities: []taxonomy.Capability{taxonomy.ReadReplicas, taxonomy.PointInTimeRestore},
			Regions:      []string{"fr-par", "nl-ams", "pl-waw"},
		},
		{
			// One offering may serve several categories.
			Name:         "Managed Database for PostgreSQL and MySQL",
			Categories:   []taxonomy.Category{taxonomy.DatabaseMySQL, taxonomy.DatabasePostgreSQL},
			Capabilities: []taxonomy.Capability{taxonomy.PointInTimeRestore, taxonomy.ReadReplicas, taxonomy.ReadReplicas},
			Regions:      []string{"nl-ams", "fr-par", "pl-waw", "fr-par"},
		},
	}
}

// sortedUnique returns a copy of in sorted ascending and de-duplicated.
func sortedUnique[T ~string](in []T) []T {
	out := slices.Clone(in)
	slices.Sort(out)

	return slices.Compact(out)
}

// offeringsSortedByName is what the catalog must hold: name order, and every
// inner slice sorted ascending and de-duplicated.
func offeringsSortedByName() []catalog.Offering {
	offerings := validOfferings()
	for i, o := range offerings {
		o.Categories = sortedUnique(o.Categories)
		o.Capabilities = sortedUnique(o.Capabilities)
		o.Regions = sortedUnique(o.Regions)
		offerings[i] = o
	}

	slices.SortFunc(offerings, func(a, b catalog.Offering) int {
		return strings.Compare(a.Name, b.Name)
	})

	return offerings
}

func newCatalog(t *testing.T, target string, snapshot catalog.Snapshot, offerings []catalog.Offering) catalog.Catalog {
	t.Helper()

	cat, err := catalog.New(target, snapshot, offerings)
	if err != nil {
		t.Fatalf("New(%q, %d offerings) returned unexpected error: %v", target, len(offerings), err)
	}

	return cat
}

func offeringNames(offerings []catalog.Offering) []string {
	if len(offerings) == 0 {
		return nil
	}

	names := make([]string, 0, len(offerings))
	for _, o := range offerings {
		names = append(names, o.Name)
	}

	return names
}

func TestNewAndAccessors(t *testing.T) {
	t.Parallel()

	cat := newCatalog(t, targetName, validSnapshot(), validOfferings())

	// No case mapping: a slug such as "scaleway" would come back unchanged.
	if got := cat.Target(); got != targetName {
		t.Errorf("Target() = %q, want %q", got, targetName)
	}

	if diff := cmp.Diff(validSnapshot(), cat.Snapshot()); diff != "" {
		t.Errorf("Snapshot() mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(offeringsSortedByName(), cat.Offerings()); diff != "" {
		t.Errorf("Offerings() are not sorted by name with sorted inner slices (-want +got):\n%s", diff)
	}
}

func TestOfferingsReturnsCopy(t *testing.T) {
	t.Parallel()

	cat := newCatalog(t, targetName, validSnapshot(), validOfferings())

	got := cat.Offerings()
	if len(got) == 0 {
		t.Fatal("Offerings() is empty")
	}

	got[0].Name = "Tampered"
	got[0].Categories[0] = taxonomy.Category("tampered")
	got[0].Capabilities[0] = taxonomy.Capability("tampered")
	got[0].Regions[0] = "tampered"

	if diff := cmp.Diff(offeringsSortedByName(), cat.Offerings()); diff != "" {
		t.Errorf("Offerings() changed after mutating a previous result (-want +got):\n%s", diff)
	}
}

func TestOfferingsForReturnsCopy(t *testing.T) {
	t.Parallel()

	cat := newCatalog(t, targetName, validSnapshot(), validOfferings())

	got := cat.OfferingsFor(taxonomy.DatabasePostgreSQL)
	if len(got) == 0 {
		t.Fatal("OfferingsFor(database.postgresql) is empty")
	}

	got[0].Name = "Tampered"
	got[0].Categories[0] = taxonomy.Category("tampered")
	got[0].Capabilities[0] = taxonomy.Capability("tampered")
	got[0].Regions[0] = "tampered"

	want := []string{"Managed Database for PostgreSQL", "Managed Database for PostgreSQL and MySQL"}
	if diff := cmp.Diff(want, offeringNames(cat.OfferingsFor(taxonomy.DatabasePostgreSQL))); diff != "" {
		t.Errorf("OfferingsFor() changed after mutating a previous result (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(offeringsSortedByName(), cat.Offerings()); diff != "" {
		t.Errorf("Offerings() changed after mutating an OfferingsFor() result (-want +got):\n%s", diff)
	}
}

// New takes ownership: mutating the slice handed to it, or the slices inside it,
// must not reach the catalog.
func TestNewDoesNotAliasInput(t *testing.T) {
	t.Parallel()

	offerings := validOfferings()
	cat := newCatalog(t, targetName, validSnapshot(), offerings)

	offerings[0].Name = "Tampered"
	offerings[0].Categories[0] = taxonomy.Category("tampered")
	offerings[0].Capabilities[0] = taxonomy.Capability("tampered")
	offerings[0].Regions[0] = "tampered"

	if diff := cmp.Diff(offeringsSortedByName(), cat.Offerings()); diff != "" {
		t.Errorf("Offerings() changed after mutating the input slice (-want +got):\n%s", diff)
	}
}

func TestOfferingsFor(t *testing.T) {
	t.Parallel()

	cat := newCatalog(t, targetName, validSnapshot(), validOfferings())

	tests := []struct {
		name string
		in   taxonomy.Category
		want []string
	}{
		{name: "compute", in: taxonomy.ComputeVM, want: []string{"Instances"}},
		{name: "bare metal", in: taxonomy.ComputeBareMetal, want: []string{"Elastic Metal"}},
		{
			// One offering serving several categories: PostgreSQL and MySQL.
			name: "postgresql served by two offerings",
			in:   taxonomy.DatabasePostgreSQL,
			want: []string{"Managed Database for PostgreSQL", "Managed Database for PostgreSQL and MySQL"},
		},
		{name: "mysql", in: taxonomy.DatabaseMySQL, want: []string{"Managed Database for PostgreSQL and MySQL"}},
		{name: "kubernetes", in: taxonomy.KubernetesManaged, want: []string{"Kapsule"}},
		{name: "object storage", in: taxonomy.StorageObject, want: []string{"Object Storage"}},
		{name: "no offering serves block storage", in: taxonomy.StorageBlock, want: nil},
		{name: "unclassified is never offered", in: taxonomy.Unclassified, want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, offeringNames(cat.OfferingsFor(tc.in))); diff != "" {
				t.Errorf("OfferingsFor(%q) mismatch (-want +got):\n%s", tc.in, diff)
			}
		})
	}
}

func TestOfferingHas(t *testing.T) {
	t.Parallel()

	offering := catalog.Offering{
		Name:         "Kapsule",
		Categories:   []taxonomy.Category{taxonomy.KubernetesManaged},
		Capabilities: []taxonomy.Capability{taxonomy.Autoscaling, taxonomy.NodeHighAvailability},
		Regions:      []string{"fr-par"},
	}

	tests := []struct {
		name string
		in   taxonomy.Capability
		want bool
	}{
		{name: "listed", in: taxonomy.Autoscaling, want: true},
		{name: "listed last", in: taxonomy.NodeHighAvailability, want: true},
		{name: "not listed", in: taxonomy.PointInTimeRestore, want: false},
		{name: "empty", in: taxonomy.Capability(""), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := offering.Has(tc.in); got != tc.want {
				t.Errorf("Has(%q) = %t, want %t", tc.in, got, tc.want)
			}
		})
	}
}

// A snapshot may list only zone-level high availability: the taxonomy holds the
// implication zone level implies node level, and New applies it to every
// offering, so Offering.Has answers correctly everywhere (mapping, tests, a
// future comparison report). Resources are never expanded, so this is the one
// place the implication is applied. Capabilities stay sorted and de-duplicated,
// so a node-level need appears exactly once.
func TestNewExpandsImpliedCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		listed  []taxonomy.Capability
		want    []taxonomy.Capability
		hasNode bool
		hasZone bool
	}{
		{
			// The shape milestone 2 and 3 snapshots will have: only the zone
			// level is written down.
			name:    "zone level gives node level too",
			listed:  []taxonomy.Capability{taxonomy.ZoneHighAvailability},
			want:    []taxonomy.Capability{taxonomy.NodeHighAvailability, taxonomy.ZoneHighAvailability},
			hasNode: true,
			hasZone: true,
		},
		{
			// The implication runs downwards only: a node-level offering does not
			// survive the loss of a zone, so it must not claim the zone level.
			name:    "node level does not give zone level",
			listed:  []taxonomy.Capability{taxonomy.NodeHighAvailability},
			want:    []taxonomy.Capability{taxonomy.NodeHighAvailability},
			hasNode: true,
			hasZone: false,
		},
		{
			// A snapshot may list both levels. That is not a duplicate capability:
			// the catalog still holds the node level once.
			name:    "both levels listed stay valid",
			listed:  []taxonomy.Capability{taxonomy.ZoneHighAvailability, taxonomy.NodeHighAvailability},
			want:    []taxonomy.Capability{taxonomy.NodeHighAvailability, taxonomy.ZoneHighAvailability},
			hasNode: true,
			hasZone: true,
		},
		{
			// The implication reaches only the capability the taxonomy states it
			// for: everything else the snapshot lists is kept as given.
			name:    "an unrelated capability is left alone",
			listed:  []taxonomy.Capability{taxonomy.IPv6, taxonomy.ZoneHighAvailability},
			want:    []taxonomy.Capability{taxonomy.IPv6, taxonomy.NodeHighAvailability, taxonomy.ZoneHighAvailability},
			hasNode: true,
			hasZone: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{{
				Name:         "Managed Database for PostgreSQL",
				Categories:   []taxonomy.Category{taxonomy.DatabasePostgreSQL},
				Capabilities: tc.listed,
				Regions:      []string{"fr-par"},
			}})

			offering := cat.Offerings()[0]

			if diff := cmp.Diff(tc.want, offering.Capabilities); diff != "" {
				t.Errorf("Capabilities mismatch (-want +got):\n%s", diff)
			}

			if got := offering.Has(taxonomy.NodeHighAvailability); got != tc.hasNode {
				t.Errorf("Has(node-high-availability) = %t, want %t", got, tc.hasNode)
			}

			if got := offering.Has(taxonomy.ZoneHighAvailability); got != tc.hasZone {
				t.Errorf("Has(zone-high-availability) = %t, want %t", got, tc.hasZone)
			}
		})
	}
}

// The retrieval instant is normalised to UTC: report renderers format it as a
// UTC calendar date, so keeping the caller's zone would shift the date.
func TestSnapshotRetrievedOnIsUTC(t *testing.T) {
	t.Parallel()

	snapshot := catalog.Snapshot{
		SourceURL:     "https://www.scaleway.com/en/pricing/",
		RetrievedOn:   time.Date(2026, 11, 1, 20, 0, 0, 0, time.FixedZone("x", -10*60*60)),
		DefaultRegion: "fr-par",
	}

	cat := newCatalog(t, targetName, snapshot, validOfferings())

	got := cat.Snapshot()
	if got.RetrievedOn.Location() != time.UTC {
		t.Errorf("Snapshot().RetrievedOn.Location() = %v, want UTC", got.RetrievedOn.Location())
	}

	if !got.RetrievedOn.Equal(snapshot.RetrievedOn) {
		t.Errorf("Snapshot().RetrievedOn = %v, want the same instant as %v", got.RetrievedOn, snapshot.RetrievedOn)
	}

	if got.SourceURL != snapshot.SourceURL {
		t.Errorf("Snapshot().SourceURL = %q, want %q", got.SourceURL, snapshot.SourceURL)
	}
}

// SourceURL is what a report cites as the origin of the data, so it must be a
// plain absolute https URL: no credentials, no host hidden behind an at sign, no
// characters that are unsafe outside a URL. It is free text as well, so control
// characters and over-long values are rejected by text.Validate. Errors name the
// source URL and stay bounded whatever the input.
func TestSnapshotSourceURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		// wantErrs are the sentinel errors the error must wrap; nil means the URL
		// is accepted.
		wantErrs []error
		// secret is the part of a rejected URL that must never be echoed. Saying
		// which character of a password is wrong would put that character in a
		// report, so the error must be silent about it.
		secret string
	}{
		// Accepted: a plain page, the same page with a query and a fragment, and
		// parentheses and escaped characters, which are legal URL characters.
		// Escaping them for a Markdown table is the reporter's job.
		{name: "pricing page", url: "https://www.scaleway.com/en/pricing/"},
		{name: "query and fragment", url: "https://www.scaleway.com/en/pricing/?tab=compute#instances"},
		{name: "parentheses", url: "https://example.com/a(b)"},
		{name: "escaped space", url: "https://example.com/a%20b"},
		{name: "sub domain", url: "https://sub-domain.example.com/x"},
		// Hardening against an HTML or Markdown renderer must not refuse what is
		// a plain URL: an ampersand between query parameters is not an entity,
		// and a percent escape that decodes to text is part of the URL.
		{name: "bare ampersands between query parameters", url: "https://example.com/?a=1&b=2"},
		{name: "ampersand and equals before a semicolon", url: "https://example.com/?a=1&b=2;c=3"},
		// An ampersand that starts no reference shape is a query parameter: a
		// named reference without its semicolon (&amp, &copy=2) is accepted on
		// purpose, because refusing it by prefix would refuse ordinary parameters
		// such as &region= and &section=.
		{name: "ampersand before a parameter called region", url: "https://example.com/?a=1&region=eu"},
		// An at sign is user info only in the authority, before the path or the
		// query starts.
		{name: "at sign in the path", url: "https://example.com/a@b"},
		{name: "at sign in the query", url: "https://example.com/?q=a@b"},
		{name: "percent encoded non ascii", url: "https://example.com/caf%C3%A9"},
		{name: "percent encoded ampersand", url: "https://example.com/?q=AT%26T"},
		{name: "percent encoded percent sign", url: "https://example.com/100%25"},

		// Invalid free text: rejected before the URL is even parsed.
		{
			name:     "right to left override",
			url:      "https://example.com/a\u202eb",
			wantErrs: []error{catalog.ErrInvalidCatalog, text.ErrInvalid},
		},
		{
			name:     "longer than max len",
			url:      "https://example.com/" + strings.Repeat("a", text.MaxLen),
			wantErrs: []error{catalog.ErrInvalidCatalog, text.ErrInvalid},
		},

		// Not a plain page: credentials in the URL would end up in a report, and a
		// host in front of an at sign hides the real one.
		{name: "userinfo with password", url: "https://user:pass@example.com/", wantErrs: []error{catalog.ErrInvalidCatalog}, secret: "pass"},
		{name: "look-alike host before at sign", url: "https://www.scaleway.com@evil.example/", wantErrs: []error{catalog.ErrInvalidCatalog}, secret: "www.scaleway.com"},
		{name: "port without host", url: "https://:443/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "opaque url", url: "https:example.com/opaque", wantErrs: []error{catalog.ErrInvalidCatalog}},

		// A host must be a plain DNS name. Percent-encoding lets an invisible,
		// blank or non-ASCII character into the host, which a reader cannot see and
		// a report would print as an ordinary-looking domain.
		{name: "percent encoded right to left override host", url: "https://%E2%80%AEmoc.elgoog/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "percent encoded hangul filler host", url: "https://%E3%85%A4/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "percent encoded braille blank host", url: "https://%E2%A0%80/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "percent encoded invalid byte host", url: "https://%ff/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "percent encoded no break space host", url: "https://%C2%A0/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		// A host that is only an escape for '%' is not a host name at all.
		{name: "percent encoded percent host", url: "https://%25/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		// Characters that are only legal in a path or a query, or not at all.
		{name: "parentheses in host", url: "https://exa(mple).com/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "ampersand in host", url: "https://example.com&evil/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		// An IP literal is not a host name, and square brackets anywhere in a URL
		// are the syntax of a link in Markdown.
		{name: "ipv6 literal host", url: "https://[2001:db8::1]/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "ipv4 literal host", url: "https://192.0.2.1/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		// A browser resolves a host whose last label is a number as an IPv4
		// address: one label of decimal digits, or of 0x and hexadecimal digits.
		// 0x7f.1 and 2130706433 are both 127.0.0.1.
		{name: "short hexadecimal ipv4 host", url: "https://0x7f.1/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "decimal integer ipv4 host", url: "https://2130706433/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "decimal last label", url: "https://example.123/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "hexadecimal last label with a lower x", url: "https://example.0x1f/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "hexadecimal last label with an upper X", url: "https://example.0X1F/", wantErrs: []error{catalog.ErrInvalidCatalog}},
		// A number anywhere but the last label is a name a browser keeps: the
		// first label may start with one, and a letter later makes the label text.
		{name: "host name starting with a digit", url: "https://1password.com/"},
		{name: "sub domain starting with a digit", url: "https://123.example.com/"},
		{name: "last label with letters and a digit", url: "https://example.co1/"},
		{name: "www sub domain", url: "https://www.example.com/"},
		{
			name:     "markdown image in path",
			url:      "https://example.com/a)![i](https://evil.example/x.png)",
			wantErrs: []error{catalog.ErrInvalidCatalog},
		},
		// A percent sign must start a complete two-digit escape, in a query too.
		{name: "invalid escape in query", url: "https://example.com/?q=%zz", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "truncated escape in path", url: "https://example.com/a%2", wantErrs: []error{catalog.ErrInvalidCatalog}},

		// Characters a URL does not allow as given.
		{name: "space", url: "https://example.com/a b", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "double quote", url: "https://example.com/a\"b", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "angle brackets", url: "https://example.com/<x>", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "backtick", url: "https://example.com/a`b", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "pipe", url: "https://example.com/a|b", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "backslash", url: "https://example.com/a\\b", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "caret", url: "https://example.com/a^b", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "braces", url: "https://example.com/a{b}", wantErrs: []error{catalog.ErrInvalidCatalog}},

		// Defence in depth: a report shows the source URL as a code span, and an
		// HTML or Markdown renderer would turn an entity reference into the
		// character it names, so the URL a reader sees would not be the URL the
		// catalog holds. Entity shapes are refused rather than rendered.
		{name: "named entity reference", url: "https://example.com/?q=&amp;", wantErrs: []error{catalog.ErrInvalidCatalog}},
		// A named entity followed by a digit is the shape some parsers accept and
		// others do not (&lt without its semicolon), so it is refused: the URL a
		// reader sees would depend on the renderer.
		{name: "named entity with digits", url: "https://example.com/a&lt1;b", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "decimal numeric entity", url: "https://example.com/?q=&#38;", wantErrs: []error{catalog.ErrInvalidCatalog}},
		// HTML decodes a numeric reference without its semicolon too, so a bare
		// &#38 or &#x26 is a reference shape as well.
		{name: "decimal numeric reference without a semicolon", url: "https://example.com/?x&#38", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "decimal numeric reference without a semicolon before a letter", url: "https://example.com/?x&#38x", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "hex numeric entity with a lower x", url: "https://example.com/?q=&#x26;", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "hex numeric entity with an upper X", url: "https://example.com/?q=&#X26;", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "hex numeric reference with a lower x and no semicolon", url: "https://example.com/?x&#x26", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "hex numeric reference with an upper X and no semicolon", url: "https://example.com/?x&#X26", wantErrs: []error{catalog.ErrInvalidCatalog}},
		// An ampersand and a hash that start no numeric reference are plain text:
		// &# then a letter that is not x, or &#x then a letter that is not hex.
		{name: "ampersand and hash before a letter", url: "https://example.com/?a=1&#frag"},
		{name: "ampersand and hash and x before a non hex letter", url: "https://example.com/?a=1&#xyz"},

		// A percent escape decodes to a byte, and a byte that is not valid text
		// has nothing a report can show: %0A is a line break, %00 a NUL,
		// %E2%80%AE a bidi override and %FF no UTF-8 at all.
		{name: "escape decoding to a line feed", url: "https://example.com/a%0Ab", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "escape decoding to NUL", url: "https://example.com/a%00b", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "escape decoding to a right to left override", url: "https://example.com/a%E2%80%AEb", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "escape decoding to a right to left override in lower case hex", url: "https://example.com/a%e2%80%aeb", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "escape decoding to a byte that is not UTF-8", url: "https://example.com/a%FFb", wantErrs: []error{catalog.ErrInvalidCatalog}},
		{name: "right to left override escape in the query", url: "https://example.com/?q=%E2%80%AE", wantErrs: []error{catalog.ErrInvalidCatalog}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			snapshot := catalog.Snapshot{
				SourceURL:     tc.url,
				RetrievedOn:   validSnapshot().RetrievedOn,
				DefaultRegion: validSnapshot().DefaultRegion,
			}

			_, err := catalog.New(targetName, snapshot, validOfferings())
			if len(tc.wantErrs) == 0 {
				if err != nil {
					t.Fatalf("New() with source URL %q returned unexpected error: %v", tc.url, err)
				}

				return
			}

			for _, wantErr := range tc.wantErrs {
				if !errors.Is(err, wantErr) {
					t.Fatalf("New() with source URL %q error = %v, want an error wrapping %v", tc.url, err, wantErr)
				}
			}

			msg := err.Error()
			if lower := strings.ToLower(msg); !strings.Contains(lower, "source url") {
				t.Errorf("New() error = %q, want it to name the field %q", msg, "source URL")
			}

			// The URL may carry credentials, so it is never echoed back.
			if strings.Contains(msg, tc.url) {
				t.Errorf("New() error = %q, want it not to echo the source URL", msg)
			}

			assertNoSecret(t, msg, tc.secret)
		})
	}
}

// assertNoSecret fails when msg echoes secret or any three-rune run of it: an
// error that prints part of a credential is as bad as one that prints it all.
func assertNoSecret(t *testing.T, msg, secret string) {
	t.Helper()

	if secret == "" {
		return
	}

	runes := []rune(secret)
	for i := 0; i+2 < len(runes); i++ {
		if part := string(runes[i : i+3]); strings.Contains(msg, part) {
			t.Errorf("error = %q, want it not to echo %q of the credential", msg, part)
		}
	}
}

// The reason a source URL is rejected must not depend on the credentials it
// carries: an error that named the offending character, or where it was, would
// put part of a password into a report. So a URL that differs from a rejected one
// only in its user info produces exactly the same message.
func TestSnapshotSourceURLErrorDoesNotEchoUserInfo(t *testing.T) {
	t.Parallel()

	baseline := sourceURLError(t, "https://user:pass@example.com/").Error()

	tests := []struct {
		name string
		url  string
		// secrets are the strings whose presence in the message would mean it
		// echoed the credentials: the secret itself, and the code point the
		// offending character has.
		secrets []string
	}{
		{
			name:    "non ascii password",
			url:     "https://user:péss@example.com/",
			secrets: []string{"péss", "U+00E9"},
		},
		{
			name:    "character an rfc 3986 url does not allow",
			url:     "https://user:pa^ss@example.com/",
			secrets: []string{"pa^ss", "pa", "U+005E"},
		},
		// A browser's URL parser reads the user info after a single slash, or with
		// none at all, as user info too, so the reader must find it there before it
		// names a character of the password.
		{
			name:    "single slash user info with a character an rfc 3986 url does not allow",
			url:     "https:/user:p^ss@example.com/",
			secrets: []string{"p^ss", "U+005E"},
		},
		{
			name:    "single slash user info with a non ascii password",
			url:     "https:/user:pässword@example.com/",
			secrets: []string{"pässword", "U+00E4"},
		},
		{
			name:    "no slash user info with a character an rfc 3986 url does not allow",
			url:     "https:user:p^ss@example.com/",
			secrets: []string{"p^ss", "U+005E"},
		},
		// A browser strips leading spaces and C0 controls before it parses a URL,
		// so what follows them is user info the reader must find before it names a
		// character of the password.
		{
			name:    "leading space and a zero width space in the password",
			url:     " https://user:p\u200bss@example.com/",
			secrets: []string{"U+200B"},
		},
		{
			name:    "leading space and a c0 control in the password",
			url:     " https://user:p\x01ss@example.com/",
			secrets: []string{"U+0001"},
		},
		{
			name:    "leading space and a soft hyphen in the password",
			url:     " https://user:p\u00adss@example.com/",
			secrets: []string{"U+00AD"},
		},
		// Some parsers do not treat a backslash as a separator, so this is user
		// info with a backslash password rather than a path.
		{
			name:    "backslash in the password",
			url:     "https://user:p\\ss@example.com/",
			secrets: []string{"U+005C"},
		},
		{
			name:    "zero width space then a backslash in the password",
			url:     "https://user:p\u200b\\ss@example.com/",
			secrets: []string{"U+200B"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			msg := sourceURLError(t, tc.url).Error()

			if msg != baseline {
				t.Errorf("New() error = %q, want the same message as for a rejected URL with other user info (%q)", msg, baseline)
			}

			for _, secret := range tc.secrets {
				if strings.Contains(msg, secret) {
					t.Errorf("New() error = %q, want it not to echo %q", msg, secret)
				}
			}

			if strings.Contains(msg, tc.url) {
				t.Errorf("New() error = %q, want it not to echo the source URL", msg)
			}
		})
	}
}

// An '@' that is outside the authority is not user info, but the reader cannot
// know that before url.Parse has spoken: the check that runs before the parse
// gives the bare message for any URL holding an '@', and the checks that run
// after it keep their detail. ADR 0009 records the trade-off; this pins both
// sides of it, so a reordering of the checks cannot silently flip either.
func TestSnapshotSourceURLErrorDetailAroundUserInfoGuard(t *testing.T) {
	t.Parallel()

	baseline := sourceURLError(t, "https://user:pass@example.com/").Error()

	tests := []struct {
		name string
		url  string
		// bare is true when the message must be exactly the one for a URL
		// with user info: the failing check runs before url.Parse.
		bare bool
		// detail is what the message must keep when bare is false: the check
		// runs after url.Parse confirmed there is no user info.
		detail []string
	}{
		{
			name: "at sign in the path and an incomplete escape",
			url:  "https://example.com/a@b/%ZZ",
			bare: true,
		},
		{
			name:   "at sign in the path and an escape that is not UTF-8",
			url:    "https://example.com/a@b/%FF",
			detail: []string{"byte 24"},
		},
		{
			name:   "at sign in the query and an escape that decodes to a line feed",
			url:    "https://example.com/?q=a@b%0A",
			detail: []string{"U+000A"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			msg := sourceURLError(t, tc.url).Error()

			if tc.bare {
				if msg != baseline {
					t.Errorf("New() error = %q, want the same message as for a rejected URL with other user info (%q)", msg, baseline)
				}
			} else if msg == baseline {
				t.Errorf("New() error = %q, want it to keep the detail of the check, not the bare message for user info", msg)
			}

			for _, part := range tc.detail {
				if !strings.Contains(msg, part) {
					t.Errorf("New() error = %q, want it to contain %q", msg, part)
				}
			}

			if strings.Contains(msg, tc.url) {
				t.Errorf("New() error = %q, want it not to echo the source URL", msg)
			}
		})
	}
}

// sourceURLError returns the error New rejects rawURL with, failing the test when
// the URL is accepted or the error does not say the catalog is invalid.
func sourceURLError(t *testing.T, rawURL string) error {
	t.Helper()

	snapshot := catalog.Snapshot{
		SourceURL:     rawURL,
		RetrievedOn:   validSnapshot().RetrievedOn,
		DefaultRegion: validSnapshot().DefaultRegion,
	}

	_, err := catalog.New(targetName, snapshot, validOfferings())
	if err == nil {
		t.Fatalf("New() with source URL %q returned no error, want one wrapping %v", rawURL, catalog.ErrInvalidCatalog)
	}

	if !errors.Is(err, catalog.ErrInvalidCatalog) {
		t.Fatalf("New() with source URL %q error = %v, want an error wrapping %v", rawURL, err, catalog.ErrInvalidCatalog)
	}

	return err
}

// A resource with no capabilities and one with an empty capability list are the
// same offering; callers must not have to care which spelling a fixture uses.
func TestNewCanonicalisesEmptyCapabilities(t *testing.T) {
	t.Parallel()

	withNil := catalog.Offering{
		Name:         "Instances",
		Categories:   []taxonomy.Category{taxonomy.ComputeVM},
		Capabilities: nil,
		Regions:      []string{"fr-par"},
	}

	withEmpty := catalog.Offering{
		Name:         "Instances",
		Categories:   []taxonomy.Category{taxonomy.ComputeVM},
		Capabilities: []taxonomy.Capability{},
		Regions:      []string{"fr-par"},
	}

	nilCat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{withNil})
	emptyCat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{withEmpty})

	if got := nilCat.Offerings()[0].Capabilities; got != nil {
		t.Errorf("Capabilities = %#v for nil input, want nil", got)
	}

	if got := emptyCat.Offerings()[0].Capabilities; got != nil {
		t.Errorf("Capabilities = %#v for empty input, want nil", got)
	}

	if diff := cmp.Diff(nilCat.Offerings(), emptyCat.Offerings()); diff != "" {
		t.Errorf("nil and empty capabilities are not canonicalised to the same offering (-nil +empty):\n%s", diff)
	}
}

func TestNewErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		target    string
		snapshot  catalog.Snapshot
		offerings []catalog.Offering
		wantErr   error
		wantMsg   []string
	}{
		{
			name:      "empty target",
			target:    "",
			snapshot:  validSnapshot(),
			offerings: validOfferings(),
			wantErr:   catalog.ErrInvalidCatalog,
		},
		{
			name:      "whitespace only target",
			target:    "   ",
			snapshot:  validSnapshot(),
			offerings: validOfferings(),
			wantErr:   catalog.ErrInvalidCatalog,
		},
		{
			name:   "empty snapshot source url",
			target: targetName,
			snapshot: catalog.Snapshot{
				RetrievedOn:   validSnapshot().RetrievedOn,
				DefaultRegion: validSnapshot().DefaultRegion,
			},
			offerings: validOfferings(),
			wantErr:   catalog.ErrInvalidCatalog,
		},
		{
			name:   "plain http source url",
			target: targetName,
			snapshot: catalog.Snapshot{
				SourceURL:     "http://www.scaleway.com/en/pricing/",
				RetrievedOn:   validSnapshot().RetrievedOn,
				DefaultRegion: validSnapshot().DefaultRegion,
			},
			offerings: validOfferings(),
			wantErr:   catalog.ErrInvalidCatalog,
		},
		{
			name:   "javascript source url",
			target: targetName,
			snapshot: catalog.Snapshot{
				SourceURL:     "javascript:alert(1)",
				RetrievedOn:   validSnapshot().RetrievedOn,
				DefaultRegion: validSnapshot().DefaultRegion,
			},
			offerings: validOfferings(),
			wantErr:   catalog.ErrInvalidCatalog,
		},
		{
			name:   "relative source url",
			target: targetName,
			snapshot: catalog.Snapshot{
				SourceURL:     "www.scaleway.com/en/pricing/",
				RetrievedOn:   validSnapshot().RetrievedOn,
				DefaultRegion: validSnapshot().DefaultRegion,
			},
			offerings: validOfferings(),
			wantErr:   catalog.ErrInvalidCatalog,
		},
		{
			name:   "https source url without host",
			target: targetName,
			snapshot: catalog.Snapshot{
				SourceURL:     "https://",
				RetrievedOn:   validSnapshot().RetrievedOn,
				DefaultRegion: validSnapshot().DefaultRegion,
			},
			offerings: validOfferings(),
			wantErr:   catalog.ErrInvalidCatalog,
		},
		{
			name:   "zero retrieval date",
			target: targetName,
			snapshot: catalog.Snapshot{
				SourceURL:     validSnapshot().SourceURL,
				DefaultRegion: validSnapshot().DefaultRegion,
			},
			offerings: validOfferings(),
			wantErr:   catalog.ErrInvalidCatalog,
		},
		{
			name:      "empty offering name",
			target:    targetName,
			snapshot:  validSnapshot(),
			offerings: []catalog.Offering{{Categories: []taxonomy.Category{taxonomy.ComputeVM}, Regions: []string{"fr-par"}}},
			wantErr:   catalog.ErrEmptyOfferingName,
		},
		{
			name:      "whitespace only offering name",
			target:    targetName,
			snapshot:  validSnapshot(),
			offerings: []catalog.Offering{{Name: "   ", Categories: []taxonomy.Category{taxonomy.ComputeVM}, Regions: []string{"fr-par"}}},
			wantErr:   catalog.ErrEmptyOfferingName,
		},
		{
			name:     "duplicate offering name",
			target:   targetName,
			snapshot: validSnapshot(),
			offerings: []catalog.Offering{
				{Name: "Instances", Categories: []taxonomy.Category{taxonomy.ComputeVM}, Regions: []string{"fr-par"}},
				{Name: "Instances", Categories: []taxonomy.Category{taxonomy.ComputeVM}, Regions: []string{"fr-par"}},
			},
			wantErr: catalog.ErrDuplicateOffering,
			wantMsg: []string{"Instances"},
		},
		{
			// A run of spaces is not a difference: two names that collapse to the
			// same text would appear as the same offering in a report.
			name:     "duplicate offering name differing in inner whitespace",
			target:   targetName,
			snapshot: validSnapshot(),
			offerings: []catalog.Offering{
				{Name: "Object Storage", Categories: []taxonomy.Category{taxonomy.StorageObject}, Regions: []string{"fr-par"}},
				{Name: "Object  Storage", Categories: []taxonomy.Category{taxonomy.StorageObject}, Regions: []string{"fr-par"}},
			},
			wantErr: catalog.ErrDuplicateOffering,
		},
		{
			name:      "offering serving no category",
			target:    targetName,
			snapshot:  validSnapshot(),
			offerings: []catalog.Offering{{Name: "Instances", Regions: []string{"fr-par"}}},
			wantErr:   catalog.ErrNoCategory,
			wantMsg:   []string{"Instances"},
		},
		{
			name:      "offering without regions",
			target:    targetName,
			snapshot:  validSnapshot(),
			offerings: []catalog.Offering{{Name: "Instances", Categories: []taxonomy.Category{taxonomy.ComputeVM}}},
			wantErr:   catalog.ErrNoRegion,
			wantMsg:   []string{"Instances"},
		},
		{
			name:     "offering with empty regions",
			target:   targetName,
			snapshot: validSnapshot(),
			offerings: []catalog.Offering{{
				Name:       "Instances",
				Categories: []taxonomy.Category{taxonomy.ComputeVM},
				Regions:    []string{},
			}},
			wantErr: catalog.ErrNoRegion,
			wantMsg: []string{"Instances"},
		},
		{
			name:     "offering with a blank region among valid ones",
			target:   targetName,
			snapshot: validSnapshot(),
			offerings: []catalog.Offering{{
				Name:       "Instances",
				Categories: []taxonomy.Category{taxonomy.ComputeVM},
				Regions:    []string{"fr-par", "   "},
			}},
			wantErr: catalog.ErrNoRegion,
			wantMsg: []string{"Instances"},
		},
		{
			name:     "offering with an empty region name",
			target:   targetName,
			snapshot: validSnapshot(),
			offerings: []catalog.Offering{{
				Name:       "Instances",
				Categories: []taxonomy.Category{taxonomy.ComputeVM},
				Regions:    []string{""},
			}},
			wantErr: catalog.ErrNoRegion,
			wantMsg: []string{"Instances"},
		},
		{
			// A target never offers "unclassified": that is the absence of a category.
			name:     "offering serving unclassified",
			target:   targetName,
			snapshot: validSnapshot(),
			offerings: []catalog.Offering{{
				Name:       "Mystery",
				Categories: []taxonomy.Category{taxonomy.Unclassified},
				Regions:    []string{"fr-par"},
			}},
			wantErr: catalog.ErrInvalidCatalog,
			wantMsg: []string{"Mystery", "unclassified"},
		},
		{
			name:     "offering with unknown category",
			target:   targetName,
			snapshot: validSnapshot(),
			offerings: []catalog.Offering{{
				Name:       "Instances",
				Categories: []taxonomy.Category{taxonomy.Category("compute.mv")},
				Regions:    []string{"fr-par"},
			}},
			wantErr: taxonomy.ErrUnknownCategory,
			wantMsg: []string{"Instances", "compute.mv"},
		},
		{
			name:     "offering with unknown capability",
			target:   targetName,
			snapshot: validSnapshot(),
			offerings: []catalog.Offering{{
				Name:         "Instances",
				Categories:   []taxonomy.Category{taxonomy.ComputeVM},
				Capabilities: []taxonomy.Capability{taxonomy.Capability("pitr")},
				Regions:      []string{"fr-par"},
			}},
			wantErr: taxonomy.ErrUnknownCapability,
			wantMsg: []string{"Instances", "pitr"},
		},
		{
			// ADR 0011 retired the unscoped value and catalog.New reads every
			// capability through taxonomy.ParseCapability, so a snapshot still
			// listing it is refused rather than silently read as one of the two
			// scopes. The error names the offering and quotes the retired value and
			// both replacements, which a file written before the split can act on.
			name:     "offering with retired high-availability capability",
			target:   targetName,
			snapshot: validSnapshot(),
			offerings: []catalog.Offering{{
				Name:         "Instances",
				Categories:   []taxonomy.Category{taxonomy.ComputeVM},
				Capabilities: []taxonomy.Capability{taxonomy.Capability("high-availability")},
				Regions:      []string{"fr-par"},
			}},
			wantErr: taxonomy.ErrUnknownCapability,
			// Each value is quoted: the retired value is a substring of
			// "node-high-availability", so the quotes keep the assertions apart.
			wantMsg: []string{
				text.Quote("Instances"),
				text.Quote("high-availability"),
				text.Quote("node-high-availability"),
				text.Quote("zone-high-availability"),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := catalog.New(tc.target, tc.snapshot, tc.offerings)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("New() error = %v, want an error wrapping %v", err, tc.wantErr)
			}

			for _, want := range tc.wantMsg {
				if msg := err.Error(); !strings.Contains(msg, want) {
					t.Errorf("New() error = %q, want it to mention %q", msg, want)
				}
			}
		})
	}
}

// Free text from a catalog reaches reports and terminals, so control characters,
// bidi overrides, invalid UTF-8 and over-long values are rejected instead of
// rendered. The error names the field and, when it has one, the offering.
func TestNewRejectsInvalidText(t *testing.T) {
	t.Parallel()

	invalid := []struct {
		name  string
		value string
	}{
		{name: "ansi escape", value: "\x1b[2J"},
		{name: "newline", value: "row\nvalue"},
		{name: "right to left override", value: "a\u202eb"},
		{name: "zero width space", value: "a\u200bb"},
		// Some renderers treat Zl and Zp as line breaks, the same risk as a newline.
		{name: "line separator", value: "a\u2028b"},
		{name: "paragraph separator", value: "a\u2029b"},
		{name: "invalid utf8", value: "a\xffb"},
		{name: "longer than max len", value: strings.Repeat("a", text.MaxLen+1)},
		// Leading and trailing whitespace is padding, not part of the value: a
		// padded name or region is not the same string as the name it pads.
		{name: "leading space", value: " Scaleway"},
		{name: "trailing space", value: "Scaleway "},
	}

	type invalidCase struct {
		name         string
		target       string
		offering     catalog.Offering
		wantField    string
		wantOffering string
		// wantErrs are the sentinel errors the error must wrap. The target is part
		// of the catalog's own validity, so its errors say ErrInvalidCatalog too;
		// an offering's name or region is only invalid free text.
		wantErrs []error
	}

	tests := make([]invalidCase, 0, len(invalid)*3)

	for _, tc := range invalid {
		tests = append(tests, invalidCase{
			name:      "target/" + tc.name,
			target:    tc.value,
			offering:  catalog.Offering{Name: "Instances", Categories: []taxonomy.Category{taxonomy.ComputeVM}, Regions: []string{"fr-par"}},
			wantField: "target",
			wantErrs:  []error{catalog.ErrInvalidCatalog, text.ErrInvalid},
		})

		tests = append(tests, invalidCase{
			name:      "offering name/" + tc.name,
			target:    targetName,
			offering:  catalog.Offering{Name: tc.value, Categories: []taxonomy.Category{taxonomy.ComputeVM}, Regions: []string{"fr-par"}},
			wantField: "name",
			wantErrs:  []error{text.ErrInvalid},
		})

		tests = append(tests, invalidCase{
			name:         "region/" + tc.name,
			target:       targetName,
			offering:     catalog.Offering{Name: "Instances", Categories: []taxonomy.Category{taxonomy.ComputeVM}, Regions: []string{"fr-par", tc.value}},
			wantField:    "region",
			wantOffering: "Instances",
			wantErrs:     []error{text.ErrInvalid},
		})
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := catalog.New(tc.target, validSnapshot(), []catalog.Offering{tc.offering})
			for _, wantErr := range tc.wantErrs {
				if !errors.Is(err, wantErr) {
					t.Fatalf("New() error = %v, want an error wrapping %v", err, wantErr)
				}
			}

			if err == nil {
				t.Fatalf("New() returned no error, want an error naming the field %q", tc.wantField)
			}

			msg := err.Error()
			if lower := strings.ToLower(msg); !strings.Contains(lower, tc.wantField) {
				t.Errorf("New() error = %q, want it to name the field %q", msg, tc.wantField)
			}

			if tc.wantOffering != "" && !strings.Contains(msg, tc.wantOffering) {
				t.Errorf("New() error = %q, want it to name the offering %q", msg, tc.wantOffering)
			}
		})
	}
}

// A non-ASCII space separator is legitimate text inside a name or a region: a
// target may sell "Offre\u00a0Pro" and name a region in its own language. Only an
// ID, which a reader retypes, rejects these characters, so an offering keeps them
// exactly as given.
func TestNewAcceptsNonASCIISpaceInFreeText(t *testing.T) {
	t.Parallel()

	separators := []struct {
		name string
		sep  string
	}{
		{name: "no break space", sep: "\u00a0"},
		{name: "ogham space mark", sep: "\u1680"},
		{name: "figure space", sep: "\u2007"},
		{name: "narrow no break space", sep: "\u202f"},
		{name: "ideographic space", sep: "\u3000"},
	}

	for _, tc := range separators {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			name := "Offre" + tc.sep + "Pro"
			region := "fr" + tc.sep + "par"

			cat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{{
				Name:       name,
				Categories: []taxonomy.Category{taxonomy.ComputeVM},
				Regions:    []string{region},
			}})

			got := cat.Offerings()
			if len(got) != 1 {
				t.Fatalf("Offerings() returned %d offerings, want 1", len(got))
			}

			if got[0].Name != name {
				t.Errorf("Name = %q, want %q stored as given", got[0].Name, name)
			}

			if diff := cmp.Diff([]string{region}, got[0].Regions); diff != "" {
				t.Errorf("Regions are not stored as given (-want +got):\n%s", diff)
			}
		})
	}
}

// Duplicate detection ignores runs of inner whitespace, but a stored value is
// never normalised: a report shows the target's own spelling of the name.
func TestNewKeepsNameAsGiven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		offeringName string
	}{
		{name: "single inner space", offeringName: "Object Storage"},
		{name: "double inner space", offeringName: "Object  Storage"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{{
				Name:       tc.offeringName,
				Categories: []taxonomy.Category{taxonomy.StorageObject},
				Regions:    []string{"fr-par"},
			}})

			got := cat.Offerings()
			if len(got) != 1 {
				t.Fatalf("Offerings() returned %d offerings, want 1", len(got))
			}

			if got[0].Name != tc.offeringName {
				t.Errorf("Name = %q, want %q stored as given", got[0].Name, tc.offeringName)
			}
		})
	}
}

// The constants of ADR 0024 fix the units a plan, a price and a memory size are
// held in, and the caps that bound the arithmetic of milestone 6. They are part
// of the format, so they are pinned here rather than left to a reader of the Go
// source: a snapshot writer, another repository's test and an estimate all
// depend on the same numbers.
func TestCatalogConstants(t *testing.T) {
	t.Parallel()

	if catalog.AmountScale != 10_000 {
		t.Errorf("AmountScale = %d, want 10000 units in one currency unit", catalog.AmountScale)
	}

	if catalog.MaxAmount != 1_000_000*catalog.AmountScale {
		t.Errorf("MaxAmount = %s, want one million currency units", catalog.MaxAmount)
	}

	if catalog.EUR != "EUR" {
		t.Errorf("EUR = %q, want %q: the only currency a price may hold", catalog.EUR, "EUR")
	}

	if catalog.MaxPlanVCPUs != 1<<16 {
		t.Errorf("MaxPlanVCPUs = %d, want %d", catalog.MaxPlanVCPUs, 1<<16)
	}

	if catalog.MaxPlanMemoryMilliGiB != (1<<24)*1_000 {
		t.Errorf("MaxPlanMemoryMilliGiB = %d, want %d thousandths of a GiB", catalog.MaxPlanMemoryMilliGiB, (1<<24)*1_000)
	}

	if catalog.MaxIncludedStorageGiB != 1<<30 {
		t.Errorf("MaxIncludedStorageGiB = %d, want %d", catalog.MaxIncludedStorageGiB, 1<<30)
	}

	if catalog.MaxBillingCapHours != 744 {
		t.Errorf("MaxBillingCapHours = %d, want 744 hours, the hours of a 31-day month", catalog.MaxBillingCapHours)
	}
}

// Amount.String formats an exact amount as a plain decimal: at least two
// fractional digits, the trailing zeros dropped, and a leading minus sign when
// the amount is negative. It is how a report and a SOURCES.md show a price, so
// two sources holding the same price must produce the same text.
func TestAmountString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		amount catalog.Amount
		want   string
	}{
		{name: "zero", amount: 0, want: "0.00"},
		{name: "ten-thousandth", amount: 1, want: "0.0001"},
		{name: "ten ten-thousandths", amount: 10, want: "0.001"},
		{name: "four cents", amount: 400, want: "0.04"},
		{name: "twelve and a half cents", amount: 1250, want: "0.125"},
		{name: "twelve and a half", amount: 125000, want: "12.50"},
		{name: "thirteen and a hundred and a quarter", amount: 130125, want: "13.0125"},
		{name: "the largest amount", amount: catalog.MaxAmount, want: "1000000.00"},
		{name: "four cents below zero", amount: -400, want: "-0.04"},
		{name: "twelve and a half below zero", amount: -125000, want: "-12.50"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.amount.String(); got != tc.want {
				t.Errorf("Amount(%d).String() = %q, want %q", int64(tc.amount), got, tc.want)
			}
		})
	}
}

// pricedOffering is an offering with plans, storage prices and fixed fees whose
// input is deliberately unsorted and repeated: New must return the plans sorted
// by name, each price's regions sorted and de-duplicated, and the prices sorted
// by their first region with the every-region price first. Its storage prices
// and its fixed fees are region-scoped, one of them repeated, so a test can
// change a storage or a fee region in place as well as a plan price's (ADR 0024,
// ADR 0026).
func pricedOffering() catalog.Offering {
	return catalog.Offering{
		Name:         "Example Compute",
		Categories:   []taxonomy.Category{taxonomy.ComputeVM},
		Capabilities: []taxonomy.Capability{taxonomy.IPv6},
		Regions:      []string{"fr-par", "nl-ams"},
		Plans: []catalog.Plan{
			{
				Name:               "EC-2-4",
				VCPUs:              2,
				MemoryMilliGiB:     4000,
				IncludedStorageGiB: 20,
				Prices: []catalog.Price{
					{Amount: 130125, Currency: catalog.EUR, Regions: []string{"nl-ams", "nl-ams"}},
					{Amount: 125000, Currency: catalog.EUR, Regions: []string{"fr-par"}},
				},
			},
			{
				Name:           "EC-1-0.5",
				VCPUs:          1,
				MemoryMilliGiB: 500,
				Prices:         []catalog.Price{{Amount: 36500, Currency: catalog.EUR}},
			},
		},
		StoragePrices: []catalog.Price{
			{Amount: 400, Currency: catalog.EUR, Regions: []string{"nl-ams", "nl-ams"}},
			{Amount: 450, Currency: catalog.EUR, Regions: []string{"fr-par"}},
		},
		// The fixed monthly fee per instance (one cluster), on top of the plans
		// and the storage: a free control plane in fr-par, a paid one in nl-ams,
		// out of order and with a region repeated (ADR 0026).
		FixedFees: []catalog.Price{
			{Amount: 657000, Currency: catalog.EUR, Regions: []string{"nl-ams", "nl-ams"}},
			{Amount: 0, Currency: catalog.EUR, Regions: []string{"fr-par"}},
		},
	}
}

// pricedOfferingNormalised is what the catalog must hold for pricedOffering:
// plans by name, price regions sorted, de-duplicated and nil when empty, and the
// prices by first region with the every-region price first.
func pricedOfferingNormalised() catalog.Offering {
	return catalog.Offering{
		Name:         "Example Compute",
		Categories:   []taxonomy.Category{taxonomy.ComputeVM},
		Capabilities: []taxonomy.Capability{taxonomy.IPv6},
		Regions:      []string{"fr-par", "nl-ams"},
		Plans: []catalog.Plan{
			{
				Name:           "EC-1-0.5",
				VCPUs:          1,
				MemoryMilliGiB: 500,
				Prices:         []catalog.Price{{Amount: 36500, Currency: catalog.EUR}},
			},
			{
				Name:               "EC-2-4",
				VCPUs:              2,
				MemoryMilliGiB:     4000,
				IncludedStorageGiB: 20,
				Prices: []catalog.Price{
					{Amount: 125000, Currency: catalog.EUR, Regions: []string{"fr-par"}},
					{Amount: 130125, Currency: catalog.EUR, Regions: []string{"nl-ams"}},
				},
			},
		},
		StoragePrices: []catalog.Price{
			{Amount: 450, Currency: catalog.EUR, Regions: []string{"fr-par"}},
			{Amount: 400, Currency: catalog.EUR, Regions: []string{"nl-ams"}},
		},
		FixedFees: []catalog.Price{
			{Amount: 0, Currency: catalog.EUR, Regions: []string{"fr-par"}},
			{Amount: 657000, Currency: catalog.EUR, Regions: []string{"nl-ams"}},
		},
	}
}

// offeringChanged returns pricedOffering with mutate applied, so a case breaks
// exactly one plan, price or list and leaves the rest valid.
func offeringChanged(mutate func(*catalog.Offering)) catalog.Offering {
	o := pricedOffering()
	mutate(&o)

	return o
}

// TestNewAcceptsPlansAndPrices pins the canonical form New gives plans, prices
// and fixed fees: plans sorted by name, each price's regions sorted and
// de-duplicated with every-region prices keeping no regions at all, and the
// prices of each list sorted by first region with the every-region price first
// (ADR 0024, ADR 0026).
func TestNewAcceptsPlansAndPrices(t *testing.T) {
	t.Parallel()

	cat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{pricedOffering()})

	got := cat.Offerings()
	if len(got) != 1 {
		t.Fatalf("Offerings() returned %d offerings, want 1", len(got))
	}

	want := pricedOfferingNormalised()

	if diff := cmp.Diff(want.Plans, got[0].Plans); diff != "" {
		t.Errorf("Plans are not sorted and normalised (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(want.StoragePrices, got[0].StoragePrices); diff != "" {
		t.Errorf("StoragePrices are not sorted and normalised (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(want.FixedFees, got[0].FixedFees); diff != "" {
		t.Errorf("FixedFees are not sorted and normalised (-want +got):\n%s", diff)
	}
}

// TestNewFixedFees pins the canonical form New gives an offering's fixed fees:
// each fee's regions sorted and de-duplicated, a fee with no regions kept as no
// regions at all (it applies to every region of the offering), and the fees
// sorted by their first region with the every-region fee first (ADR 0026).
func TestNewFixedFees(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		offering catalog.Offering
		want     []catalog.Price
	}{
		{
			// A fee with no regions is how a target says it charges the same fee
			// everywhere: one fee, covering the offering's fr-par and nl-ams.
			name: "a fee that applies to every region",
			offering: catalog.Offering{
				Name:       "Example Kubernetes",
				Categories: []taxonomy.Category{taxonomy.KubernetesManaged},
				Regions:    []string{"fr-par", "nl-ams"},
				FixedFees: []catalog.Price{
					{Amount: 657000, Currency: catalog.EUR},
				},
			},
			want: []catalog.Price{
				{Amount: 657000, Currency: catalog.EUR},
			},
		},
		{
			// The fees are sorted by their first region, and a region listed
			// twice in one fee is one region, not an overlap.
			name: "fees over several regions, out of order and repeated",
			offering: catalog.Offering{
				Name:       "Example Kubernetes",
				Categories: []taxonomy.Category{taxonomy.KubernetesManaged},
				Regions:    []string{"fr-par", "nl-ams", "pl-waw"},
				FixedFees: []catalog.Price{
					{Amount: 100, Currency: catalog.EUR, Regions: []string{"nl-ams"}},
					{Amount: 200, Currency: catalog.EUR, Regions: []string{"pl-waw", "fr-par", "fr-par"}},
				},
			},
			want: []catalog.Price{
				{Amount: 200, Currency: catalog.EUR, Regions: []string{"fr-par", "pl-waw"}},
				{Amount: 100, Currency: catalog.EUR, Regions: []string{"nl-ams"}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{tc.offering})

			got := cat.Offerings()
			if len(got) != 1 {
				t.Fatalf("Offerings() returned %d offerings, want 1", len(got))
			}

			if diff := cmp.Diff(tc.want, got[0].FixedFees); diff != "" {
				t.Errorf("FixedFees are not sorted and normalised (-want +got):\n%s", diff)
			}
		})
	}
}

// TestOfferingFixedFee pins the method an estimate asks for the fixed monthly
// fee of one instance in one region (ADR 0026): the fee of that region and
// true, a fee of 0 with true when the region's control plane is included, and 0
// with false when the offering charges no fixed fee at all or is not available
// in the region. The boolean is what tells "free" from "no fee".
func TestOfferingFixedFee(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		offering catalog.Offering
		want     map[string]catalog.Amount
	}{
		{
			name: "a fee for each region, one of them free",
			offering: catalog.Offering{
				Name:       "Example Kubernetes",
				Categories: []taxonomy.Category{taxonomy.KubernetesManaged},
				Regions:    []string{"fr-par", "nl-ams"},
				FixedFees: []catalog.Price{
					{Amount: 657000, Currency: catalog.EUR, Regions: []string{"fr-par"}},
					{Amount: 0, Currency: catalog.EUR, Regions: []string{"nl-ams"}},
				},
			},
			want: map[string]catalog.Amount{"fr-par": 657000, "nl-ams": 0},
		},
		{
			name: "a fee that applies to every region",
			offering: catalog.Offering{
				Name:       "Example Kubernetes",
				Categories: []taxonomy.Category{taxonomy.KubernetesManaged},
				Regions:    []string{"fr-par", "nl-ams"},
				FixedFees: []catalog.Price{
					{Amount: 657000, Currency: catalog.EUR},
				},
			},
			want: map[string]catalog.Amount{"fr-par": 657000, "nl-ams": 657000},
		},
		{
			name: "no fixed fee",
			offering: catalog.Offering{
				Name:       "Example Kubernetes",
				Categories: []taxonomy.Category{taxonomy.KubernetesManaged},
				Regions:    []string{"fr-par", "nl-ams"},
			},
			want: map[string]catalog.Amount{},
		},
	}

	// A region no case may charge a fee in: it is not a region of any offering
	// above, and a fee is never borrowed from another region.
	const elsewhere = "de-fra"

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{tc.offering})

			offering := cat.Offerings()[0]

			for _, region := range append(offering.Regions, elsewhere, "") {
				want, wantOK := tc.want[region]

				got, ok := offering.FixedFee(region)
				if ok != wantOK {
					t.Errorf("FixedFee(%s) reports a fee: %t, want %t",
						text.Quote(region), ok, wantOK)

					continue
				}

				if got != want {
					t.Errorf("FixedFee(%s) = %s, want %s",
						text.Quote(region), got, want)
				}
			}
		})
	}
}

// An offering that sells no plan and publishes no storage price is a valid
// offering: an empty list is a value, not an absence (ADR 0024). The same holds
// for its fixed fees, which are left out of a document to say there are none
// (ADR 0026).
func TestNewOfferingWithoutPlansOrStoragePrices(t *testing.T) {
	t.Parallel()

	withoutLists := catalog.Offering{
		Name:       "Object Storage",
		Categories: []taxonomy.Category{taxonomy.StorageObject},
		Regions:    []string{"fr-par"},
	}

	withEmptyLists := withoutLists
	withEmptyLists.Plans = []catalog.Plan{}
	withEmptyLists.StoragePrices = []catalog.Price{}
	withEmptyLists.FixedFees = []catalog.Price{}

	for _, offering := range []catalog.Offering{withoutLists, withEmptyLists} {
		cat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{offering})

		got := cat.Offerings()[0]
		if got.Plans != nil {
			t.Errorf("Plans = %v for offering %q, want nil", got.Plans, got.Name)
		}

		if got.StoragePrices != nil {
			t.Errorf("StoragePrices = %v for offering %q, want nil", got.StoragePrices, got.Name)
		}

		if got.FixedFees != nil {
			t.Errorf("FixedFees = %v for offering %q, want nil", got.FixedFees, got.Name)
		}

		// No fixed fee is not a fee of zero: the boolean says which it is.
		if fee, ok := got.FixedFee("fr-par"); ok || fee != 0 {
			t.Errorf("FixedFee(%q) = (%s, %t) for offering %q, want (0.00, false)",
				"fr-par", fee, ok, got.Name)
		}
	}
}

// TestNewPlanAndPriceErrors pins every rule New holds for plans, prices and
// storage prices: the sentinel the error wraps, and that the message names the
// offering and the plan (or the list index) and the field at fault. A price that
// was silently dropped, rounded or read as free would change an estimate without
// a word (ADR 0024, PRD #190).
func TestNewPlanAndPriceErrors(t *testing.T) {
	t.Parallel()

	const (
		offeringName = "Example Compute"
		planName     = "EC-2-4"
	)

	tests := []struct {
		name     string
		offering catalog.Offering
		wantErr  error
		// wantMsg lists the substrings the message must hold besides the offering
		// name: the plan name or the list index it is at, and the field.
		wantMsg []string
	}{
		{
			name:     "blank plan name",
			offering: offeringChanged(func(o *catalog.Offering) { o.Plans[0].Name = "" }),
			wantErr:  catalog.ErrInvalidPlan,
			wantMsg:  []string{"plans[0]", "name"},
		},
		{
			name:     "white space plan name",
			offering: offeringChanged(func(o *catalog.Offering) { o.Plans[0].Name = "   " }),
			wantErr:  catalog.ErrInvalidPlan,
			wantMsg:  []string{"plans[0]", "name"},
		},
		{
			name:     "plan name with a control character",
			offering: offeringChanged(func(o *catalog.Offering) { o.Plans[0].Name = "EC\u0000-2-4" }),
			wantErr:  catalog.ErrInvalidPlan,
			wantMsg:  []string{"plans[0]", "name"},
		},
		{
			name:     "vcpus below one",
			offering: offeringChanged(func(o *catalog.Offering) { o.Plans[0].VCPUs = 0 }),
			wantErr:  catalog.ErrInvalidPlan,
			wantMsg:  []string{planName, "vcpus"},
		},
		{
			name:     "vcpus above the cap",
			offering: offeringChanged(func(o *catalog.Offering) { o.Plans[0].VCPUs = catalog.MaxPlanVCPUs + 1 }),
			wantErr:  catalog.ErrInvalidPlan,
			wantMsg:  []string{planName, "vcpus"},
		},
		{
			name:     "memory below one thousandth of a GiB",
			offering: offeringChanged(func(o *catalog.Offering) { o.Plans[0].MemoryMilliGiB = 0 }),
			wantErr:  catalog.ErrInvalidPlan,
			wantMsg:  []string{planName, "memoryGiB"},
		},
		{
			name: "memory above the cap",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.Plans[0].MemoryMilliGiB = catalog.MaxPlanMemoryMilliGiB + 1
			}),
			wantErr: catalog.ErrInvalidPlan,
			wantMsg: []string{planName, "memoryGiB"},
		},
		{
			name:     "included storage below zero",
			offering: offeringChanged(func(o *catalog.Offering) { o.Plans[0].IncludedStorageGiB = -1 }),
			wantErr:  catalog.ErrInvalidPlan,
			wantMsg:  []string{planName, "storageGiB"},
		},
		{
			name: "included storage above the cap",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.Plans[0].IncludedStorageGiB = catalog.MaxIncludedStorageGiB + 1
			}),
			wantErr: catalog.ErrInvalidPlan,
			wantMsg: []string{planName, "storageGiB"},
		},
		{
			name: "duplicate plan name",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.Plans[1].Name = o.Plans[0].Name
			}),
			wantErr: catalog.ErrDuplicatePlan,
			wantMsg: []string{planName},
		},
		{
			// A run of spaces is not a difference: two plan names that collapse to
			// the same text are the same plan, as offering names are compared.
			name: "duplicate plan name differing in inner white space",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.Plans[0].Name = "Example Plan"
				o.Plans[1].Name = "Example  Plan"
			}),
			wantErr: catalog.ErrDuplicatePlan,
			wantMsg: []string{"Example"},
		},
		{
			name:     "plan without a price",
			offering: offeringChanged(func(o *catalog.Offering) { o.Plans[0].Prices = nil }),
			wantErr:  catalog.ErrUnpricedPlan,
			wantMsg:  []string{planName, "price"},
		},
		{
			name:     "plan with an empty price list",
			offering: offeringChanged(func(o *catalog.Offering) { o.Plans[0].Prices = []catalog.Price{} }),
			wantErr:  catalog.ErrUnpricedPlan,
			wantMsg:  []string{planName, "price"},
		},
		{
			name: "negative price amount",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.Plans[0].Prices[0].Amount = -1
			}),
			wantErr: catalog.ErrInvalidPrice,
			wantMsg: []string{planName, "amount"},
		},
		{
			name: "price amount above the maximum",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.Plans[0].Prices[0].Amount = catalog.MaxAmount + 1
			}),
			wantErr: catalog.ErrInvalidPrice,
			wantMsg: []string{planName, "amount"},
		},
		{
			// The currency is compared exactly: no case folding and no trimming.
			name: "currency in lower case",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.Plans[0].Prices[0].Currency = "eur"
			}),
			wantErr: catalog.ErrInvalidPrice,
			wantMsg: []string{planName, "currency", "eur"},
		},
		{
			name: "currency that is not EUR",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.Plans[0].Prices[0].Currency = "USD"
			}),
			wantErr: catalog.ErrInvalidPrice,
			wantMsg: []string{planName, "currency"},
		},
		{
			name:     "empty currency",
			offering: offeringChanged(func(o *catalog.Offering) { o.Plans[0].Prices[0].Currency = "" }),
			wantErr:  catalog.ErrInvalidPrice,
			wantMsg:  []string{planName, "currency"},
		},
		{
			name: "blank price region",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.Plans[0].Prices[0].Regions = []string{"  "}
			}),
			wantErr: catalog.ErrInvalidPrice,
			wantMsg: []string{planName, "regions"},
		},
		{
			name: "price region with a control character",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.Plans[0].Prices[0].Regions = []string{"fr\u0000par"}
			}),
			wantErr: catalog.ErrInvalidPrice,
			wantMsg: []string{planName, "regions"},
		},
		{
			name: "price region the offering is not in",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.Plans[0].Prices[0].Regions = []string{"de-fra"}
			}),
			wantErr: catalog.ErrPriceRegion,
			wantMsg: []string{planName, "de-fra"},
		},
		{
			name: "two prices for the same region",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.Plans[0].Prices = []catalog.Price{
					{Amount: 125000, Currency: catalog.EUR, Regions: []string{"fr-par"}},
					{Amount: 130125, Currency: catalog.EUR, Regions: []string{"fr-par"}},
				}
			}),
			wantErr: catalog.ErrOverlappingPrices,
			wantMsg: []string{planName, "prices[", "fr-par"},
		},
		{
			// A price with no region applies to every region of the offering, so it
			// overlaps any other price of the same list.
			name: "every-region price beside a price for one region",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.Plans[0].Prices = []catalog.Price{
					{Amount: 125000, Currency: catalog.EUR},
					{Amount: 130125, Currency: catalog.EUR, Regions: []string{"fr-par"}},
				}
			}),
			wantErr: catalog.ErrOverlappingPrices,
			wantMsg: []string{planName, "prices["},
		},
		{
			name: "storage price with a negative amount",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.StoragePrices[0].Amount = -100
			}),
			wantErr: catalog.ErrInvalidPrice,
			wantMsg: []string{"storagePrices[0]", "amount"},
		},
		{
			name: "storage price amount above the maximum",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.StoragePrices[0].Amount = catalog.MaxAmount + 1
			}),
			wantErr: catalog.ErrInvalidPrice,
			wantMsg: []string{"storagePrices[0]", "amount"},
		},
		{
			name: "storage price in another currency",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.StoragePrices[0].Currency = "USD"
			}),
			wantErr: catalog.ErrInvalidPrice,
			wantMsg: []string{"storagePrices[0]", "currency"},
		},
		{
			name: "storage price with a blank region",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.StoragePrices[0].Regions = []string{" "}
			}),
			wantErr: catalog.ErrInvalidPrice,
			wantMsg: []string{"storagePrices[0]", "regions"},
		},
		{
			name: "storage price region the offering is not in",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.StoragePrices[0].Regions = []string{"de-fra"}
			}),
			wantErr: catalog.ErrPriceRegion,
			wantMsg: []string{"storagePrices[0]", "de-fra"},
		},
		{
			name: "two storage prices for the same region",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.StoragePrices = []catalog.Price{
					{Amount: 400, Currency: catalog.EUR, Regions: []string{"fr-par"}},
					{Amount: 500, Currency: catalog.EUR, Regions: []string{"fr-par"}},
				}
			}),
			wantErr: catalog.ErrOverlappingPrices,
			wantMsg: []string{"storagePrices[", "fr-par"},
		},
		{
			name: "every-region storage price beside one for a region",
			offering: offeringChanged(func(o *catalog.Offering) {
				o.StoragePrices = []catalog.Price{
					{Amount: 400, Currency: catalog.EUR},
					{Amount: 500, Currency: catalog.EUR, Regions: []string{"nl-ams"}},
				}
			}),
			wantErr: catalog.ErrOverlappingPrices,
			wantMsg: []string{"storagePrices["},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := catalog.New(targetName, validSnapshot(), []catalog.Offering{tc.offering})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("New() error = %v, want one wrapping %v", err, tc.wantErr)
			}

			msg := err.Error()

			if strings.ContainsAny(msg, "\r\n") {
				t.Errorf("New() error = %q, want a single line", msg)
			}

			for _, want := range append([]string{offeringName}, tc.wantMsg...) {
				if !strings.Contains(msg, want) {
					t.Errorf("New() error = %q, want it to name %s", msg, text.Quote(want))
				}
			}
		})
	}
}

// TestNewPlanBillingCapHours pins the optional monthly billing cap of an
// offering's plans (ADR 0029): 0 is the domain's one spelling of "uncapped", a
// cap from 1 to MaxBillingCapHours is kept as given, and a negative cap or one
// above the maximum is refused with ErrInvalidBillingCap in one line naming the
// offering and the field. A cap above 744 would never be reached, so it is
// refused rather than read as uncapped.
func TestNewPlanBillingCapHours(t *testing.T) {
	t.Parallel()

	const offeringName = "Example Compute Plans"

	capped := func(hours int64) catalog.Offering {
		return catalog.Offering{
			Name:                offeringName,
			Categories:          []taxonomy.Category{taxonomy.ComputeVM},
			Regions:             []string{"fr-par"},
			PlanBillingCapHours: hours,
		}
	}

	tests := []struct {
		name    string
		hours   int64
		wantErr error
	}{
		{name: "uncapped", hours: 0},
		{name: "one hour", hours: 1},
		{name: "672 hours, the hours of a 28-day month", hours: 672},
		{name: "744 hours, the largest a month has", hours: catalog.MaxBillingCapHours},
		{name: "negative cap", hours: -1, wantErr: catalog.ErrInvalidBillingCap},
		{name: "cap above the maximum", hours: catalog.MaxBillingCapHours + 1, wantErr: catalog.ErrInvalidBillingCap},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cat, err := catalog.New(targetName, validSnapshot(), []catalog.Offering{capped(tc.hours)})

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("New() with a plan billing cap of %d returned unexpected error: %v", tc.hours, err)
				}

				if got := cat.Offerings()[0].PlanBillingCapHours; got != tc.hours {
					t.Errorf("Offerings()[0].PlanBillingCapHours = %d, want %d", got, tc.hours)
				}

				return
			}

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("New() with a plan billing cap of %d returned error %v, want one wrapping %v",
					tc.hours, err, tc.wantErr)
			}

			msg := err.Error()

			if strings.ContainsAny(msg, "\r\n") {
				t.Errorf("New() error = %q, want a single line", msg)
			}

			for _, want := range []string{offeringName, "planBillingCapHours"} {
				if !strings.Contains(msg, want) {
					t.Errorf("New() error = %q, want it to name %s", msg, text.Quote(want))
				}
			}
		})
	}
}

// fixedFeeOffering is a Kubernetes offering whose fixed fees cover both its
// regions: the control plane is charged in fr-par and included in nl-ams, so a
// case can break one fee and leave the coverage valid (ADR 0026).
func fixedFeeOffering() catalog.Offering {
	return catalog.Offering{
		Name:       "Example Kubernetes",
		Categories: []taxonomy.Category{taxonomy.KubernetesManaged},
		Regions:    []string{"fr-par", "nl-ams"},
		FixedFees: []catalog.Price{
			{Amount: 657000, Currency: catalog.EUR, Regions: []string{"fr-par"}},
			{Amount: 0, Currency: catalog.EUR, Regions: []string{"nl-ams"}},
		},
	}
}

// fixedFeeOfferingChanged returns fixedFeeOffering with mutate applied, so a
// case breaks exactly one fee or the coverage and leaves the rest valid.
func fixedFeeOfferingChanged(mutate func(*catalog.Offering)) catalog.Offering {
	o := fixedFeeOffering()
	mutate(&o)

	return o
}

// TestNewFixedFeeErrors pins every rule New holds for an offering's fixed fees:
// the sentinel the error wraps, and that the message names the offering, the
// fee list and the region at fault. A fee list is a price list with one rule of
// its own: it must cover every region of the offering exactly once, because a
// region left without a fee would read as a free control plane (ADR 0026, PRD
// #190).
func TestNewFixedFeeErrors(t *testing.T) {
	t.Parallel()

	const offeringName = "Example Kubernetes"

	tests := []struct {
		name     string
		offering catalog.Offering
		wantErr  error
		// wantMsg lists the substrings the message must hold besides the offering
		// name: the fee list index, the field, or the region left without a fee.
		wantMsg []string
	}{
		{
			name: "negative fee amount",
			offering: fixedFeeOfferingChanged(func(o *catalog.Offering) {
				o.FixedFees[0].Amount = -1
			}),
			wantErr: catalog.ErrInvalidPrice,
			wantMsg: []string{"fixedFees[0]", "amount"},
		},
		{
			name: "fee amount above the maximum",
			offering: fixedFeeOfferingChanged(func(o *catalog.Offering) {
				o.FixedFees[0].Amount = catalog.MaxAmount + 1
			}),
			wantErr: catalog.ErrInvalidPrice,
			wantMsg: []string{"fixedFees[0]", "amount"},
		},
		{
			// The currency is compared exactly: no case folding and no trimming.
			name: "fee in a currency that is not EUR",
			offering: fixedFeeOfferingChanged(func(o *catalog.Offering) {
				o.FixedFees[1].Currency = "eur"
			}),
			wantErr: catalog.ErrInvalidPrice,
			wantMsg: []string{"fixedFees[1]", "currency", "eur"},
		},
		{
			name: "fee with a blank region",
			offering: fixedFeeOfferingChanged(func(o *catalog.Offering) {
				o.FixedFees[0].Regions = []string{" "}
			}),
			wantErr: catalog.ErrInvalidPrice,
			wantMsg: []string{"fixedFees[0]", "regions"},
		},
		{
			name: "fee region the offering is not in",
			offering: fixedFeeOfferingChanged(func(o *catalog.Offering) {
				o.FixedFees[0].Regions = []string{"de-fra"}
			}),
			wantErr: catalog.ErrPriceRegion,
			wantMsg: []string{"fixedFees[0]", "de-fra"},
		},
		{
			name: "two fees for the same region",
			offering: fixedFeeOfferingChanged(func(o *catalog.Offering) {
				o.FixedFees[1].Regions = []string{"fr-par"}
			}),
			wantErr: catalog.ErrOverlappingPrices,
			wantMsg: []string{"fixedFees[1]", "fr-par"},
		},
		{
			// A fee with no region applies to every region of the offering, so it
			// overlaps any other fee of the same list.
			name: "every-region fee beside a fee for one region",
			offering: fixedFeeOfferingChanged(func(o *catalog.Offering) {
				o.FixedFees = []catalog.Price{
					{Amount: 657000, Currency: catalog.EUR},
					{Amount: 0, Currency: catalog.EUR, Regions: []string{"nl-ams"}},
				}
			}),
			wantErr: catalog.ErrOverlappingPrices,
			wantMsg: []string{"fixedFees[1]"},
		},
		{
			// Every region of the offering needs a fee: the region left out here
			// would estimate as a free control plane (ADR 0026).
			name: "offering region with no fee",
			offering: fixedFeeOfferingChanged(func(o *catalog.Offering) {
				o.FixedFees = []catalog.Price{
					{Amount: 657000, Currency: catalog.EUR, Regions: []string{"fr-par"}},
				}
			}),
			wantErr: catalog.ErrMissingFee,
			wantMsg: []string{"fixedFees", "nl-ams"},
		},
		{
			// When several regions lack a fee, the error names the first in sorted
			// order, so it is the same on every run.
			name: "several regions with no fee",
			offering: fixedFeeOfferingChanged(func(o *catalog.Offering) {
				o.Regions = []string{"fr-par", "nl-ams", "pl-waw"}
				o.FixedFees = []catalog.Price{
					{Amount: 657000, Currency: catalog.EUR, Regions: []string{"pl-waw"}},
				}
			}),
			wantErr: catalog.ErrMissingFee,
			wantMsg: []string{"fixedFees", "fr-par"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := catalog.New(targetName, validSnapshot(), []catalog.Offering{tc.offering})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("New() error = %v, want one wrapping %v", err, tc.wantErr)
			}

			msg := err.Error()

			if strings.ContainsAny(msg, "\r\n") {
				t.Errorf("New() error = %q, want a single line", msg)
			}

			for _, want := range append([]string{offeringName}, tc.wantMsg...) {
				if !strings.Contains(msg, want) {
					t.Errorf("New() error = %q, want it to name %s", msg, text.Quote(want))
				}
			}
		})
	}
}

// New refuses a snapshot without a usable default region: every estimate falls
// back to it when the user names no region, so a blank one would estimate in no
// region at all. New does not check that it is a region of any offering: the
// declared regions are not in the domain, and a catalog with no offering still
// has a default region (ADR 0024).
func TestNewDefaultRegion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		region  string
		wantErr error
	}{
		{name: "a declared region", region: "fr-par"},
		{name: "a region no offering is available in", region: "de-fra"},
		{name: "empty", wantErr: catalog.ErrInvalidCatalog},
		{name: "only white space", region: "   ", wantErr: catalog.ErrInvalidCatalog},
		{name: "a control character", region: "fr\u0000par", wantErr: catalog.ErrInvalidCatalog},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			snapshot := validSnapshot()
			snapshot.DefaultRegion = tc.region

			cat, err := catalog.New(targetName, snapshot, validOfferings())

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("New() with default region %s error = %v, want one wrapping %v",
						text.Quote(tc.region), err, tc.wantErr)
				}

				if !strings.Contains(err.Error(), "default region") {
					t.Errorf("New() error = %q, want it to name the default region", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("New() with default region %s returned unexpected error: %v", text.Quote(tc.region), err)
			}

			if got := cat.Snapshot().DefaultRegion; got != tc.region {
				t.Errorf("Snapshot().DefaultRegion = %s, want %s as given",
					text.Quote(got), text.Quote(tc.region))
			}
		})
	}
}

// tamperPricedOffering breaks every plan, price, storage price and fixed fee
// reachable through off, so a catalog that hands out a list it shares with its
// input or its result shows the change. It changes each region in place and
// never replaces a region slice: a slice the catalog shares but a test replaces
// is never written to, so a shallow copy would go unnoticed, and a storage
// price's or a fixed fee's regions deserve the same proof as a plan price's
// (ADR 0024, ADR 0026). It walks the lists instead of indexing a plan: the plans
// come back sorted by name.
func tamperPricedOffering(t *testing.T, off *catalog.Offering) {
	t.Helper()

	if len(off.Plans) == 0 {
		t.Fatal("the offering sells no plan to tamper with")
	}

	for i := range off.Plans {
		off.Plans[i].Name = "Tampered"

		for j := range off.Plans[i].Prices {
			off.Plans[i].Prices[j].Amount = 1

			for k := range off.Plans[i].Prices[j].Regions {
				off.Plans[i].Prices[j].Regions[k] = "tampered"
			}
		}
	}

	if len(off.StoragePrices) == 0 {
		t.Fatal("the offering publishes no storage price to tamper with")
	}

	regions := 0

	for i := range off.StoragePrices {
		off.StoragePrices[i].Amount = 1

		for k := range off.StoragePrices[i].Regions {
			off.StoragePrices[i].Regions[k] = "tampered"

			regions++
		}
	}

	// A fixture whose storage prices are all every-region prices would leave the
	// loop above with nothing to change, so the test would pin nothing.
	if regions == 0 {
		t.Fatal("the offering's storage prices hold no region to tamper with")
	}

	if len(off.FixedFees) == 0 {
		t.Fatal("the offering charges no fixed fee to tamper with")
	}

	feeRegions := 0

	for i := range off.FixedFees {
		off.FixedFees[i].Amount = 1

		for k := range off.FixedFees[i].Regions {
			off.FixedFees[i].Regions[k] = "tampered"

			feeRegions++
		}
	}

	if feeRegions == 0 {
		t.Fatal("the offering's fixed fees hold no region to tamper with")
	}
}

// The catalog is immutable: mutating the slices handed to New, or what Offerings
// and OfferingsFor return, must not reach the catalog. Plans, their prices, a
// price's regions and an offering's fixed fees are part of that copy (ADR 0024,
// ADR 0026).
func TestPlansAndPricesAreCopied(t *testing.T) {
	t.Parallel()

	want := pricedOfferingNormalised()

	check := func(t *testing.T, cat catalog.Catalog) {
		t.Helper()

		got := cat.Offerings()
		if len(got) != 1 {
			t.Fatalf("Offerings() returned %d offerings, want 1", len(got))
		}

		if diff := cmp.Diff(want.Plans, got[0].Plans); diff != "" {
			t.Errorf("Plans changed (-want +got):\n%s", diff)
		}

		if diff := cmp.Diff(want.StoragePrices, got[0].StoragePrices); diff != "" {
			t.Errorf("StoragePrices changed (-want +got):\n%s", diff)
		}

		if diff := cmp.Diff(want.FixedFees, got[0].FixedFees); diff != "" {
			t.Errorf("FixedFees changed (-want +got):\n%s", diff)
		}
	}

	t.Run("input slices", func(t *testing.T) {
		t.Parallel()

		offerings := []catalog.Offering{pricedOffering()}
		cat := newCatalog(t, targetName, validSnapshot(), offerings)

		tamperPricedOffering(t, &offerings[0])

		check(t, cat)
	})

	t.Run("Offerings result", func(t *testing.T) {
		t.Parallel()

		cat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{pricedOffering()})

		got := cat.Offerings()
		if len(got) != 1 {
			t.Fatalf("Offerings() returned %d offerings, want 1", len(got))
		}

		tamperPricedOffering(t, &got[0])

		check(t, cat)
	})

	t.Run("OfferingsFor result", func(t *testing.T) {
		t.Parallel()

		cat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{pricedOffering()})

		got := cat.OfferingsFor(taxonomy.ComputeVM)
		if len(got) == 0 {
			t.Fatal("OfferingsFor(compute.vm) is empty")
		}

		tamperPricedOffering(t, &got[0])

		check(t, cat)
	})
}
