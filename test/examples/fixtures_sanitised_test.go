package examples_test

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/google/go-cmp/cmp"
)

// realAccountID is an account ID a fixture may not carry: any 12-digit number
// other than the documented dummy 123456789012 stands for a real AWS account or
// GCP project number, so the sanitisation check must report it.
const realAccountID = "210987654321"

// realHost is a host name a fixture may not carry: it is not a documentation
// name (example.com, .example) and not a provider's public API host name, so it
// stands for a customer's own domain.
const realHost = "shop.acme.io"

// TestFixturesAreSanitised pins CLAUDE.md rule 2 for every file a reader copies
// a value out of, not only for the samples under examples/: the guide pages
// under docs/guide/ (the inventory guide's own examples included) and every
// testdata/ directory in the module carry inventory-shaped text too, so each of
// them may carry documentation values only. The check covers the listed paths
// whether or not they hold a problem today, so it never passes by scanning
// nothing.
func TestFixturesAreSanitised(t *testing.T) {
	t.Parallel()

	files, err := checkedFiles(repoRoot)
	if err != nil {
		t.Fatalf("checkedFiles(%q): %v", repoRoot, err)
	}

	for _, want := range []string{
		"examples/",
		"docs/guide/",
		"docs/guide/inventory/",
		"test/e2e/testdata/",
		"testdata/",
		"internal/adapter/source/inventoryfile/testdata/",
	} {
		if !slices.ContainsFunc(files, func(file sample) bool { return strings.HasPrefix(file.name, want) }) {
			t.Errorf("checkedFiles(%q) covers no file under %s: want at least one, so the check cannot pass by scanning nothing",
				repoRoot, want)
		}
	}

	problems, err := fixtureProblems(repoRoot)
	if err != nil {
		t.Fatalf("fixtureProblems(%q): %v", repoRoot, err)
	}

	// The map has no order, so the failures are reported in path order: a run
	// reads the same way every time.
	for _, name := range slices.Sorted(maps.Keys(problems)) {
		for _, p := range problems[name] {
			t.Errorf("%s:%d: %q %s", name, p.line, p.value, p.rule)
		}
	}
}

