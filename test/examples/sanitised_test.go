package examples_test

import (
	"net/netip"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
)

const (
	// dummyAccountID is the AWS account ID every sample uses (CLAUDE.md
	// rule 2): 123456789012 is a documented example value, never a real one.
	dummyAccountID = "123456789012"

	// dummyHyphenatedAccountID is the same dummy account written the way AWS
	// documentation often writes it, 1234-5678-9012: an account ID in that
	// shape is checked too, so a sample cannot document a real account that
	// way.
	dummyHyphenatedAccountID = "1234-5678-9012"

	// dummyGUID is the all-zero identifier every sample uses for an Azure
	// subscription or tenant ID: a GUID no subscription can own.
	dummyGUID = "00000000-0000-0000-0000-000000000000"

	// documentationPrefix is the RFC 5737 documentation range every sample
	// address is in. Names use example.com (RFC 2606) or the reserved .example
	// TLD.
	documentationPrefix = "192.0.2.0/24"

	// documentationPrefixV6 is the RFC 3849 documentation prefix (2001:db8::/32)
	// every sample IPv6 address must be in.
	documentationPrefixV6 = "2001:db8::/32"

	// providerAPIDomain is the domain a target's public API host names sit under
	// (compute.googleapis.com). A GCP inventory names a resource by its Cloud
	// Asset Inventory asset type, and an asset type starts with the service's
	// API host name; those host names identify no customer.
	providerAPIDomain = "googleapis.com"
)

// The sanitisation patterns. They are deliberately simple: a sample is dummy
// data, and each pattern catches the shape of a value that could identify a
// real account.
var (
	// accountIDPattern finds every run of 12 digits, the length of an AWS
	// account ID. Only a run that stands alone is an account ID (standsAlone):
	// a run inside a longer number, a letter sequence or a hyphenated identifier
	// such as an Azure subscription GUID's last group is something else.
	accountIDPattern = regexp.MustCompile(`[0-9]{12}`)

	// hyphenatedAccountIDPattern finds a hyphenated account ID, the shape AWS
	// documentation writes 123456789012 in as 1234-5678-9012. Only a run that
	// stands alone is an account ID (standsAlone), so the digit groups of a
	// GUID or of a longer number are not.
	hyphenatedAccountIDPattern = regexp.MustCompile(`[0-9]{4}-[0-9]{4}-[0-9]{4}`)

	// guidPattern finds an Azure subscription or tenant ID: eight, four, four,
	// four and twelve hexadecimal digits, the shape of a GUID, in either case.
	guidPattern = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

	// ipv4Pattern finds every dotted-quad candidate for an IPv4 address: four
	// runs of 1 to 3 digits. Each is parsed, so "1.2.3.400" is reported as
	// malformed rather than taken for an address.
	ipv4Pattern = regexp.MustCompile(`\b[0-9]{1,3}(?:\.[0-9]{1,3}){3}\b`)

	// ipv6Pattern finds every candidate IPv6 address: two or more colon
	// separated groups of up to four hexadecimal digits, so a compressed
	// address (2001:db8::1, ::1) is matched whole. The shape is loose on
	// purpose - a clock time (12:30:45) matches it, and so does the double colon
	// of a cloud resource type (AWS::EC2::Instance) - so each candidate must
	// stand alone (ipv6CandidateStandsAlone) and is then parsed with net/netip;
	// only a candidate that is a real address is checked (ipv6Problems).
	ipv6Pattern = regexp.MustCompile(`(?i)(?:[0-9a-f]{0,4}:){2,}[0-9a-f]{0,4}`)

	// hostnamePattern finds every dot-separated name, the shape of a DNS name:
	// one or more labels, each starting and ending with a letter or digit,
	// separated by dots. The match is case-insensitive (?i) so a name written
	// in capitals cannot slip past the allow list (SHOP.ACME.IO). Not every
	// match is a host name: a decimal number (84.10), the taxonomy's vocabulary
	// (compute.vm, database.postgresql), an IPv4 address (checked by
	// ipv4Problems) and an inventory field path (size.storageGiB,
	// usage.hoursPerMonth) match it too, and are skipped in hostnameProblems.
	hostnamePattern = regexp.MustCompile(`(?i)\b[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+\b`)

	// documentationPrefixNet and documentationPrefixV6Net are the documentation
	// ranges, parsed once.
	documentationPrefixNet   = netip.MustParsePrefix(documentationPrefix)
	documentationPrefixV6Net = netip.MustParsePrefix(documentationPrefixV6)
)

