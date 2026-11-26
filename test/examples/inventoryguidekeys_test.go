package examples_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This file holds one half of PRD #571's privacy promise for the inventory
// guide's AWS section (*Only the fields the program reads*): each new call's
// recorded output keeps only the keys that call's jq program reads, so the raw
// output the guide tells a reader to delete carries none of the fields the PRD
// excludes. The other half is test/site's: it reads the guide's command text, so
// a command that dropped its --query cannot hide behind a fixture that never
// held the extra keys.

// guideCallFixture is one recorded call of the guide's search and Kafka
// classification: the call as the guide spells it, the fixture its --query or
// --format output is recorded in, and the keys its jq program reads.
type guideCallFixture struct {
	// call names the CLI call, for a failure message.
	call string
	// file is the fixture, relative to the module root.
	file string
	// keys are the fields the call's program reads: a key outside this list is
	// a field the program does not read, and so one the projection drops.
	keys []string
}

// searchAndKafkaFixtures are the guide's new calls of PRD #571, with the
// fixture of each and the fields its program reads. The fixtures hold the
// --query or --format output, so a key here is a field the guide's operator saw
// in the raw output before deleting it. The guide runs an AWS call once per
// region, so a call recorded in two regions has an entry per region, and the two
// must read the same fields.
var searchAndKafkaFixtures = []guideCallFixture{
	{
		call: "aws opensearch list-domain-names",
		file: "test/e2e/testdata/inventory-guide/aws/eu-central-1/opensearch-domain-names.json",
		keys: []string{"DomainNames", "DomainName"},
	},
	{
		call: "aws opensearch describe-domains",
		file: "test/e2e/testdata/inventory-guide/aws/eu-central-1/opensearch-domains.json",
		keys: []string{
			"DomainStatusList", "DomainName", "ARN", "EngineVersion", "ClusterConfig",
			"InstanceType", "InstanceCount", "ZoneAwarenessEnabled", "MultiAZWithStandbyEnabled",
			"EBSOptions", "EBSEnabled", "VolumeSize", "EncryptionAtRestOptions", "Enabled",
			"Deleted",
		},
	},
	{
		call: "aws opensearch describe-domains in eu-west-1",
		file: "test/e2e/testdata/inventory-guide/aws/eu-west-1/opensearch-domains.json",
		keys: []string{
			"DomainStatusList", "DomainName", "ARN", "EngineVersion", "ClusterConfig",
			"InstanceType", "InstanceCount", "ZoneAwarenessEnabled", "MultiAZWithStandbyEnabled",
			"EBSOptions", "EBSEnabled", "VolumeSize", "EncryptionAtRestOptions", "Enabled",
			"Deleted",
		},
	},
	{
		call: "aws opensearchserverless list-collections",
		file: "test/e2e/testdata/inventory-guide/aws/eu-central-1/opensearchserverless-collections.json",
		keys: []string{"collectionSummaries", "id", "name", "nextToken"},
	},
	{
		call: "aws opensearchserverless batch-get-collection",
		file: "test/e2e/testdata/inventory-guide/aws/eu-central-1/opensearchserverless-batch-collections.json",
		keys: []string{"collectionDetails", "name", "arn", "type", "standbyReplicas", "status"},
	},
	{
		call: "aws kafka list-clusters-v2",
		file: "test/e2e/testdata/inventory-guide/aws/eu-central-1/kafka-clusters-v2.json",
		keys: []string{
			"ClusterInfoList", "ClusterName", "ClusterType", "ClusterArn", "State", "Provisioned",
			"NumberOfBrokerNodes", "BrokerNodeGroupInfo", "InstanceType", "ClientSubnets", "StorageInfo",
			"EbsStorageInfo", "VolumeSize",
		},
	},
	{
		// GCP's one call for its Kafka clusters (PRD #571, milestone 3): the
		// cluster's name and its capacity, and nothing else. The two numbers of
		// the capacity are the cluster's totals over its brokers, so the program
		// reads them and writes no size, and no bootstrap address, TLS config or
		// access config reaches the file.
		call: "gcloud managed-kafka clusters list",
		file: "test/e2e/testdata/inventory-guide/gcp/managed-kafka-clusters.json",
		keys: []string{"name", "capacityConfig", "vcpuCount", "memoryBytes"},
	},
}