// TestPlantedRealDataFailsTheFixtureCheck pins that the check reaches every kind
// of path it claims to cover: a file planted at a guide path, at a path under
// docs/guide/inventory/, at a testdata path of an adapter, of the module root, of
// test/e2e, at a testdata path nested inside another testdata directory and at a
// sample path is reported, with the account ID and the host name it carries.
// Without this, a walker that silently skipped a directory would pass
// TestFixturesAreSanitised.
func TestPlantedRealDataFailsTheFixtureCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		content string
		want    []string
	}{
		{
			name:    "guide page",
			path:    "docs/guide/planted.md",
			content: "account " + realAccountID + " runs " + realHost,
			want:    []string{realAccountID, realHost},
		},
		{
			name:    "inventory guide example",
			path:    "docs/guide/inventory/planted.jq",
			content: "account " + realAccountID + " runs " + realHost,
			want:    []string{realAccountID, realHost},
		},
		{
			name:    "end-to-end golden",
			path:    "test/e2e/testdata/planted.golden",
			content: "account " + realAccountID + " runs " + realHost,
			want:    []string{realAccountID, realHost},
		},
		{
			name:    "module root testdata",
			path:    "testdata/planted.json",
			content: "account " + realAccountID + " runs " + realHost,
			want:    []string{realAccountID, realHost},
		},
		{
			name:    "adapter testdata",
			path:    "internal/adapter/report/markdown/testdata/planted.golden",
			content: "account " + realAccountID + " runs " + realHost,
			want:    []string{realAccountID, realHost},
		},
		{
			name:    "testdata nested in a testdata",
			path:    "test/lint/testdata/docsources/mod/test/e2e/testdata/planted.txt",
			content: "account " + realAccountID + " runs " + realHost,
			want:    []string{realAccountID, realHost},
		},
		{
			name:    "sample inventory",
			path:    "examples/aws/planted.json",
			content: "account " + realAccountID + " runs " + realHost,
			want:    []string{realAccountID, realHost},
		},
		{
			name:    "account ID alone",
			path:    "docs/guide/inventory/planted-account.jq",
			content: "account " + realAccountID,
			want:    []string{realAccountID},
		},
		{
			name:    "host name alone",
			path:    "docs/guide/inventory/planted-host.jq",
			content: "the shop runs on " + realHost,
			want:    []string{realHost},
		},
		{
			// A name under the mDNS and private-zone suffix .local or under
			// Iceland's .is identifies a customer's own host just as one under
			// any other TLD does, and a Go selector in the same file is no host:
			// the row plants all four, so the check must report the two hosts and
			// nothing else.
			name:    "private and Icelandic hosts beside Go selectors",
			path:    "docs/guide/planted-local.md",
			content: "the file server is db01.corp.local and the shop is shop.example.is, which the code checks with errors.Is and time.Local",
			want:    []string{"db01.corp.local", "shop.example.is"},
		},
		{
			// A Go-source fixture under a testdata directory is a checked file
			// too, and the selectors it names are no host: a rule that matched
			// them by suffix would report every Go file in the module.
			name:    "Go selectors in a Go-source fixture",
			path:    "testdata/planted.go",
			content: "package fixture\n\nimport (\n\t\"errors\"\n\t\"time\"\n)\n\nvar (\n\tzone     = time.Local\n\tsentinel = errors.Is\n)\n",
			want:    nil,
		},
		{
			// AWS writes an address into a host name with hyphens rather than
			// dots, so the dotted-quad rule cannot see it; the bare form has no
			// dot at all, so only the hyphenated rule finds it.
			name:    "private address written into a name",
			path:    "docs/guide/planted-ip.md",
			content: "the instance ip-10-1-2-3 answers on port 8080",
			want:    []string{"10-1-2-3"},
		},
		{
			// The same address written with underscores rather than hyphens,
			// the way a page that copies one out of a name may write it.
			name:    "private address with underscore separators",
			path:    "docs/guide/planted-ip-underscore.md",
			content: "the peer ip_10_1_2_3 answers on port 8080",
			want:    []string{"10_1_2_3"},
		},
		{
			// A zero-padded octet is no octet to netip.ParseAddr, so the run is
			// read as the address it spells and reported as it is written.
			name:    "private address with a zero-padded octet",
			path:    "testdata/planted-ip-padded.json",
			content: `"peer": "ip-10-01-2-3"`,
			want:    []string{"10-01-2-3"},
		},
		{
			name:    "customer host under the Indian TLD",
			path:    "docs/guide/planted-in.md",
			content: "the shop answers at web.acme.in",
			want:    []string{"web.acme.in"},
		},
		{
			name:    "customer bucket in an s3 URL",
			path:    "testdata/planted-bucket.json",
			content: "the objects are in s3://acme-prod-bucket/key",
			want:    []string{"s3://acme-prod-bucket"},
		},
		{
			name:    "private address in an AWS host name",
			path:    "testdata/planted-host.json",
			content: `"hostname": "ip-10-1-2-3.eu-central-1.compute.internal"`,
			want:    []string{"10-1-2-3", "ip-10-1-2-3.eu-central-1.compute.internal"},
		},
		{
			// An address is reported wherever a name carries it, not only
			// behind an ip-: AWS and Azure write it behind a label of the
			// machine's own as well.
			name:    "private address after a letter-hyphen prefix",
			path:    "docs/guide/planted-node.md",
			content: "the node node-10-1-2-3 answers on port 8080",
			want:    []string{"10-1-2-3"},
		},
		{
			name:    "customer host under the government TLD",
			path:    "docs/guide/planted-gov.md",
			content: "the database is db.acme.gov",
			want:    []string{"db.acme.gov"},
		},
		{
			// The vendor host is exempt and the path after it is not: the
			// owner names the customer, so the check reports the host and the
			// owner together.
			name:    "customer owner path after a listed vendor host",
			path:    "testdata/planted-owner.json",
			content: "the module is github.com/acme/private-repo",
			want:    []string{"github.com/acme"},
		},
		{
			// The SSH clone form a reader copies out of a git command names the
			// owner after a colon, and the owner is the customer's all the same.
			name:    "customer owner in an SSH clone URL",
			path:    "docs/guide/planted-clone.md",
			content: "cloned with git@github.com:acme/private-repo.git",
			want:    []string{"github.com/acme"},
		},
		{
			// An ARN names a bucket with no scheme, and the ARN is reported whole
			// so the failure says which value to change.
			name:    "customer bucket in an ARN",
			path:    "testdata/planted-arn.json",
			content: `"bucket": "arn:aws:s3:::acme-prod-bucket"`,
			want:    []string{"arn:aws:s3:::acme-prod-bucket"},
		},
		{
			name:    "customer bucket in a Cloud Storage URL",
			path:    "testdata/planted-gcs.json",
			content: "the objects are at https://storage.googleapis.com/acme-prod-data/key",
			want:    []string{"storage.googleapis.com/acme-prod-data"},
		},
		{
			// A Cloud Asset name opens with the service host and carries the
			// customer's project ID, which is globally unique.
			name:    "customer project ID in a Cloud Asset name",
			path:    "test/e2e/testdata/planted-project.json",
			content: `"name": "//compute.googleapis.com/projects/acme-corp-prod/zones/z/instances/vm-1"`,
			want:    []string{"projects/acme-corp-prod"},
		},
		{
			// The dummy subscription GUID identifies nobody, while the storage
			// account the ARM ID names is globally unique and the customer's.
			name:    "customer storage account in an ARM ID",
			path:    "docs/guide/planted-arm.md",
			content: "/subscriptions/" + dummyGUID + "/resourceGroups/rg-shop/providers/Microsoft.Storage/storageAccounts/acmeprodcustomers",
			want:    []string{"storageAccounts/acmeprodcustomers"},
		},
		{
			// The reserved projects/_ segment names no project, so only the
			// bucket after it is the customer's own name.
			name:    "customer bucket in a Cloud Storage resource name",
			path:    "docs/guide/inventory/planted-bucket-name.jq",
			content: `"name": "projects/_/buckets/acme-prod-data"`,
			want:    []string{"buckets/acme-prod-data"},
		},
		{
			// An S3 access point is an ARN of its own shape, whose region and
			// account the bucket ARN's ::: never carries.
			name:    "customer access point in an S3 ARN",
			path:    "testdata/planted-accesspoint.json",
			content: `"accessPoint": "arn:aws:s3:eu-central-1:` + dummyAccountID + `:accesspoint/acme-prod-ap"`,
			want:    []string{"accesspoint/acme-prod-ap"},
		},
		{
			// The other side of the same rules, planted rather than assumed: the
			// project's own host and a sample's bucket are what a checked file may
			// carry, so the check reports nothing for them.
			name:    "public project host and sample bucket",
			path:    "testdata/planted-clean.txt",
			content: "cloned from github.com/timothymamo/shift2eu, the sample objects are in s3://example-bucket/key",
			want:    nil,
		},
		{
			// Every sample address is in 192.0.2.0/24, and the hyphenated rule
			// holds an address written into a name to that range just as the
			// dotted rule does: ip-192-0-2-10 is clean, the other two RFC 5737
			// ranges are no sample value and are reported.
			name:    "documentation addresses written into names",
			path:    "docs/guide/planted-ip-doc.md",
			content: "ip-192-0-2-10, ip-198-51-100-7 and ip-203-0-113-9 are documentation names",
			want:    []string{"198-51-100-7", "203-0-113-9"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			plantFixture(t, dir, tc.path, tc.content)

			problems, err := fixtureProblems(dir)
			if err != nil {
				t.Fatalf("fixtureProblems(%q): %v", dir, err)
			}

			// The temp tree holds this one file, so every reported problem is
			// this file's and the values are exactly what the row plants.
			if diff := cmp.Diff(tc.want, problemValues(problems[tc.path])); diff != "" {
				t.Errorf("%s holds %q, but fixtureProblems reports other values (-want +got):\n%s",
					tc.path, tc.content, diff)
			}
		})
	}
}