// allowedHostnames are the documentation domain names a sample may carry,
// matched case-insensitively against the lowercased candidate. A name under one
// of them (shop.example.com) inherits the entry that starts with a dot.
var allowedHostnames = []string{
	"example.com",
	".example.com",
	".example",
}

// inventoryFieldPaths are the resource fields inventory format version 2 writes
// with a dot (docs/adr/0025). A document and a README name them as they are
// (size.vcpus, usage.hoursPerMonth), and hostnamePattern matches a path written
// under resources[N] from the field name on, because the "[" before the index
// and the "]" after it end the word. An inventory field path is not a host name.
var inventoryFieldPaths = []string{
	"size.vcpus",
	"size.memoryGiB",
	"size.storageGiB",
	"size.count",
	"usage.hoursPerMonth",
	"monthlyCost.amount",
	"monthlyCost.currency",
}

// allowedHostnamesHint is how a failure message spells the host name rule.
var allowedHostnamesHint = strings.Join(allowedHostnames, ", ") +
	", or a service host under " + providerAPIDomain

// allowedResourceTypeNamespaces are the Azure Resource Manager resource
// provider namespaces a sample names a resource by (Microsoft.Compute/
// virtualMachines). They are public provider vocabulary, identical in every
// subscription, so they identify no customer, and an Azure rawType is written
// with one. They are matched with their exact case on purpose: Azure documents
// them capitalised (Microsoft.Compute), while a lowercase name of the same
// shape (microsoft.com) is the real public domain and must fail in a sample
// (a fixture is checked after maskAzureNamespaces, as the AI reviewer's scan
// reads a line).
var allowedResourceTypeNamespaces = []string{
	"Microsoft.Cdn",
	"Microsoft.Compute",
	"Microsoft.ContainerService",
	"Microsoft.DBforMySQL",
	"Microsoft.DBforPostgreSQL",
	"Microsoft.HDInsight",
	"Microsoft.Network",
	"Microsoft.ServiceBus",
	"Microsoft.Storage",
	"Microsoft.Web",
}

// problem is one value a sample may not carry: the 1-based line it is on, the
// offending text itself and the rule it breaks. A message names the file, the
// line and the value, so whoever reads the failure knows what to change.
type problem struct {
	line  int
	value string
	rule  string
}

// TestSamplesAreSanitised pins CLAUDE.md rule 2 for the samples: no sample may
// carry real identifiers, so every account ID (123456789012 or 1234-5678-9012)
// is the dummy one, every Azure subscription or tenant ID is
// 00000000-0000-0000-0000-000000000000, every IPv4 address is in
// 192.0.2.0/24, every IPv6 address is in 2001:db8::/32, and every domain name is
// a documentation name (example.com or .example) or a provider's public API host
// name (allowedHostname). Every file under examples/ is read, the READMEs as
// well as the inventories. A person copies these files into their own demo, so
// they read as documentation values only.
func TestSamplesAreSanitised(t *testing.T) {
	t.Parallel()

	for _, file := range allFiles(t) {
		t.Run(file.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(file.path)
			if err != nil {
				t.Fatalf("read %s: %v", file.path, err)
			}

			text := string(data)

			reportProblems(t, file.name, accountIDProblems(text))
			reportProblems(t, file.name, guidProblems(text))
			reportProblems(t, file.name, ipv4Problems(text))
			reportProblems(t, file.name, ipv6Problems(text))
			reportProblems(t, file.name, hostnameProblems(text))
		})
	}
}

// reportProblems fails t once per problem, naming the file, the line and the
// offending value.
func reportProblems(t *testing.T, name string, problems []problem) {
	t.Helper()

	for _, p := range problems {
		t.Errorf("%s:%d: %q %s", name, p.line, p.value, p.rule)
	}
}

