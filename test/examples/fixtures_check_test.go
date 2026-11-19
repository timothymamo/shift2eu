package examples_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
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

// Equal reports whether e and other are the same entry, field by field. go-cmp
// calls it to compare two entries, whose fields are unexported.
func (e allowListEntry) Equal(other allowListEntry) bool {
	return e == other
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

// checkedFiles returns every regular file the real-data check reads, sorted by
// path: those under <root>/examples and <root>/docs/guide, and those under every
// directory named testdata anywhere below root. A sample's name is its
// slash-separated path relative to root. A missing directory yields no file and
// no error. A file that is not valid UTF-8 (an image, an archive) holds no text
// to copy a value out of and is left out.
//
// The walk goes through an os.Root, so no read can follow a symbolic link out
// of root.
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

		if !entry.Type().IsRegular() || !checkedPath(name) {
			return nil
		}

		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}

		if !utf8.Valid(data) {
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

// sanitisationProblems returns every value a checked file may not carry under
// CLAUDE.md rule 2, rule by rule: account IDs, GUIDs, IPv4 addresses, IPv6
// addresses and host names. The first four rules are the samples' own; the host
// name rule is the narrower fixture one (fixtureHostnameProblems), so the strict
// rule TestSamplesAreSanitised applies over examples/ is unchanged.
func sanitisationProblems(text string) []problem {
	return slices.Concat(
		accountIDProblems(text),
		guidProblems(text),
		ipv4Problems(text),
		ipv6Problems(text),
		fixtureHostnameProblems(text),
	)
}

// publicTLDs are the top-level domains, lowercased, under which the fixture
// check reads a dotted name as a host name. The guide pages and the testdata
// directories are full of dotted values that are no host at all (file names,
// versions, jq and JSON paths, plan names, Go selectors), so a name counts as a
// host only when its last label is one of these. The list is curated, not
// complete: the generic TLDs a company runs a host under and the common
// European and North American country codes. It deliberately leaves out the
// TLDs that clash with a file extension a fixture or a guide page names (md,
// sh, py, rs), so install.md or run.sh is never read as a host; json, yaml, go
// and golden are no TLDs at all. Poland's pl is kept although it is Perl's
// extension too: no fixture names a Perl script.
var publicTLDs = []string{
	// Generic.
	"com", "net", "org", "io", "dev", "app", "cloud", "ai", "co",
	"info", "biz", "tech", "online", "site", "xyz",
	// The EU and common country codes.
	"eu", "de", "fr", "nl", "uk", "ch", "at", "be", "se", "fi", "no", "dk",
	"es", "it", "pl", "us",
}

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
// (publicTLDs) and that are not an exact public vendor host
// (publicVendorHosts). Every name hostnameProblems already accepts (example.com
// and its subdomains, the reserved .example name, the provider API hosts, the
// taxonomy vocabulary) stays accepted.
func fixtureHostnameProblems(text string) []problem {
	var problems []problem

	for _, p := range hostnameProblems(text) {
		lower := strings.ToLower(p.value)

		if slices.Contains(publicVendorHosts, lower) {
			continue
		}

		if !slices.Contains(publicTLDs, lastLabel(lower)) {
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

// fixtureProblems runs the sanitisation check over every checked file below
// root, leaving out the values realDataAllowList excuses in that file. The
// problems are keyed by the file's name; a clean file has no key.
func fixtureProblems(root string) (map[string][]problem, error) {
	files, err := checkedFiles(root)
	if err != nil {
		return nil, err
	}

	problems := make(map[string][]problem)

	for _, file := range files {
		data, err := os.ReadFile(file.path)
		if err != nil {
			return nil, err
		}

		for _, p := range sanitisationProblems(string(data)) {
			if allowListed(file.name, p.value) {
				continue
			}

			problems[file.name] = append(problems[file.name], p)
		}
	}

	return problems, nil
}

// allowListed reports whether realDataAllowList excuses value in the file name.
func allowListed(name, value string) bool {
	return slices.ContainsFunc(realDataAllowList, func(entry allowListEntry) bool {
		return entry.file == name && entry.value == value
	})
}