// excludedFixtureKeysPath is the one list of the fields PRD #571 excludes from
// every projection and fixture of the guide's search and messaging calls, shared
// with test/site's projection check so the two cannot drift apart (review finding
// on PR #639). It sits in this package's testdata directory, and test/site reads it
// by the same repository-relative path.
const excludedFixtureKeysPath = "test/examples/testdata/inventory_guide_excluded_keys.txt"

// excludedFixtureKeysRecordTablePath is the table of record documents both
// readers of that list are given, with the records or the rejection each expects
// of them (review finding on PR #654). The readers are this package's
// readExcludedFixtureKeys and test/site's readAWSExcludedFields, in
// test/site/inventory_test.go: neither test package may import the other, so each
// holds its own copy of the parser, and this table is what holds the two to the
// same documents. Both run it, each against its own copy of the parser, through
// TestTheTwoExcludedKeyReadersAcceptAndRejectTheSameRecords.
const excludedFixtureKeysRecordTablePath = "test/examples/testdata/inventory_guide_excluded_key_records.txt"

// excludedFixtureKeys is the shared list of the fields no fixture of a new call
// may carry (PRD #571, *Only the fields the program reads*): an access policy
// names the account's principals, a security option its settings, and an endpoint
// or a bootstrap address a host with the account's name in it.
type excludedFixtureKeys struct {
	// names are the fields a fixture may not hold at any depth.
	names []string
	// prefixes are the fields a fixture may not hold whatever their suffix: a
	// cluster's ZooKeeper connection strings and its broker addresses, which name
	// the cluster's hosts.
	prefixes []string
}

// excludes reports whether the key is one the shared list excludes.
func (e excludedFixtureKeys) excludes(key string) bool {
	return slices.Contains(e.names, key) ||
		slices.ContainsFunc(e.prefixes, func(prefix string) bool { return strings.HasPrefix(key, prefix) })
}

// The two kinds a record of the shared excluded-key list may have.
const (
	// recordKindKey is the kind of a record naming one field the guide's search
	// and messaging calls may not carry.
	recordKindKey = "key"
	// recordKindPrefix is the kind of a record naming a prefix no field may start
	// with.
	recordKindPrefix = "prefix"
)

// The reasons the shared excluded-key list, and the table of its record
// documents, reject a document: one error each, so a case of the table can name
// the reason it expects and both copies of the parser can be held to it (review
// finding on PR #654).
var (
	// errRecordMalformed rejects a record that is neither `key: <field>` nor
	// `prefix: <prefix>`.
	errRecordMalformed = errors.New("a record is neither `key: <field>` nor `prefix: <prefix>`")
	// errRecordKind rejects a record of a kind the list does not know.
	errRecordKind = errors.New("a record's kind is neither `key` nor `prefix`")
	// errRecordComment rejects a record line that carries a comment, which would
	// become part of the field's name.
	errRecordComment = errors.New("a record line carries a comment")
	// errRecordRepeat rejects a record an earlier line already wrote, which would
	// hide that first copy.
	errRecordRepeat = errors.New("a record repeats an earlier one")
	// errRecordEmptyHalf rejects a list that holds no key or no prefix.
	errRecordEmptyHalf = errors.New("the list holds no key or no prefix")
)

// excludedKeyRecord is one record of the shared excluded-key list: a field the
// guide's search and messaging calls may not carry, or a prefix no field may
// start with (PRD #571).
type excludedKeyRecord struct {
	// kind is recordKindKey or recordKindPrefix.
	kind string
	// name is the field, or the prefix a field may not start with.
	name string
}

// spelling renders the record as the shared list writes it.
func (r excludedKeyRecord) spelling() string {
	return r.kind + ": " + r.name
}