// values returns the offending values of problems, in order, so a table test
// can compare them.
func values(problems []problem) []string {
	if len(problems) == 0 {
		return nil
	}

	got := make([]string, len(problems))
	for i, p := range problems {
		got[i] = p.value
	}

	return got
}

// accountIDProblems returns a problem for every 12-digit account ID, GCP
// project number or hyphenated account ID that is not the dummy one.
func accountIDProblems(text string) []problem {
	var problems []problem

	for _, match := range accountIDPattern.FindAllStringIndex(text, -1) {
		if !standsAlone(text, match[0], match[1]) {
			continue
		}

		if got := text[match[0]:match[1]]; got != dummyAccountID {
			problems = append(problems, problem{
				line:  lineOf(text, match[0]),
				value: got,
				rule:  "is not the dummy account ID " + dummyAccountID + " (an AWS account ID or GCP project number, CLAUDE.md rule 2)",
			})
		}
	}

	for _, match := range hyphenatedAccountIDPattern.FindAllStringIndex(text, -1) {
		if !standsAlone(text, match[0], match[1]) {
			continue
		}

		if got := text[match[0]:match[1]]; got != dummyHyphenatedAccountID {
			problems = append(problems, problem{
				line:  lineOf(text, match[0]),
				value: got,
				rule:  "is not the dummy account ID " + dummyHyphenatedAccountID + " (an AWS account ID written with hyphens, CLAUDE.md rule 2)",
			})
		}
	}

	return problems
}

// standsAlone reports whether text[start:end] is a whole identifier rather than
// a part of a longer one: neither neighbour may be a digit, a letter or a
// hyphen.
func standsAlone(text string, start, end int) bool {
	return !identifierByte(text, start-1) && !identifierByte(text, end)
}

// identifierByte reports whether the byte at i, when there is one, is part of
// an identifier: a digit, an ASCII letter or a hyphen.
func identifierByte(text string, i int) bool {
	if i < 0 || i >= len(text) {
		return false
	}

	c := text[i]

	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-'
}

// lineOf returns the 1-based line of the byte at i, so a problem points at the
// line a reader must fix.
func lineOf(text string, i int) int {
	return strings.Count(text[:i], "\n") + 1
}

// guidProblems returns a problem for every GUID that is not the dummy one. A
// sample names an Azure subscription or tenant ID with a GUID, and only the
// all-zero one identifies nothing.
func guidProblems(text string) []problem {
	var problems []problem

	for _, match := range guidPattern.FindAllStringIndex(text, -1) {
		if !standsAlone(text, match[0], match[1]) {
			continue
		}

		if got := text[match[0]:match[1]]; got != dummyGUID {
			problems = append(problems, problem{
				line:  lineOf(text, match[0]),
				value: got,
				rule:  "is not the dummy Azure subscription or tenant ID " + dummyGUID + " (CLAUDE.md rule 2)",
			})
		}
	}

	return problems
}

// ipv4Problems returns a problem for every IPv4 address outside the
// documentation range, and for a dotted quad that is not an address at all.
func ipv4Problems(text string) []problem {
	var problems []problem

	for _, match := range ipv4Pattern.FindAllStringIndex(text, -1) {
		candidate := text[match[0]:match[1]]

		addr, err := netip.ParseAddr(candidate)
		if err != nil || !addr.Is4() {
			problems = append(problems, problem{
				line:  lineOf(text, match[0]),
				value: candidate,
				rule:  "looks like an IPv4 address but is not one; samples use addresses in " + documentationPrefix,
			})

			continue
		}

		if !documentationPrefixNet.Contains(addr) {
			problems = append(problems, problem{
				line:  lineOf(text, match[0]),
				value: candidate,
				rule:  "is outside the documentation range " + documentationPrefix + " (CLAUDE.md rule 2)",
			})
		}
	}

	return problems
}