// TestNonUTF8TextIsNotSkipped pins that a checked file whose bytes are not
// valid UTF-8 is still read for values. A fixture arrives in whatever encoding
// the tool that wrote it chose: a Windows editor writes UTF-16 with a
// byte-order mark, and a page saved from a browser may be in a Latin-1
// encoding. A real account ID or host name in such a file is still real data,
// so skipping the file would let one into the repository unseen.
func TestNonUTF8TextIsNotSkipped(t *testing.T) {
	t.Parallel()

	text := "account " + realAccountID + " runs " + realHost

	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "UTF-16LE with a byte-order mark",
			content: utf16LEBOM(text),
		},
		{
			// Z\xfcrich is Zürich in Latin-1: the 0xFC byte makes the file invalid
			// UTF-8, the way a name in a page a person saved does.
			name:    "Latin-1",
			content: text + " Z\xfcrich",
		},
	}

	const plantedPath = "test/e2e/testdata/planted-encoding.json"

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			plantFixture(t, dir, plantedPath, tc.content)

			problems, err := fixtureProblems(dir)
			if err != nil {
				t.Fatalf("fixtureProblems(%q): %v", dir, err)
			}

			got := problemValues(problems[plantedPath])
			for _, want := range []string{realAccountID, realHost} {
				if !slices.Contains(got, want) {
					t.Errorf("%s holds %q in %s, but fixtureProblems reports %v there, which lacks %q",
						plantedPath, text, tc.name, got, want)
				}
			}
		})
	}
}