// parseExcludedKeys reads the records of the shared excluded-key list (PRD
// #571). A record is `key: <field>` or `prefix: <prefix>`; a comment and a blank
// line are no record. A malformed record, a record line that carries a comment
// (which would become part of the field's name), a record of an unknown kind or a
// record repeated is an error, and so is a list with an empty half: a list this
// parser skipped or shrank would let a field the PRD excludes through both
// checks, and a repeat would hide its first copy.
//
// The same parser, kept by hand, is test/site's parseAWSExcludedFields in
// test/site/inventory_test.go, behind its readAWSExcludedFields (the twin of this
// package's readExcludedFixtureKeys). Neither test package may import the other,
// so excludedFixtureKeysRecordTablePath is what holds the two copies to the same
// documents, and TestTheTwoExcludedKeyReadersAcceptAndRejectTheSameRecords runs
// it against this one (review finding on PR #654).
func parseExcludedKeys(document string) ([]excludedKeyRecord, error) {
	var (
		records  []excludedKeyRecord
		keys     int
		prefixes int
		// seen maps a record to the line that wrote it, so a repeat names both
		// lines.
		seen = map[string]int{}
	)

	for number, line := range strings.Split(document, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.Contains(trimmed, "#") {
			return nil, fmt.Errorf("%w: line %d reads %q, want the comment on a line of its own, or the comment is part of the field's name and no field matches it",
				errRecordComment, number+1, trimmed)
		}

		kind, name, ok := strings.Cut(trimmed, ": ")
		if !ok || name == "" {
			return nil, fmt.Errorf("%w: line %d reads %q, want `key: <field>` or `prefix: <prefix>`",
				errRecordMalformed, number+1, trimmed)
		}

		if first, repeated := seen[trimmed]; repeated {
			return nil, fmt.Errorf("%w: line %d repeats line %d's record %q, want one record per field, since a second copy hides the first and leaves unclear which one the two checks read",
				errRecordRepeat, number+1, first, trimmed)
		}

		seen[trimmed] = number + 1

		switch kind {
		case recordKindKey:
			keys++
		case recordKindPrefix:
			prefixes++
		default:
			return nil, fmt.Errorf("%w: line %d reads %q, a record of kind %q, want `key: <field>` or `prefix: <prefix>`",
				errRecordKind, number+1, trimmed, kind)
		}

		records = append(records, excludedKeyRecord{kind: kind, name: name})
	}

	if keys == 0 || prefixes == 0 {
		return nil, fmt.Errorf("%w: it holds %d key(s) and %d prefix(es), want both, since the list is the whole of what both checks exclude and an empty half would let a field the PRD excludes through",
			errRecordEmptyHalf, keys, prefixes)
	}

	return records, nil
}

// readExcludedFixtureKeys reads the shared list of the fields every projection
// and fixture of the guide's search and messaging calls leaves out (PRD #571),
// and hands it to parseExcludedKeys. Its twin is test/site's readAWSExcludedFields
// in test/site/inventory_test.go, which reads the same file for the guide's
// --query with a copy of that parser (review finding on PR #639), and
// excludedFixtureKeysRecordTablePath is the table both run so a document either
// copy accepted or rejected differently from the other fails both (review finding
// on PR #654).
func readExcludedFixtureKeys(t *testing.T) excludedFixtureKeys {
	t.Helper()

	records, err := parseExcludedKeys(readFile(t, filepath.Join(repoRoot, filepath.FromSlash(excludedFixtureKeysPath))))
	if err != nil {
		t.Fatalf("%s: %v", excludedFixtureKeysPath, err)
	}

	var excluded excludedFixtureKeys

	for _, record := range records {
		switch record.kind {
		case recordKindKey:
			excluded.names = append(excluded.names, record.name)
		case recordKindPrefix:
			excluded.prefixes = append(excluded.prefixes, record.name)
		}
	}

	return excluded
}

// excludedKeyRecordRejections maps a rejection reason the shared table names to
// the error parseExcludedKeys returns for it: a document the table rejects as
// "duplicate" is one this parser and test/site's must both reject as
// errRecordRepeat, and so on. A table case naming a reason this map does not hold
// fails the table's test rather than shrinking what the two parsers are held to.
var excludedKeyRecordRejections = map[string]error{
	"malformed":    errRecordMalformed,
	"unknown kind": errRecordKind,
	"comment":      errRecordComment,
	"duplicate":    errRecordRepeat,
	"empty half":   errRecordEmptyHalf,
}