// ipv6Problems returns a problem for every IPv6 address outside the
// documentation prefix. A candidate the pattern catches that is not an address
// (a clock time, say) is not a problem.
func ipv6Problems(text string) []problem {
	var problems []problem

	for _, match := range ipv6Pattern.FindAllStringIndex(text, -1) {
		if !ipv6CandidateStandsAlone(text, match[0], match[1]) {
			continue
		}

		candidate := text[match[0]:match[1]]

		addr, err := netip.ParseAddr(candidate)
		if err != nil {
			continue
		}

		if !documentationPrefixV6Net.Contains(addr) {
			problems = append(problems, problem{
				line:  lineOf(text, match[0]),
				value: candidate,
				rule:  "is outside the documentation prefix " + documentationPrefixV6 + " (CLAUDE.md rule 2)",
			})
		}
	}

	return problems
}

// ipv6CandidateStandsAlone reports whether text[start:end] is a whole address
// rather than a part of a longer word: no neighbour may be a letter, a digit or
// a colon. The colon excludes the resource type names an inventory carries
// (AWS::EC2::Instance, AWS::S3::Bucket), which are not addresses.
func ipv6CandidateStandsAlone(text string, start, end int) bool {
	return !wordOrColon(text, start-1) && !wordOrColon(text, end)
}

// wordOrColon reports whether the byte at i, when there is one, is part of a
// word that could extend an address: a letter, a digit or a colon.
func wordOrColon(text string, i int) bool {
	if i < 0 || i >= len(text) {
		return false
	}

	c := text[i]

	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == ':'
}

// hostnameProblems returns a problem for every domain name a sample may not
// carry (allowedHostname).
func hostnameProblems(text string) []problem {
	var problems []problem

	for _, match := range hostnamePattern.FindAllStringIndex(text, -1) {
		candidate := text[match[0]:match[1]]

		if _, err := netip.ParseAddr(candidate); err == nil {
			// An IPv4 address, checked by ipv4Problems.
			continue
		}

		// A number with a decimal point (a monthlyCost.amount of 84.10) has the
		// shape of a two-label name and is not data about a host.
		if numeric(candidate) {
			continue
		}

		// An inventory field path is the format's own vocabulary, not a value.
		if slices.Contains(inventoryFieldPaths, candidate) {
			continue
		}

		// The taxonomy vocabulary is dotted as well, and an inventory carries
		// it in every resource: a value the taxonomy declares is not a domain
		// name.
		if _, err := taxonomy.ParseCategory(candidate); err == nil {
			continue
		}

		if _, err := taxonomy.ParseCapability(candidate); err == nil {
			continue
		}

		if allowedHostname(candidate) {
			continue
		}

		if insidePath(text, match[0]) {
			continue
		}

		problems = append(problems, problem{
			line:  lineOf(text, match[0]),
			value: candidate,
			rule:  "looks like a domain name but is not a documentation name (" + allowedHostnamesHint + ") (CLAUDE.md rule 2)",
		})
	}

	return problems
}

// numeric reports whether candidate is a number rather than a host name: every
// label is all digits, as in a number written as a plain decimal (84.10,
// 1234.5). A host name holds a letter in at least one label, so a host name
// followed by a digit label (shop.acme.io.1) is still read as a host name.
// Dotted quads are all digits as well; ipv4Problems checks those.
func numeric(candidate string) bool {
	for label := range strings.SplitSeq(candidate, ".") {
		if label == "" {
			return false
		}

		for i := range len(label) {
			if label[i] < '0' || label[i] > '9' {
				return false
			}
		}
	}

	return true
}

// insidePath reports whether the candidate starting at start is a segment of a
// path rather than a host name: it directly follows a slash that is not the
// authority marker of a URL. A URL's host follows "//" (https://shop.acme.io,
// //shop.acme.io) or, in a scheme written with a single slash, ":/"
// (http:/shop.acme.io, issue #610), so it is checked; the file names a README's
// link target or command carries (../docs/guide/try-it.md,
// examples/aws/inventory.json) follow a path separator and are not domain names.
func insidePath(text string, start int) bool {
	if start == 0 || text[start-1] != '/' {
		return false
	}

	return !strings.HasSuffix(text[:start], "//") && !strings.HasSuffix(text[:start], ":/")
}