// TestSymlinkInACheckedPathFailsTheCheck pins that a symbolic link in a checked
// path cannot smuggle a value past the check: the link's target may be a file
// outside the checked paths, so reading through the link is how a real value
// would reach a checked path. Either design fails the run - a problem that
// carries the values read through the link, or one that names the link itself -
// but the link never goes unreported, and the check never follows it silently
// and passes.
func TestSymlinkInACheckedPathFailsTheCheck(t *testing.T) {
	t.Parallel()

	const (
		target = "docs/adr/planted-real-value.json"
		link   = "test/e2e/testdata/link.json"
	)

	content := "account " + realAccountID + " runs " + realHost

	dir := t.TempDir()
	plantFixture(t, dir, target, content)

	linkPath := filepath.Join(dir, filepath.FromSlash(link))
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o750); err != nil {
		t.Fatalf("create the directory for %s: %v", link, err)
	}

	if err := os.Symlink(filepath.Join(dir, filepath.FromSlash(target)), linkPath); err != nil {
		t.Skipf("this filesystem cannot hold a symbolic link, so the case cannot be planted: %v", err)
	}

	problems, err := fixtureProblems(dir)
	if err != nil {
		t.Fatalf("fixtureProblems(%q): %v", dir, err)
	}

	// The check may report the link itself or the values it reads through it,
	// so only a non-empty result under the link's key is pinned.
	if len(problems[link]) == 0 {
		t.Errorf("%s is a symbolic link to %s, which holds %q, but fixtureProblems reports no problem under it (it reports problems for %v)",
			link, target, content, slices.Sorted(maps.Keys(problems)))
	}
}

// utf16LEBOM returns text encoded as UTF-16 little-endian behind a byte-order
// mark (0xFF 0xFE), the shape a Windows editor writes a UTF-16 file in.
func utf16LEBOM(text string) string {
	units := utf16.Encode([]rune(text))

	encoded := make([]byte, 0, 2+2*len(units))
	encoded = append(encoded, 0xff, 0xfe)

	for _, unit := range units {
		encoded = binary.LittleEndian.AppendUint16(encoded, unit)
	}

	return string(encoded)
}

// TestFilesOutsideTheCheckedPathsAreIgnored pins the other side of the rule: a
// Go file in a package directory and an ADR page are not fixtures and hold no
// sample to copy, so a value in either of them is no fixture problem. Without
// this, a walker that took the whole module would report the ADRs' own examples.
func TestFilesOutsideTheCheckedPathsAreIgnored(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	plantFixture(t, dir, "internal/domain/planted.go", "package domain\n\n// account "+realAccountID+" runs "+realHost+"\n")
	plantFixture(t, dir, "docs/adr/planted.md", "account "+realAccountID+" runs "+realHost)

	problems, err := fixtureProblems(dir)
	if err != nil {
		t.Fatalf("fixtureProblems(%q): %v", dir, err)
	}

	for _, name := range slices.Sorted(maps.Keys(problems)) {
		t.Errorf("%s is not a checked path, but fixtureProblems reports %v there", name, problemValues(problems[name]))
	}
}