// excludedKeyRecordCase is one case of the shared table of record documents: a
// list both copies of the parser are given, and the outcome each expects of it.
type excludedKeyRecordCase struct {
	// document is the list text both parsers are given.
	document string
	// want is the records the document holds, each spelled as the list spells it
	// and joined by " ; ", or "" for a document both parsers reject.
	want string
	// rejection is the reason both parsers reject the document, one of
	// excludedKeyRecordRejections' keys, or "" for a document both accept.
	rejection string
}

// name names the case for a subtest.
func (tc excludedKeyRecordCase) name() string {
	outcome := "accepted as " + tc.want
	if tc.rejection != "" {
		outcome = "rejected as " + tc.rejection
	}

	return outcome + ": " + strings.ReplaceAll(tc.document, "\n", ", ")
}

// excludedKeyRecordCases returns the shared table of record documents,
// excludedFixtureKeysRecordTablePath: a case per line, `<outcome> | <document>`,
// with `\n` for a newline in the document and the outcome being the records the
// document holds or `rejected: <reason>`. It fails when the table holds no case,
// no accepted document, or no document rejected for one of the reasons above, so
// that a reason the two parsers agree on cannot lose its pin silently.
func excludedKeyRecordCases(t *testing.T) []excludedKeyRecordCase {
	t.Helper()

	var cases []excludedKeyRecordCase

	reasons := map[string]bool{}

	for number, line := range strings.Split(readFile(t, filepath.Join(repoRoot, filepath.FromSlash(excludedFixtureKeysRecordTablePath))), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		outcome, document, ok := strings.Cut(trimmed, " | ")
		if !ok {
			t.Fatalf("%s line %d reads %q, want `<outcome> | <document>`",
				excludedFixtureKeysRecordTablePath, number+1, trimmed)
		}

		tc := excludedKeyRecordCase{document: strings.ReplaceAll(document, `\n`, "\n")}

		if reason, rejected := strings.CutPrefix(outcome, "rejected: "); rejected {
			tc.rejection = reason
			reasons[reason] = true
		} else {
			tc.want = outcome
		}

		cases = append(cases, tc)
	}

	if len(cases) == 0 {
		t.Fatalf("%s holds no case, want one per record document the two parsers are held to",
			excludedFixtureKeysRecordTablePath)
	}

	if !slices.ContainsFunc(cases, func(tc excludedKeyRecordCase) bool { return tc.rejection == "" }) {
		t.Errorf("%s holds no accepted document, want one: a table of rejected documents alone says nothing about what the two parsers accept",
			excludedFixtureKeysRecordTablePath)
	}

	for reason := range excludedKeyRecordRejections {
		if !reasons[reason] {
			t.Errorf("%s holds no document rejected as %q, want one: a reason no case exercises leaves the two parsers' agreement on it unpinned",
				excludedFixtureKeysRecordTablePath, reason)
		}
	}

	return cases
}

// TestTheTwoExcludedKeyReadersAcceptAndRejectTheSameRecords pins that this
// package's reader of the shared excluded-key list and test/site's agree (review
// finding on PR #654). parseExcludedKeys and test/site's parseAWSExcludedFields,
// in test/site/inventory_test.go, are hand-kept copies of one parser, since
// neither test package may import the other, and one copy that grew to accept a
// document the other rejects would let one half of PRD #571's promise hold while
// the other let a field through. Both run excludedFixtureKeysRecordTablePath,
// each against its own copy, and a document the table holds is accepted as the
// records it lists, or rejected for the reason it names, in both.
func TestTheTwoExcludedKeyReadersAcceptAndRejectTheSameRecords(t *testing.T) {
	t.Parallel()

	for _, tc := range excludedKeyRecordCases(t) {
		t.Run(tc.name(), func(t *testing.T) {
			t.Parallel()

			records, err := parseExcludedKeys(tc.document)

			if tc.rejection != "" {
				reason, known := excludedKeyRecordRejections[tc.rejection]
				if !known {
					t.Fatalf("%s rejects a document as %q, a reason this test does not know: add it to excludedKeyRecordRejections, and to test/site's copy of that map",
						excludedFixtureKeysRecordTablePath, tc.rejection)
				}

				if !errors.Is(err, reason) {
					t.Fatalf("parseExcludedKeys reads %q as %v, want it rejected as %q (%v): the shared table's document is one both parsers must reject, and for the same reason",
						tc.document, err, tc.rejection, reason)
				}

				return
			}

			if err != nil {
				t.Fatalf("parseExcludedKeys rejects %q as %v, want the records %q: the shared table's document is one both parsers must accept",
					tc.document, err, tc.want)
			}

			spellings := make([]string, 0, len(records))
			for _, record := range records {
				spellings = append(spellings, record.spelling())
			}

			if got := strings.Join(spellings, " ; "); got != tc.want {
				t.Errorf("parseExcludedKeys reads %q as %q, want %q: the records the document holds, in the list's order",
					tc.document, got, tc.want)
			}
		})
	}
}