// allowedHostname reports whether candidate, a name shaped like a domain name,
// is one a sample may carry: a documentation domain, an Azure resource type
// namespace, or a provider's public API host name. The comparison is
// case-insensitive, so an allowed name written in capitals passes and any other
// name written in capitals (SHOP.ACME.IO) fails.
func allowedHostname(candidate string) bool {
	if slices.Contains(allowedResourceTypeNamespaces, candidate) {
		return true
	}

	lower := strings.ToLower(candidate)

	for _, allowed := range allowedHostnames {
		if lower == strings.TrimPrefix(allowed, ".") ||
			strings.HasPrefix(allowed, ".") && strings.HasSuffix(lower, allowed) {
			return true
		}
	}

	return providerHostname(lower)
}

// providerHostname reports whether candidate, already lowercased, is a
// provider's public API host name: the domain itself or exactly one label
// before it (compute.googleapis.com, the service host of an asset type). A name
// with more labels before it (real-bucket.storage.googleapis.com) names a
// customer's bucket, so a sample may not carry it.
func providerHostname(candidate string) bool {
	label, ok := strings.CutSuffix(candidate, "."+providerAPIDomain)
	if !ok {
		return candidate == providerAPIDomain
	}

	return label != "" && !strings.Contains(label, ".")
}

// TestAllowedHostname pins which names the sanitisation check lets through: the
// documentation domains, the Azure resource type namespaces a rawType is built
// from (Microsoft.Compute/virtualMachines) and the provider API host names a
// GCP asset type is built from (compute.googleapis.com/Instance), and nothing
// else. The rule is a deny list narrowed by an allow list, so a real customer
// domain, a lookalike of an allowed one and any other provider-looking host
// still fails, whichever case it is written in.
func TestAllowedHostname(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		candidate string
		want      bool
	}{
		{name: "documentation domain", candidate: "example.com", want: true},
		{name: "name under the documentation domain", candidate: "shop.example.com", want: true},
		{name: "reserved documentation TLD", candidate: "shop.example", want: true},
		{name: "documentation domain in capitals", candidate: "SHOP.EXAMPLE.COM", want: true},
		{name: "provider API domain", candidate: "googleapis.com", want: true},
		{name: "GCP compute asset type host", candidate: "compute.googleapis.com", want: true},
		{name: "GCP SQL asset type host", candidate: "sqladmin.googleapis.com", want: true},
		{name: "Azure resource type namespace", candidate: "Microsoft.Compute", want: true},
		{name: "real customer domain", candidate: "shop.acme.io", want: false},
		{name: "real customer domain in capitals", candidate: "SHOP.ACME.IO", want: false},
		{name: "bucket host below the provider API domain", candidate: "real-bucket.storage.googleapis.com", want: false},
		{name: "lookalike of an allowed domain", candidate: "notgoogleapis.com", want: false},
		{name: "allowed domain as a prefix", candidate: "googleapis.com.acme.io", want: false},
		{name: "other Google host name", candidate: "storage.cloud.google.com", want: false},
		{name: "Azure resource type namespace in lowercase", candidate: "microsoft.compute", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := allowedHostname(tc.candidate); got != tc.want {
				t.Errorf("allowedHostname(%q) = %v, want %v", tc.candidate, got, tc.want)
			}
		})
	}
}

// TestAccountIDProblems pins which account IDs the sanitisation check rejects:
// a 12-digit account ID, a GCP project number and a hyphenated account ID must
// be the dummy value, while a run of digits that is part of a longer identifier
// (the last group of a GUID) is not an account ID at all.
func TestAccountIDProblems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "no number", text: "shop-web-1 in eu-central-1", want: nil},
		{name: "dummy account ID", text: `"accountId": "123456789012"`, want: nil},
		{name: "real account ID", text: `"accountId": "123456789013"`, want: []string{"123456789013"}},
		{name: "dummy hyphenated account ID", text: "account 1234-5678-9012", want: nil},
		{name: "real hyphenated account ID", text: "account 1111-2222-3333", want: []string{"1111-2222-3333"}},
		{name: "digits at the end of a GUID", text: "8f2c4b1e-9a3d-4f6e-8b7a-987654321098", want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, values(accountIDProblems(tc.text))); diff != "" {
				t.Errorf("accountIDProblems(%q) mismatch (-want +got):\n%s", tc.text, diff)
			}
		})
	}
}