// TestAllowListStalenessReadsFilesLikeTheCheck pins that the staleness
// judgement reads an allow-listed file the way the check itself reads it
// (fixtureText): a file a Windows editor wrote as UTF-16 still holds the value
// its entry covers, so the entry is not stale. Read as raw bytes, the value is
// invisible and an entry that is still needed would be reported as stale,
// which pushes whoever reads the failure to delete the entry and lets the
// value back into the check's report.
func TestAllowListStalenessReadsFilesLikeTheCheck(t *testing.T) {
	t.Parallel()

	const (
		file  = "docs/guide/planted-utf16.md"
		value = "127.0.0.1"
	)

	dir := t.TempDir()
	plantFixture(t, dir, file, utf16LEBOM("the server listens on "+value+" by default\n"))

	list := []allowListEntry{{
		file:  file,
		value: value,
		why:   "the loopback address the planted page names; it names no host of anyone's",
	}}

	stale, err := staleAllowListEntries(dir, list)
	if err != nil {
		t.Fatalf("staleAllowListEntries(%q): %v", dir, err)
	}

	if len(stale) != 0 {
		t.Errorf("%s holds %q as UTF-16, which the check decodes before it looks, but staleAllowListEntries judges %v stale",
			file, value, stale)
	}
}

// TestAllowListStalenessJudgesEveryEntry pins that an allow-list entry whose
// file the staleness pass cannot read does not end the pass: the entries after
// it are still judged, and the unreadable entry is reported alongside them. An
// entry naming a missing or non-regular file is what the allow list's own
// structural checks report, so ending the judgement there would hide every
// stale entry behind it, and the duplicate-pair check that follows too, exactly
// when the allow list is already broken.
func TestAllowListStalenessJudgesEveryEntry(t *testing.T) {
	t.Parallel()

	const (
		missing      = "docs/guide/planted-missing.md"
		staleFile    = "docs/guide/planted-stale.md"
		loopbackAddr = "127.0.0.1"
	)

	dir := t.TempDir()
	plantFixture(t, dir, staleFile, "the sample node is shop-web-1 in eu-central-1\n")

	// The unreadable entry comes first, so the pass has to go on to the stale
	// one rather than stop at the read error.
	list := []allowListEntry{
		{
			file:  missing,
			value: loopbackAddr,
			why:   "the entry whose file is gone; the structural checks report it",
		},
		{
			file:  staleFile,
			value: loopbackAddr,
			why:   "the entry whose file reports nothing, so the entry is stale",
		},
	}

	stale, err := staleAllowListEntries(dir, list)
	if err == nil {
		t.Errorf("staleAllowListEntries(%q) reports no error although %s cannot be read: an unreadable entry is no clean entry",
			dir, missing)
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("staleAllowListEntries(%q) reports %v, want an error wrapping %v for the unreadable %s",
			dir, err, fs.ErrNotExist, missing)
	}

	got := make([]string, len(stale))
	for i, s := range stale {
		got[i] = s.entry.file
	}

	if diff := cmp.Diff([]string{staleFile}, got); diff != "" {
		t.Errorf("staleAllowListEntries(%q) reads %s as reporting nothing, so its entry is stale, even though %s cannot be read (-want +got):\n%s",
			dir, staleFile, missing, diff)
	}
}