// TestInventoryGuideSearchAndKafkaFixturesCarryOnlyTheFieldsTheProgramReads pins
// that each new call's recorded output holds the fields its jq program reads and
// no other, and none of the fields PRD #571 excludes.
func TestInventoryGuideSearchAndKafkaFixturesCarryOnlyTheFieldsTheProgramReads(t *testing.T) {
	t.Parallel()

	// The list is read once, before the subtests: a malformed one is the list's
	// failure, not each fixture's, and reading it per subtest would fail every
	// one of them for the same reason.
	excluded := readExcludedFixtureKeys(t)

	for _, tc := range searchAndKafkaFixtures {
		t.Run(tc.call, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(repoRoot, filepath.FromSlash(tc.file))

			// A call the AWS CLI does not page writes one JSON value per page into the
			// file, so every value the fixture holds is checked, not the first alone.
			for _, value := range readFixtureValues(t, path, tc.call) {
				for _, problem := range fixtureKeyProblems(value, tc.keys, excluded) {
					t.Errorf("%s: %s", tc.file, problem)
				}
			}
		})
	}
}

// describeDomainsBatchLine finds the guide's `xargs -r -n <n>` line that hands
// `aws opensearch describe-domains` the names of one batch.
var describeDomainsBatchLine = regexp.MustCompile(`xargs -r -n ([0-9]+) aws opensearch describe-domains `)

// describeDomainsBatch returns the number of domain names the guide asks
// `aws opensearch describe-domains` for at a time, read from the guide's own
// `xargs -r -n <n>` line (docs/guide/inventory.md) rather than written out here:
// the recording holds one JSON value per batch, so a change of -n in the guide
// must break this check with it instead of leaving a fixture the guide no longer
// writes (PRD #571, review finding on PR #639).
func describeDomainsBatch(t *testing.T) int {
	t.Helper()

	const guidePath = "docs/guide/inventory.md"

	matches := describeDomainsBatchLine.FindAllStringSubmatch(readFile(t, filepath.Join(repoRoot, guidePath)), -1)
	if len(matches) != 1 {
		t.Fatalf("%s holds %d line(s) of `xargs -r -n <n> aws opensearch describe-domains`, want exactly one: this check reads the batch size the guide asks describe-domains for, and a second line would leave which one is unclear",
			guidePath, len(matches))
	}

	batch, err := strconv.Atoi(matches[0][1])
	if err != nil {
		t.Fatalf("%s asks describe-domains for %q domain(s) at a time, want a number: %v",
			guidePath, matches[0][1], err)
	}

	if batch < 1 {
		t.Fatalf("%s asks describe-domains for %d domain(s) at a time, want at least 1: a batch of none asks the call for no domain and writes no value of the recording",
			guidePath, batch)
	}

	return batch
}