// TestGUIDProblems pins which Azure subscription and tenant IDs the
// sanitisation check rejects: any GUID must be the all-zero dummy one, whatever
// its case.
func TestGUIDProblems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "no GUID", text: `"source": "azure"`, want: nil},
		{name: "dummy subscription ID", text: "subscriptions/" + dummyGUID, want: nil},
		{
			name: "real subscription ID",
			text: "/subscriptions/8f2c4b1e-9a3d-4f6e-8b7a-1c2d3e4f5a6b/resourceGroups/shop",
			want: []string{"8f2c4b1e-9a3d-4f6e-8b7a-1c2d3e4f5a6b"},
		},
		{
			name: "real tenant ID in capitals",
			text: "tenant 8F2C4B1E-9A3D-4F6E-8B7A-1C2D3E4F5A6B",
			want: []string{"8F2C4B1E-9A3D-4F6E-8B7A-1C2D3E4F5A6B"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, values(guidProblems(tc.text))); diff != "" {
				t.Errorf("guidProblems(%q) mismatch (-want +got):\n%s", tc.text, diff)
			}
		})
	}
}

// TestIPv6Problems pins which IPv6 addresses the sanitisation check rejects:
// an address in the RFC 3849 documentation prefix passes, any other address
// fails, and a colon-separated value that is not an address (a clock time) is
// not a problem.
func TestIPv6Problems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "no address", text: "eu-central-1, ipv6", want: nil},
		{name: "documentation address", text: `"address": "2001:db8::1"`, want: nil},
		{name: "documentation prefix", text: "2001:db8::/32", want: nil},
		{name: "documentation address in capitals", text: `"address": "2001:DB8::1"`, want: nil},
		{name: "global address", text: `"address": "2a00:1450::1"`, want: []string{"2a00:1450::1"}},
		{name: "clock time", text: "started at 12:30:45 and finished", want: nil},
		{name: "AWS compute resource type", text: `"rawType": "AWS::EC2::Instance"`, want: nil},
		{name: "AWS storage resource type", text: `"rawType": "AWS::S3::Bucket"`, want: nil},
		{
			name: "global address after another one",
			text: "2001:db8::1 2a00:1450::1",
			want: []string{"2a00:1450::1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, values(ipv6Problems(tc.text))); diff != "" {
				t.Errorf("ipv6Problems(%q) mismatch (-want +got):\n%s", tc.text, diff)
			}
		})
	}
}

// TestIPv4Problems pins which addresses the sanitisation check reports, now
// that hostnameProblems leaves every all-digit dotted name to it: the
// documentation range 192.0.2.0/24 the samples use is allowed, and so is a
// prefix in it, while a private, a public or a malformed address is reported.
// The two other RFC 5737 ranges (198.51.100.0/24 and 203.0.113.0/24) are
// reported on purpose: the samples all use one range, so a second one in a
// sample is a value somebody copied from elsewhere.
func TestIPv4Problems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "no address", text: "shop-web-1 in eu-central-1", want: nil},
		{name: "documentation address", text: `"address": "192.0.2.10"`, want: nil},
		{name: "documentation address in prose", text: "the shop answers on 192.0.2.10", want: nil},
		{name: "documentation prefix", text: `"range": "192.0.2.0/24"`, want: nil},
		{name: "private address", text: `"address": "10.0.0.5"`, want: []string{"10.0.0.5"}},
		{name: "private address with a prefix length", text: `"range": "10.0.0.5/16"`, want: []string{"10.0.0.5"}},
		{name: "other documentation address", text: `"address": "198.51.100.7"`, want: []string{"198.51.100.7"}},
		{name: "third documentation address", text: `"address": "203.0.113.7"`, want: []string{"203.0.113.7"}},
		{name: "dotted quad that is not an address", text: `"address": "1.2.3.400"`, want: []string{"1.2.3.400"}},
		{
			name: "allowed and reported address beside each other",
			text: "192.0.2.10 forwards to 10.0.0.5",
			want: []string{"10.0.0.5"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, values(ipv4Problems(tc.text))); diff != "" {
				t.Errorf("ipv4Problems(%q) mismatch (-want +got):\n%s", tc.text, diff)
			}
		})
	}
}

