package examples_test

import (
	"encoding/json"
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
		},
	},
	{
		call: "aws opensearch describe-domains in eu-west-1",
		file: "test/e2e/testdata/inventory-guide/aws/eu-west-1/opensearch-domains.json",
		keys: []string{
			"DomainStatusList", "DomainName", "ARN", "EngineVersion", "ClusterConfig",
			"InstanceType", "InstanceCount", "ZoneAwarenessEnabled", "MultiAZWithStandbyEnabled",
			"EBSOptions", "EBSEnabled", "VolumeSize", "EncryptionAtRestOptions", "Enabled",
		},
	},
	{
		call: "aws opensearchserverless list-collections",
		file: "test/e2e/testdata/inventory-guide/aws/eu-central-1/opensearchserverless-collections.json",
		keys: []string{"collectionSummaries", "id", "name"},
	},
	{
		call: "aws opensearchserverless batch-get-collection",
		file: "test/e2e/testdata/inventory-guide/aws/eu-central-1/opensearchserverless-batch-collections.json",
		keys: []string{"collectionDetails", "name", "arn", "type", "standbyReplicas"},
	},
	{
		call: "aws kafka list-clusters-v2",
		file: "test/e2e/testdata/inventory-guide/aws/eu-central-1/kafka-clusters-v2.json",
		keys: []string{
			"ClusterInfoList", "ClusterName", "ClusterType", "ClusterArn", "Provisioned", "NumberOfBrokerNodes",
			"BrokerNodeGroupInfo", "InstanceType", "ClientSubnets", "StorageInfo", "EbsStorageInfo",
			"VolumeSize",
		},
	},
}

// excludedFixtureKeys are the fields no fixture of a new call may carry (PRD
// #571, *Only the fields the program reads*): an access policy names the
// account's principals, a security option its settings, and an endpoint or a
// bootstrap address a host with the account's name in it.
var excludedFixtureKeys = []string{
	"AccessPolicies",
	"AdvancedSecurityOptions",
	"CognitoOptions",
	"Endpoint",
	"Endpoints",
	"DomainEndpointOptions",
	"collectionEndpoint",
	"dashboardEndpoint",
	"bootstrapAddress",
	"osProfile",
	"connectivityEndpoints",
}

// excludedFixtureKeyPrefixes are the fields no fixture may carry whatever their
// suffix: a cluster's ZooKeeper connection strings and its broker addresses,
// which name the cluster's hosts.
var excludedFixtureKeyPrefixes = []string{"ZookeeperConnectString", "BootstrapBroker"}

// TestInventoryGuideSearchAndKafkaFixturesCarryOnlyTheFieldsTheProgramReads pins
// that each new call's recorded output holds the fields its jq program reads and
// no other, and none of the fields PRD #571 excludes.
func TestInventoryGuideSearchAndKafkaFixturesCarryOnlyTheFieldsTheProgramReads(t *testing.T) {
	t.Parallel()

	for _, tc := range searchAndKafkaFixtures {
		t.Run(tc.call, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(repoRoot, filepath.FromSlash(tc.file))

			document, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the %s fixture: %v", tc.call, err)
			}

			var value any
			if err := json.Unmarshal(document, &value); err != nil {
				t.Fatalf("the %s fixture is not JSON: %v", tc.call, err)
			}

			problems := fixtureKeyProblems(value, tc.keys)

			for _, problem := range problems {
				t.Errorf("%s: %s", tc.file, problem)
			}
		})
	}
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

	problems := fixtureKeyProblems(planted, []string{"DomainStatusList", "DomainName"})

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
// key PRD #571 excludes, at any depth.
func fixtureKeyProblems(value any, allowed []string) []string {
	var problems []string

	var walk func(value any)

	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				if slices.ContainsFunc(excludedFixtureKeys, func(excluded string) bool {
					return key == excluded
				}) || slices.ContainsFunc(excludedFixtureKeyPrefixes, func(prefix string) bool {
					return strings.HasPrefix(key, prefix)
				}) {
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