// TestInventoryGuideDescribeDomainsFixtureRecordsEveryBatch pins the shape of a
// describe-domains recording (PRD #571, review finding on PR #639): the guide
// hands the call the names list-domain-names returned, the number of names its
// `xargs -r -n` line names at a time, and redirects every batch to
// `opensearch-domains.json`, so the file holds one JSON value per batch of those
// names, in order, and each value holds exactly the names of that batch. A
// fixture of one value would record a command the guide does not run, and a
// check that read only the first value would miss every domain of every later
// batch.
//
// A value's membership is what this check pins, not the order inside it: the
// guide fixes the batches and their order, but AWS documents no order for
// DescribeDomains' DomainStatusList, so a recording may hold a batch's names in
// any order.
func TestInventoryGuideDescribeDomainsFixtureRecordsEveryBatch(t *testing.T) {
	t.Parallel()

	names, err := filepath.Glob(filepath.Join(repoRoot, "test/e2e/testdata/inventory-guide/aws/*/opensearch-domain-names.json"))
	if err != nil {
		t.Fatalf("glob the recorded list-domain-names output: %v", err)
	}

	if len(names) == 0 {
		t.Fatalf("test/e2e/testdata/inventory-guide/aws/*/opensearch-domain-names.json matched no recorded output; the fixtures are missing")
	}

	slices.Sort(names)

	batch := describeDomainsBatch(t)

	batches := 0

	for _, namesPath := range names {
		listed := readRecordedDomainNames(t, namesPath)

		// One value per batch of names the guide asked the call for.
		want := (len(listed) + batch - 1) / batch

		batches += want

		t.Run(filepath.Base(filepath.Dir(namesPath)), func(t *testing.T) {
			t.Parallel()

			domainsPath := filepath.Join(filepath.Dir(namesPath), "opensearch-domains.json")

			pages := readRecordedDomainPages(t, domainsPath)

			if len(pages) != want {
				t.Errorf("%s holds %d JSON value(s), want %d: %s names %d domain(s) and the guide asks describe-domains for %d at a time, so it runs the call %d time(s) and every batch is a value of its own in the file",
					domainsPath, len(pages), want, namesPath, len(listed), batch, want)
			}

			// The batches keep the names in the order list-domain-names returned them:
			// value i holds names i*batch to the next batch's first name, and none
			// other, whatever order the call answered them in.
			for i, page := range pages {
				start := min(i*batch, len(listed))
				end := min(start+batch, len(listed))

				if !sameDomainBatch(page, listed[start:end]) {
					t.Errorf("%s value %d describes the domain(s) %q, want the names %q: the guide hands describe-domains the names list-domain-names returned, %d at a time, in that order, so the value of batch %d is names %d to %d, each of them once",
						domainsPath, i+1, page, listed[start:end], batch, i+1, start+1, end)
				}
			}
		})
	}

	if batches < 2 {
		t.Errorf("the recorded describe-domains output holds %d value(s) over every region, want at least 2: the guide's `xargs -r -n %d` writes one value per batch, and a fixture of one value cannot show that the program reads more than the first (review finding on PR #639)",
			batches, batch)
	}
}

// sameDomainBatch reports whether two batches of domain names hold the same
// names, each once, whatever their order: the guide fixes which names a batch
// carries, but AWS documents no order for DescribeDomains' DomainStatusList, so
// the order inside a batch is the call's to choose.
func sameDomainBatch(got, want []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want)))
}

// readRecordedDomainNames returns the names a recorded list-domain-names output
// holds, over every JSON value of the file.
func readRecordedDomainNames(t *testing.T, path string) []string {
	t.Helper()

	var names []string

	for _, value := range readFixtureValues(t, path, "aws opensearch list-domain-names") {
		document, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s holds a value that is not a list-domain-names response: %T", path, value)
		}

		for _, field := range asSlice(document["DomainNames"]) {
			entry, ok := field.(map[string]any)
			if !ok {
				t.Fatalf("%s holds a DomainNames entry that is no object: %T", path, field)
			}

			name, ok := entry["DomainName"].(string)
			if !ok {
				t.Fatalf("%s holds a DomainNames entry without a DomainName: %v", path, field)
			}

			names = append(names, name)
		}
	}

	if len(names) == 0 {
		t.Fatalf("%s names no domain, so this check read nothing", path)
	}

	return names
}

// readRecordedDomainPages returns the domain names of each JSON value of a
// recorded describe-domains output, one slice per value, in the file's order.
func readRecordedDomainPages(t *testing.T, path string) [][]string {
	t.Helper()

	var pages [][]string

	for _, value := range readFixtureValues(t, path, "aws opensearch describe-domains") {
		document, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s holds a value that is not a describe-domains response: %T", path, value)
		}

		var page []string

		for _, field := range asSlice(document["DomainStatusList"]) {
			entry, ok := field.(map[string]any)
			if !ok {
				t.Fatalf("%s holds a DomainStatusList entry that is no object: %T", path, field)
			}

			name, ok := entry["DomainName"].(string)
			if !ok {
				t.Fatalf("%s holds a DomainStatusList entry without a DomainName: %v", path, field)
			}

			page = append(page, name)
		}

		pages = append(pages, page)
	}

	return pages
}

