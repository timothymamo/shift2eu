package examples_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
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
// classification: the call as the guide spells it, the fixture its --query
// output is recorded in, and the keys its jq program reads.
type guideCallFixture struct {
	// call names the CLI call, for a failure message.
	call string
	// file is the fixture, relative to the module root.
	file string
	// keys are the fields the call's program reads: a key outside this list is
	// a field the program does not read, and so one the projection drops.
	keys []string
}

// searchAndKafkaFixtures are the guide's new AWS calls of PRD #571, with the
// fixture of each and the fields its program reads. The fixtures hold the
// --query output, so a key here is a field the guide's operator saw in the raw
// output before deleting it. The guide runs a call once per region, so a call
// recorded in two regions has an entry per region, and the two must read the
// same fields.
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
}

// excludedFixtureKeysPath is the one list of the fields PRD #571 excludes from
// every projection and fixture of the guide's search and messaging calls, shared
// with test/site's projection check so the two cannot drift apart (review finding
// on PR #639). It sits in this package's testdata directory, and test/site reads it
// by the same repository-relative path.
const excludedFixtureKeysPath = "test/examples/testdata/inventory_guide_excluded_keys.txt"

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

// holdInFixture reports whether the key is one the shared list excludes.
func (e excludedFixtureKeys) holdInFixture(key string) bool {
	return slices.Contains(e.names, key) ||
		slices.ContainsFunc(e.prefixes, func(prefix string) bool { return strings.HasPrefix(key, prefix) })
}

// readExcludedFixtureKeys reads the shared list of the fields every projection
// and fixture of the guide's search and messaging calls leaves out (PRD #571),
// which test/site's check of the guide's --query reads too (review finding on PR
// #639). A record is `key: <field>` or `prefix: <field prefix>`; comments and
// blank lines are no record. A malformed record fails the test: a list this
// reader skipped would let a field the PRD excludes through both checks.
func readExcludedFixtureKeys(t *testing.T) excludedFixtureKeys {
	t.Helper()

	document, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(excludedFixtureKeysPath)))
	if err != nil {
		t.Fatalf("read the shared excluded-key list: %v", err)
	}

	var excluded excludedFixtureKeys

	for number, line := range strings.Split(string(document), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		kind, name, ok := strings.Cut(trimmed, ": ")
		if !ok || name == "" {
			t.Fatalf("%s line %d reads %q, want `key: <field>` or `prefix: <prefix>`",
				excludedFixtureKeysPath, number+1, trimmed)
		}

		switch kind {
		case "key":
			excluded.names = append(excluded.names, name)
		case "prefix":
			excluded.prefixes = append(excluded.prefixes, name)
		default:
			t.Fatalf("%s line %d reads %q, a record of kind %q: want `key: <field>` or `prefix: <prefix>`",
				excludedFixtureKeysPath, number+1, trimmed, kind)
		}
	}

	if len(excluded.names) == 0 || len(excluded.prefixes) == 0 {
		t.Fatalf("%s holds %d key(s) and %d prefix(es), want both: the list is the whole of what both checks exclude, so an empty half would let a field the PRD excludes through",
			excludedFixtureKeysPath, len(excluded.names), len(excluded.prefixes))
	}

	return excluded
}

// TestInventoryGuideSearchAndKafkaFixturesCarryOnlyTheFieldsTheProgramReads pins
// that each new call's recorded output holds the fields its jq program reads and
// no other, and none of the fields PRD #571 excludes.
func TestInventoryGuideSearchAndKafkaFixturesCarryOnlyTheFieldsTheProgramReads(t *testing.T) {
	t.Parallel()

	for _, tc := range searchAndKafkaFixtures {
		t.Run(tc.call, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(repoRoot, filepath.FromSlash(tc.file))

			excluded := readExcludedFixtureKeys(t)

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

// describeDomainsBatch is the number of domain names the guide asks
// `aws opensearch describe-domains` for at a time (`xargs -r -n 5`), which is why
// a region of more than five domains records several JSON values in the one file
// the command redirects every batch to.
const describeDomainsBatch = 5

// TestInventoryGuideDescribeDomainsFixtureRecordsEveryBatch pins the shape of a
// describe-domains recording (PRD #571, review finding on PR #639): the guide
// hands the call five names at a time with `xargs -r -n 5` and redirects every
// batch to `opensearch-domains.json`, so a region of more than five domains
// holds one JSON value per batch. A fixture of one value would record a command
// the guide does not run, and a check that read only the first value would miss
// every domain of every later batch.
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

	batches := 0

	for _, namesPath := range names {
		listed := readRecordedDomainNames(t, namesPath)

		// One value per batch of names the guide asked the call for.
		want := (len(listed) + describeDomainsBatch - 1) / describeDomainsBatch

		batches += want

		t.Run(filepath.Base(filepath.Dir(namesPath)), func(t *testing.T) {
			t.Parallel()

			domainsPath := filepath.Join(filepath.Dir(namesPath), "opensearch-domains.json")

			pages := readRecordedDomainPages(t, domainsPath)

			if len(pages) != want {
				t.Errorf("%s holds %d JSON value(s), want %d: %s names %d domain(s) and the guide asks describe-domains for %d at a time, so it runs the call %d time(s) and every batch is a value of its own in the file",
					domainsPath, len(pages), want, namesPath, len(listed), describeDomainsBatch, want)
			}

			described := 0

			for i, page := range pages {
				if len(page) == 0 {
					t.Errorf("%s value %d holds no domain, want at least one: the guide writes a value per batch of names it asked for, so an empty value records a call the guide never made",
						domainsPath, i+1)
				}

				if len(page) > describeDomainsBatch {
					t.Errorf("%s value %d holds %d domain(s), want at most %d: the guide asks the call for %d names at a time",
						domainsPath, i+1, len(page), describeDomainsBatch, describeDomainsBatch)
				}

				described += len(page)
			}

			if described != len(listed) {
				t.Errorf("%s describes %d domain(s) over its %d value(s) and %s names %d: the guide asks describe-domains for the names list-domain-names returned, so every batch is described once",
					domainsPath, described, len(pages), namesPath, len(listed))
			}
		})
	}

	if batches < 2 {
		t.Errorf("the recorded describe-domains output holds %d value(s) over every region, want at least 2: the guide's `xargs -r -n %d` writes one value per batch, and a fixture of one value cannot show that the program reads more than the first (review finding on PR #639)",
			batches, describeDomainsBatch)
	}
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
			name, ok := field.(map[string]any)["DomainName"].(string)
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
			name, ok := field.(map[string]any)["DomainName"].(string)
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
				if excluded.holdInFixture(key) {
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
