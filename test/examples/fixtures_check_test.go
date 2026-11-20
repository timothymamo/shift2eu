package examples_test

import (
	"errors"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
type allowListEntry struct {
	// file is the slash-separated path of one regular file, relative to the
	// module root: never a directory or a glob.
	file string
	// value is the exact offending value the check reports.
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
		file:  "docs/guide/http-api.md",
		value: "127.0.0.1",
		why:   "the loopback address the server listens on by default; it names no host of anyone's",
	},
	{
		file:  "docs/guide/http-api.md",
		value: "0.0.0.0",
		why:   "the wildcard listen address that exposes the server beyond the machine; it names no host",
	},
	{
		file:  "docs/guide/reverse-proxy.md",
		value: "127.0.0.1",
		why:   "the loopback address the server listens on behind the proxy; it names no host of anyone's",
	},
	{
		file:  "docs/guide/reverse-proxy.md",
		value: "0.0.0.0",
		why:   "the wildcard listen address the page warns against; it names no host",
	},
	{
		file:  "docs/guide/try-it.md",
		value: "127.0.0.1",
		why:   "the loopback address the local server answers on; it names no host of anyone's",
	},
	{
		file:  "test/architecture/testdata/cmdprobe/cmd/shift2eu-server/main.go",
		value: "127.0.0.1",
		why:   "the probe's default listen flag, loopback on an ephemeral port; it names no host of anyone's",
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
// with dots or, into a name, with hyphens), IPv6 addresses and host names. The
// first four rules are the samples' own; the host name rule is the narrower
// fixture one (fixtureHostnameProblems) and the hyphenated-address rule covers a
// form only a fixture holds (hyphenatedIPv4Problems), so the strict rule
// TestSamplesAreSanitised applies over examples/ is unchanged.
func sanitisationProblems(text string) []problem {
	return slices.Concat(
		accountIDProblems(text),
		guidProblems(text),
		ipv4Problems(text),
		hyphenatedIPv4Problems(text),
		ipv6Problems(text),
		fixtureHostnameProblems(text),
	)
}

// publicTLDs are the top-level domains, lowercased, under which the fixture
// check reads a dotted name as a host name. The guide pages and the testdata
// directories are full of dotted values that are no host at all (file names,
// versions, jq and JSON paths, plan names, Go selectors), so a name counts as a
// host only when its last label is one of these. The list is curated, not
// complete: the generic TLDs a company runs a host under, every EU, EEA and
// EFTA country code but the two below, a few other common country codes,
// OVHcloud's own ovh, and the private DNS suffixes an internal name is built
// from (internal, the AWS and GCP private zone; corp; lan).
//
// It fails open, knowingly: because it is an allow list, a host under a TLD it
// does not name passes, so this is a filter for the likely value, never a
// guarantee. The known gaps are the sponsored and restricted TLDs (gov, edu,
// int, mil, aero, coop, jobs, museum, travel), the country codes left out below
// (in, br, cn, ru, tr, mx, ar, za, sg, hk, kr, tw, il, ae, nz, th, vn, id, my,
// ph, pk, sa, ua, ...), the newer generic ones and the internationalised ones.
// Additions are expected whenever a likely one turns up in a guide page or a
// fixture.
//
// The two suffixes a Go selector ends with would be a worse gap than that, since
// every Go-source fixture that imports the package carries one of them (Iceland's
// is in errors.Is, the private suffix local in time.Local), so both are matched
// as hosts too, but only when the name's last label is written in exactly this
// lower case (lowercaseOnlyTLDs): db01.corp.local, db01.Corp.local and
// shop.example.is are reported, the selectors, whose last label a capital opens,
// are not. The gaps there are a name whose last label is capitals
// (DB01.CORP.LOCAL passes) and a lower-case unexported Go selector in a
// Go-source fixture (cfg.local, s.is), which would be reported; nothing in the
// tree trips that one, and reporting a selector is the safe direction.
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
	// Other common country codes.
	"us", "ca", "au", "jp", "me", "tv", "ly",
	// Private DNS suffixes: such a name identifies a customer's own network.
	"internal", "corp", "lan",
}

// lowercaseOnlyTLDs are the public suffixes under which the fixture check reads
// a name as a host only when the name's last label is written in exactly this
// lower case: a Go selector ends with one of them and capitalises that label
// (errors.Is, time.Local), and a Go-source fixture is full of those, while a
// host under them - the country code of Iceland, or the .local zone mDNS and a
// customer's own internal DNS use - is written in lower case, whatever the
// labels before it are (db01.Corp.local). An all-caps name (DB01.CORP.LOCAL)
// passes, a gap of the same kind as the curated TLD list above, and a lower-case
// unexported selector in a Go-source fixture (cfg.local, s.is) would be
// reported, which fails closed.
var lowercaseOnlyTLDs = []string{"local", "is"}

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

// lastLabel returns the label after the last dot of name.
func lastLabel(name string) string {
	return name[strings.LastIndex(name, ".")+1:]
}

// hyphenatedIPv4Pattern finds an IPv4 address written with hyphens rather than
// dots: four runs of one to three digits, the way AWS and Azure write an address
// into a machine's name (ip-10-1-2-3, ip-10-1-2-3.eu-central-1.compute.internal).
// A run of four digits cannot match, so the year of a date (2026-11-20) never
// starts one, and a date has three groups rather than four; the shape alone is
// still no proof, so a candidate must be an address written into a name
// (hyphenatedIPv4InName) whose groups are octets (parseHyphenatedIPv4).
var hyphenatedIPv4Pattern = regexp.MustCompile(`\b[0-9]{1,3}(?:-[0-9]{1,3}){3}\b`)

// hyphenatedIPv4Problems returns a problem for every IPv4 address the fixture
// check reads in a name written with hyphens, when the address is outside the
// documentation range every sample address is in (documentationPrefix), the same
// one range the dotted rule holds a sample to. It reports the address the way
// ipv4Problems reports a dotted one. The .internal suffix already catches a full
// AWS host name (ip-10-1-2-3.eu-central-1.compute.internal) through the host name
// rule, but a bare ip-10-1-2-3 carries no dot at all, so without this rule a
// customer's private address written that way would pass.
//
// It knowingly leaves these forms to a follow-up issue rather than report a value
// it cannot tell from prose:
//
//   - an address with no ip- prefix in no dotted name, bare (10-1-2-3) or inside
//     a single label (node-10-1-2-3): the run alone is a run of numbers, and the
//     rule reports only what a name marks as an address;
//   - an address written with underscores (ip_10_1_2_3), which no source writes
//     into a name;
//   - a zero-padded octet (ip-10-01-2-3) and a hexadecimal address (ip-0a010203),
//     which parseHyphenatedIPv4 rejects: a name holding one passes;
//   - a run a leftmost match swallows: in x-5-10-1-2-3 the pattern matches
//     5-10-1-2-3 from the leftmost digit, which is no address, and the address
//     inside it is never looked at again.
func hyphenatedIPv4Problems(text string) []problem {
	var problems []problem

	for _, match := range hyphenatedIPv4Pattern.FindAllStringIndex(text, -1) {
		if !hyphenatedIPv4InName(text, match[0], match[1]) {
			continue
		}

		candidate := text[match[0]:match[1]]

		addr, ok := parseHyphenatedIPv4(candidate)
		if !ok {
			continue
		}

		if documentationPrefixNet.Contains(addr) {
			continue
		}

		problems = append(problems, problem{
			line:  lineOf(text, match[0]),
			value: candidate,
			rule:  "is an IPv4 address written into a name, outside the documentation range " + documentationPrefix + " (CLAUDE.md rule 2)",
		})
	}

	return problems
}

// hyphenatedIPv4InName reports whether the four-group run text[start:end] is an
// address written into a name rather than a run of numbers that merely has its
// shape: the run either follows an ip- that stands on its own, the prefix AWS and
// Azure give an address in a machine's name (ip-10-1-2-3), or it is the first
// label of a dotted name (10-1-2-3.eu-central-1.compute.internal, and so
// vm-10-1-2-3.acme.corp too). A documentation domain name can hold one as well:
// 10-1-2-3.example.com names a private address although its domain identifies
// nobody, so the address alone is reported. A run of four numbers in prose
// (1-2-3-4) or at the end of a version-like token (build-1-2-3-4) is neither: the
// ip- inside ship- or zip- is no prefix (hyphenatedIPv4AfterIPPrefix), and the
// full stop that ends a sentence opens no label, so the rule stays off values that
// only look like an address; parseHyphenatedIPv4 then keeps out every run whose
// groups are not octets and every run of digits with a leading zero.
func hyphenatedIPv4InName(text string, start, end int) bool {
	if hyphenatedIPv4AfterIPPrefix(text, start) {
		return true
	}

	return strings.HasPrefix(text[end:], ".") && end+1 < len(text) && labelByte(text[end+1])
}

// hyphenatedIPv4AfterIPPrefix reports whether text[:start] ends with an ip-
// prefix that stands on its own: one that starts the text, or one whose byte
// before is no letter or digit, so the ip- inside ship- or zip- is no prefix. The
// match is case-insensitive, since a name is written in capitals as often as
// not.
func hyphenatedIPv4AfterIPPrefix(text string, start int) bool {
	prefix := start - len("ip-")
	if prefix < 0 || !strings.EqualFold(text[prefix:start], "ip-") {
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
// separated by hyphens, as an IPv4 address: the groups are the octets, so a
// group above 255 makes the candidate no address at all, and it is then not
// reported.
func parseHyphenatedIPv4(candidate string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(strings.ReplaceAll(candidate, "-", "."))
	if err != nil || !addr.Is4() {
		return netip.Addr{}, false
	}

	return addr, true
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
func staleAllowListEntries(root string, list []allowListEntry) ([]staleAllowListEntry, error) {
	var stale []staleAllowListEntry

	for _, entry := range list {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entry.file)))
		if err != nil {
			return nil, err
		}

		reported := problemValues(sanitisationProblems(fixtureText(data)))
		if slices.Contains(reported, entry.value) {
			continue
		}

		stale = append(stale, staleAllowListEntry{entry: entry, reported: reported})
	}

	return stale, nil
}