// asSlice returns the entries of a decoded JSON array, and none of a field the
// document does not hold.
func asSlice(value any) []any {
	entries, _ := value.([]any)

	return entries
}

// readFixtureValues decodes every JSON value a recorded fixture holds. The guide
// has no CLI paginator for `opensearchserverless list-collections`, so its
// command appends each page to the file as a value of its own: a fixture the
// command wrote holds as many values as pages the call took.
func readFixtureValues(t *testing.T, path, call string) []any {
	t.Helper()

	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the %s fixture: %v", call, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(document))

	var values []any

	for {
		var value any

		err := decoder.Decode(&value)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("the %s fixture %s is not JSON: %v", call, path, err)
		}

		values = append(values, value)
	}

	if len(values) == 0 {
		t.Fatalf("the %s fixture %s holds no JSON value, so this check read nothing", call, path)
	}

	return values
}

// TestInventoryGuideSearchAndKafkaFixtureCheckFindsAPlantedKey pins that the
// check above reads the fixtures at every depth: a planted field it must report
// is reported, both as a key the call does not read and as an excluded field,
// so a fixture that grew a key after this test was written cannot pass on a
// check that reads only the top level.
func TestInventoryGuideSearchAndKafkaFixtureCheckFindsAPlantedKey(t *testing.T) {
	t.Parallel()

	planted := map[string]any{
		"DomainStatusList": []any{
			map[string]any{
				"DomainName":               "shop-search",
				"AccessPolicies":           "{\"Version\":\"2012-10-17\"}",
				"DomainEndpointOptions":    map[string]any{"EnforceHTTPS": true},
				"BootstrapBrokerStringTls": "b-1.shop.events.kafka.eu-central-1.amazonaws.com:9094",
			},
		},
	}

	problems := fixtureKeyProblems(planted, []string{"DomainStatusList", "DomainName"}, readExcludedFixtureKeys(t))

	want := []string{
		"the fixture holds the key \"AccessPolicies\", which PRD #571 excludes from every projection",
		"the fixture holds the key \"AccessPolicies\", which no jq program of the call reads",
		"the fixture holds the key \"BootstrapBrokerStringTls\", which PRD #571 excludes from every projection",
		"the fixture holds the key \"BootstrapBrokerStringTls\", which no jq program of the call reads",
		"the fixture holds the key \"DomainEndpointOptions\", which PRD #571 excludes from every projection",
		"the fixture holds the key \"DomainEndpointOptions\", which no jq program of the call reads",
		// A key of a nested object is read as well, whatever its depth.
		"the fixture holds the key \"EnforceHTTPS\", which no jq program of the call reads",
	}

	sort.Strings(want)

	if !slices.Equal(problems, want) {
		t.Errorf("fixtureKeyProblems reported the planted keys wrongly:\nwant:\n%s\ngot:\n%s",
			strings.Join(want, "\n"), strings.Join(problems, "\n"))
	}
}

// fixtureKeyProblems returns, sorted and without duplicates, what is wrong with
// the keys of a recorded fixture: a key no jq program of the call reads, and a
// key the shared list excludes, at any depth.
func fixtureKeyProblems(value any, allowed []string, excluded excludedFixtureKeys) []string {
	var problems []string

	var walk func(value any)

	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				if excluded.excludes(key) {
					problems = append(problems, "the fixture holds the key "+quotedKey(key)+", which PRD #571 excludes from every projection")
				}

				if !slices.Contains(allowed, key) {
					problems = append(problems, "the fixture holds the key "+quotedKey(key)+", which no jq program of the call reads")
				}

				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}

	walk(value)

	slices.Sort(problems)

	return slices.Compact(problems)
}

// quotedKey renders a key for a failure message.
func quotedKey(key string) string {
	return `"` + key + `"`
}