// TestAllowListEntriesAreNarrowAndUsed pins that the allow list stays an
// exception list rather than a blanket exception: each entry names one regular
// file of the module and one exact value, gives a reason, and still describes a
// value the unfiltered check reports in that file. A stale entry fails, and so
// does a directory, a glob or an entry that names no value.
func TestAllowListEntriesAreNarrowAndUsed(t *testing.T) {
	t.Parallel()

	moduleRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("resolve the module root %q: %v", repoRoot, err)
	}

	for i, entry := range realDataAllowList {
		if entry.why == "" {
			t.Errorf("realDataAllowList[%d] (%s: %q) gives no reason: an allow list entry explains itself", i, entry.file, entry.value)
		}

		if entry.value == "" {
			t.Errorf("realDataAllowList[%d] (%s) names no value: an entry covers one exact value", i, entry.file)
		}

		switch {
		case entry.file == "":
			t.Errorf("realDataAllowList[%d] (%q) names no file", i, entry.value)

			continue
		case strings.HasSuffix(entry.file, "/"):
			t.Errorf("realDataAllowList[%d]: %q names a directory, not a file: an entry covers one file", i, entry.file)

			continue
		case strings.ContainsAny(entry.file, "*?["):
			t.Errorf("realDataAllowList[%d]: %q is a glob, not a file: an entry covers one file", i, entry.file)

			continue
		}

		path := filepath.Join(moduleRoot, filepath.FromSlash(entry.file))
		if !strings.HasPrefix(path, moduleRoot+string(filepath.Separator)) {
			t.Errorf("realDataAllowList[%d]: %q is not a path inside the module (%s)", i, entry.file, moduleRoot)

			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("realDataAllowList[%d]: %s is not a file of the module: %v", i, entry.file, err)

			continue
		}

		if !info.Mode().IsRegular() {
			t.Errorf("realDataAllowList[%d]: %s is not a regular file: an entry covers one file", i, entry.file)

			continue
		}
	}

	// The staleness judgement reads every allow-listed file once, the way the
	// check itself reads it, so the entries the module still needs are the ones
	// it keeps. An entry whose file cannot be read is reported and the pass goes
	// on: stopping here would hide every stale entry behind it, and the
	// duplicate-pair check below too, exactly when the allow list is broken.
	stale, err := staleAllowListEntries(moduleRoot, realDataAllowList)
	if err != nil {
		t.Errorf("staleAllowListEntries(%q): %v", moduleRoot, err)
	}

	for _, s := range stale {
		t.Errorf("realDataAllowList[%d]: %s no longer reports %q, so the entry is stale (it reports %v)",
			slices.Index(realDataAllowList, s.entry), s.entry.file, s.entry.value, s.reported)
	}

	// The allow list is keyed on a (file, value) pair, so two entries that name
	// the same pair differ only in their reason and cover one value twice: the
	// second one excuses nothing the first does not.
	seen := make(map[[2]string]int, len(realDataAllowList))

	for i, entry := range realDataAllowList {
		key := [2]string{entry.file, entry.value}

		if first, ok := seen[key]; ok {
			t.Errorf("realDataAllowList[%d] and realDataAllowList[%d] excuse the same file and value (%s: %q): one entry each",
				first, i, entry.file, entry.value)

			continue
		}

		seen[key] = i
	}
}

// TestAllowListDoesNotLeakToOtherFiles pins that an entry covers the value in
// the one file it names and nowhere else: every value the allow list excuses,
// planted at another checked path, is still reported. An allow list keyed on a
// value alone would let a real value into a new file, whichever file it came
// from, so every distinct value is tried rather than the first entry's.
func TestAllowListDoesNotLeakToOtherFiles(t *testing.T) {
	t.Parallel()

	if len(realDataAllowList) == 0 {
		t.Skip("the allow list is empty, so no entry can leak into another file")
	}

	var excused []string

	for _, entry := range realDataAllowList {
		if !slices.Contains(excused, entry.value) {
			excused = append(excused, entry.value)
		}
	}

	// A testdata path no allow-listed file names, so only the (file, value)
	// pair of an entry could excuse the value.
	const plantedPath = "test/e2e/testdata/planted-leak.txt"

	for _, value := range excused {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			plantFixture(t, dir, plantedPath, "leaked value: "+value)

			problems, err := fixtureProblems(dir)
			if err != nil {
				t.Fatalf("fixtureProblems(%q): %v", dir, err)
			}

			if got := problemValues(problems[plantedPath]); !slices.Contains(got, value) {
				t.Errorf("%s carries %q, which the allow list excuses only in another file, but fixtureProblems reports %v there",
					plantedPath, value, got)
			}
		})
	}
}

// problemValues returns the offending values of problems, in order, so a test
// compares the values rather than the whole problem with its line and rule.
func problemValues(problems []problem) []string {
	if len(problems) == 0 {
		return nil
	}

	got := make([]string, len(problems))
	for i, p := range problems {
		got[i] = p.value
	}

	return got
}

// plantFixture writes content to path below dir, creating the directories in
// between, so a test can plant a file at a checked path the temp tree does not
// carry yet.
func plantFixture(t *testing.T, dir, path, content string) {
	t.Helper()

	full := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("create the directory for %s: %v", path, err)
	}

	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