// TestHostnameProblems pins which names the sanitisation check reports, not only
// through allowedHostname: a name in a command path, a markdown link target or
// an inventory field path is not a host, a decimal number is not one either,
// while a real host in prose or in a URL is reported - even next to a number or
// a field path that is not.
func TestHostnameProblems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "no host", text: "shop-web-1 in eu-central-1", want: nil},
		{name: "documentation domain", text: `"id": "shop.example.com"`, want: nil},
		{name: "taxonomy category", text: `"category": "database.postgresql"`, want: nil},
		{name: "IPv4 address", text: `"range": "192.0.2.0/24"`, want: nil},
		{name: "GCP asset type", text: "`compute.googleapis.com/Instance`", want: nil},
		{name: "Azure resource type", text: "`Microsoft.Compute/virtualMachines`", want: nil},
		{name: "file name in a command", text: "./bin/shift2eu assess --inventory examples/aws/inventory.json", want: nil},
		{name: "markdown link target", text: "[Try it](../docs/guide/try-it.md)", want: nil},
		{name: "decimal number", text: `"amount": 84.10,`, want: nil},
		{name: "decimal number in prose", text: "the estimate is 1234.56 per month", want: nil},
		{name: "inventory size field path", text: "give the volume a `size.storageGiB`", want: nil},
		{name: "inventory usage field path", text: "a resource runs for `usage.hoursPerMonth`", want: nil},
		{
			name: "inventory field path under a resource index",
			text: "`resources[1].size.storageGiB` is the volume's size",
			want: nil,
		},
		{
			name: "real customer host beside a decimal number",
			text: `"amount": 84.10 billed by shop.acme.io`,
			want: []string{"shop.acme.io"},
		},
		{
			name: "real customer host followed by a digit label",
			text: "billed by shop.acme.io.1",
			want: []string{"shop.acme.io.1"},
		},
		{name: "real customer host ending in a digit label", text: "the shop runs on db.acme.com.2", want: []string{"db.acme.com.2"}},
		{name: "real customer domain", text: "the shop runs on shop.acme.io", want: []string{"shop.acme.io"}},
		{name: "real customer domain in capitals", text: "the shop runs on SHOP.ACME.IO", want: []string{"SHOP.ACME.IO"}},
		{name: "real host in a URL", text: "see https://shop.acme.io/orders", want: []string{"shop.acme.io"}},
		{name: "real host in a protocol-relative URL", text: "see //shop.acme.io/orders", want: []string{"shop.acme.io"}},
		{
			name: "bucket host below the provider API domain",
			text: "gs://shop-assets/index.html at real-bucket.storage.googleapis.com",
			want: []string{"real-bucket.storage.googleapis.com"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, values(hostnameProblems(tc.text))); diff != "" {
				t.Errorf("hostnameProblems(%q) mismatch (-want +got):\n%s", tc.text, diff)
			}
		})
	}
}

// TestSanitisationProblemsNameFileAndLine pins that a problem carries the line
// the value is on, so a failure of TestSamplesAreSanitised points whoever reads
// it at the line to fix.
func TestSanitisationProblemsNameFileAndLine(t *testing.T) {
	t.Parallel()

	problems := hostnameProblems("shop-web-1 in eu-central-1\nthe shop runs on shop.acme.io\n")

	if len(problems) != 1 {
		t.Fatalf("hostnameProblems found %d problems, want 1: %v", len(problems), values(problems))
	}

	if problems[0].line != 2 {
		t.Errorf("problem on line %d, want line 2", problems[0].line)
	}
}
