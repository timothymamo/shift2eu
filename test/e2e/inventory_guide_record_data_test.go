//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// This file pins the promise the guide makes about DNS zones (PRD #581, *The
// guide reads no zone contents*): a zone's record names and values are the
// user's data, so a fixture, a collected sample or a program's output may hold a
// zone's name and its signed state, never a record. A record's name names an
// internal host or a verification token, a record's values name the addresses it
// answers with, a DNSSEC key is the zone's private signing material and its
// delegation signer record is what the registrar publishes.
//
// The guide reduces every record-set call to a count per zone and every DNSSEC
// read to the signed state, so nothing of a zone's contents reaches the files
// the guide collects, the inventory a program writes, or the fixtures that stand
// for them. The check walks every recorded output of every source the guide
// covers (the AWS fixtures per region, GCP's and Azure's, and the golden
// inventories the programs wrote for them), and the AWS and GCP programs'
// output run as the guide runs them, so a fixture that recorded one extra field,
// or a program that read one extra field through, fails here rather than in a
// reader's report.

// recordKeys are the fields no fixture, collected sample or program output may
// hold, at any depth (PRD #581, *Inventory guide*): the record sets a zone
// answers with and their routing, and the keys DNSSEC signs it with. `name` is
// not among them: every zone, and every GCP asset row, has one.
var recordKeys = []string{
	// A record set's data: its values, the alias it points at, its routing and
	// the health check that routing answers on.
	"rrdatas", "signatureRrdatas", "healthCheckedTargets", "ResourceRecords",
	"AliasTarget", "SetIdentifier",
	// The fields Route 53 writes a record set's routing with: a weight, a
	// failover role, a geolocation or a geoproximity, an IP-based CIDR
	// collection, the health check a routing answers on, a traffic policy and a
	// multivalue answer, and the TTL the record set's values live for.
	"HealthCheckId", "TrafficPolicyInstanceId", "MultiValueAnswer", "Weight",
	"Failover", "GeoLocation", "GeoProximityLocation", "CidrRoutingConfig", "TTL",
	// A signed zone's keys: Route 53's, Cloud DNS's and Azure DNS's spelling of
	// them, and the capitalised spellings of a zone's key-signing keys, of its
	// DNSKEY and of the delegation signer record its registrar publishes.
	"KeySigningKeys", "signingKeys", "publicKey", "delegationSignerInfo",
	"PublicKey", "DNSKEYRecord", "DSRecord", "DigestValue",
	// The fields Cloud DNS writes a record set's own contents and its routing
	// with: the TTL its values live for, and the routing policy itself, which
	// names the health check, the geolocation or the weighted answers it routes
	// by. The guide's record-set call keeps neither (PRD #581, milestone 3).
	"ttl", "routingPolicy",
}

// recordValues are record names of the guide's synthetic zones (PRD #581's
// fixtures, the zones under example.com). A value equal to one of them is a
// record the guide read, whatever key it sits under: the zone's own name is no
// record, so the zones themselves are not listed here.
var recordValues = []string{
	"www.example.com.",
	"api.example.com.",
	"mail.example.com.",
	"_acme-challenge.example.com.",
	"www.shop.example.com.",
	"api.shop.example.com.",
}

// TestInventoryGuideHoldsNoRecordData walks every recorded output the guide's
// tests use, and the AWS program's output, and fails on a field or a value that
// belongs to a zone's contents rather than to the inventory (PRD #581).
func TestInventoryGuideHoldsNoRecordData(t *testing.T) {
	t.Parallel()

	scanned := scannedGuideFiles(t)

	// The fixtures of the milestones that add DNS zones are what this check is
	// about: without them it would walk a guide that records no zone at all and
	// pass for the wrong reason.
	for _, fixture := range guideZoneFixtures {
		if !slices.ContainsFunc(scanned, func(file string) bool { return filepath.Base(file) == fixture.file }) {
			t.Errorf("%s holds no %s, want the fixture %s writes for %s: this check walks the guide's recorded output, and the zone calls' own output is not among it (PRD #581, milestone %s)",
				inventoryGuideDir, fixture.file, fixture.call, fixture.source, fixture.milestone)
		}
	}

	for _, path := range scanned {
		relative, err := filepath.Rel(inventoryGuideDir, path)
		if err != nil {
			t.Fatalf("relative path of %s: %v", path, err)
		}

		for _, value := range decodeGuideValues(t, path) {
			for _, problem := range recordDataProblems(value) {
				t.Errorf("%s/%s: %s", inventoryGuideDir, filepath.ToSlash(relative), problem)
			}
		}
	}

	for _, tc := range zoneProgramCases {
		assertNoRecordDataInProgramOutput(t, tc)
	}
}

// guideZoneFixture is one file a guide milestone's DNS zone calls write, which
// the scan above has to find: a check that walks output holding no zone would
// report every zone's contents clean for the wrong reason.
type guideZoneFixture struct {
	// file is the fixture's name, as the guide's command writes it under the
	// source's directory.
	file string
	// source is the source whose calls write it.
	source string
	// call is the command that writes it.
	call string
	// milestone is the PRD #581 milestone that adds it.
	milestone string
}

// guideZoneFixtures are the fixtures PRD #581's AWS and GCP milestones write:
// each source's zone list, and the per-zone answers reduced to no record.
var guideZoneFixtures = []guideZoneFixture{
	{file: "route53-hosted-zones.json", source: "aws", call: "aws route53 list-hosted-zones", milestone: "2"},
	{file: "route53-dnssec.json", source: "aws", call: "aws route53 get-dnssec", milestone: "2"},
	{file: "route53-routed-record-sets.json", source: "aws", call: "aws route53 list-resource-record-sets", milestone: "2"},
	// Cloud DNS: the managed zones per project, with the project the guide's jq
	// filter adds to each entry, and the per-zone count of routed record sets.
	{file: "dns-managed-zones.json", source: "gcp", call: "gcloud dns managed-zones list", milestone: "3"},
	{file: "dns-routed-record-sets.json", source: "gcp", call: "gcloud dns record-sets list", milestone: "3"},
}

// zoneProgramCase is one source whose program writes zones of its own, the raw
// type of the resource each public zone becomes, and the recorded output the
// program reads.
type zoneProgramCase struct {
	// source is the program's name and the directory its recorded output sits in.
	source string
	// pattern is the glob, relative to this package, of the recorded CLI output:
	// the AWS calls are per region, so its pattern has one more level.
	pattern string
	// rawType is the source's own type for a zone, which a network.dns resource
	// keeps.
	rawType string
	// milestone is the PRD #581 milestone that adds the classification.
	milestone string
}

// zoneProgramCases are the sources whose DNS zones the guide classifies today.
var zoneProgramCases = []zoneProgramCase{
	{source: "aws", pattern: inventoryGuideDir + "/aws/*/*.json", rawType: "AWS::Route53::HostedZone", milestone: "2"},
	{source: "gcp", pattern: inventoryGuideDir + "/gcp/*.json", rawType: "dns.googleapis.com/ManagedZone", milestone: "3"},
}

// assertNoRecordDataInProgramOutput pins the other half of the promise for one
// source: the program, run as the guide runs it on every recorded input, writes
// an inventory that holds no zone's contents either. The golden file it writes is
// one of the files the walk above reads; this runs the program itself, so a
// golden gone stale cannot pass for the program's output.
func assertNoRecordDataInProgramOutput(t *testing.T, tc zoneProgramCase) {
	t.Helper()

	inputs, err := filepath.Glob(tc.pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", tc.pattern, err)
	}

	if len(inputs) == 0 {
		t.Fatalf("%s matched no recorded CLI output; the fixtures are missing", tc.pattern)
	}

	slices.Sort(inputs)

	output := runJQ(t, jqPath(t), tc.source, inputs)

	for _, value := range decodeGuideBytes(t, "the "+tc.source+" program's output", output) {
		for _, problem := range recordDataProblems(value) {
			t.Errorf("the %s program's output: %s", tc.source, problem)
		}
	}

	// The output has to hold the zones the fixtures record, or this check scanned
	// an output the zone calls never reached: a resource of another guide
	// milestone would pass here while the zone's own fields went unread.
	doc := decodeInventory(t, output)

	if !slices.ContainsFunc(doc.Resources, func(resource inventoryResource) bool {
		return resource.Category == "network.dns" && resource.RawType == tc.rawType
	}) {
		t.Errorf("the %s program's output holds no resource of category network.dns with rawType %s, want each public zone of %s/%s's zone list as one (PRD #581, milestone %s): this check scans the zone's own output",
			tc.source, tc.rawType, inventoryGuideDir, tc.source, tc.milestone)
	}
}

// scannedGuideFiles returns every regular file under the guide's recorded output,
// all sources' fixtures and the golden inventories the programs wrote, in a
// stable order.
func scannedGuideFiles(t *testing.T) []string {
	t.Helper()

	var files []string

	err := filepath.WalkDir(inventoryGuideDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !entry.IsDir() {
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", inventoryGuideDir, err)
	}

	if len(files) == 0 {
		t.Fatalf("%s holds no file, so this check read nothing", inventoryGuideDir)
	}

	slices.Sort(files)

	return files
}

// decodeGuideValues decodes every JSON value a recorded output holds: the guide
// appends a page of a call that has no CLI paginator as a value of its own.
func decodeGuideValues(t *testing.T, path string) []any {
	t.Helper()

	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return decodeGuideBytes(t, path, document)
}

// decodeGuideBytes decodes every JSON value of a document.
func decodeGuideBytes(t *testing.T, name string, document []byte) []any {
	t.Helper()

	decoder := json.NewDecoder(bytes.NewReader(document))

	var values []any

	for {
		var value any

		err := decoder.Decode(&value)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("%s is not JSON: %v", name, err)
		}

		values = append(values, value)
	}

	if len(values) == 0 {
		t.Fatalf("%s holds no JSON value, so this check read nothing", name)
	}

	return values
}

// recordDataProblems returns, sorted and without duplicates, what a decoded
// document holds of a zone's contents: a field of recordKeys at any depth, and a
// string value equal to one of recordValues.
func recordDataProblems(value any) []string {
	var problems []string

	var walk func(value any)

	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				if slices.Contains(recordKeys, key) {
					problems = append(problems, "the field "+strconv.Quote(key)+
						" is a DNS zone's contents, want a zone's name and its signed state only: the guide's record-set calls keep a count per zone, never a record (PRD #581)")
				}

				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case string:
			if slices.Contains(recordValues, typed) {
				problems = append(problems, "the value "+strconv.Quote(typed)+
					" names a DNS record of the guide's synthetic zones, want a zone's own name at most: a record name names the user's hosts (PRD #581)")
			}
		}
	}

	walk(value)

	slices.Sort(problems)

	return slices.Compact(problems)
}

// TestInventoryGuideRecordDataCheckFindsAPlantedRecord pins that the check above
// can fail: the scanner reports a record set's field at any depth and a record's
// name as a value, so a clean report is that nothing is there rather than that
// nothing is looked for.
func TestInventoryGuideRecordDataCheckFindsAPlantedRecord(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document string
		// want is the text the problem has to name, "" for a document this check
		// has nothing to report about.
		want string
	}{
		{
			name:     "a record set's values",
			document: `{"HostedZones": [{"Name": "shop.example.com.", "ResourceRecords": [{"Value": "192.0.2.10"}]}]}`,
			want:     `"ResourceRecords"`,
		},
		{
			name:     "a record's values under another key",
			document: `{"zones": [{"Name": "example.com.", "records": [{"rrdatas": ["192.0.2.10"]}]}]}`,
			want:     `"rrdatas"`,
		},
		{
			name:     "a signed zone's key material",
			document: `{"Id": "Z0123456789ABCDEFGHIK", "Status": {"ServeSignature": "SIGNING", "KeySigningKeys": [{"PublicKey": "..."}]}}`,
			want:     `"KeySigningKeys"`,
		},
		{
			name:     "a record set's routing",
			document: `{"Id": "/hostedzone/Z0123456789ABCDEFGHIJ", "HealthCheckId": "01234567-89ab-cdef-0123-456789abcdef", "Weight": 10}`,
			want:     `"HealthCheckId"`,
		},
		{
			name:     "a key-signing key's DNSSEC record",
			document: `{"Id": "Z0123456789ABCDEFGHIK", "Status": {"ServeSignature": "SIGNING", "KeySigningKeys": [{"DNSKEYRecord": "257 3 13 ...", "DSRecord": "2371 13 2 ...", "DigestValue": "..."}]}}`,
			want:     `"DNSKEYRecord"`,
		},
		{
			name:     "a record's name as a value",
			document: `{"Id": "Z0123456789ABCDEFGHIL", "Name": "shop-api.example.com.", "Note": "api.shop.example.com."}`,
			want:     `"api.shop.example.com."`,
		},
		{
			name:     "a zone's name and its signed state",
			document: `{"Id": "/hostedzone/Z0123456789ABCDEFGHIK", "Name": "example.com.", "ServeSignature": "SIGNING", "RoutedRecordSets": 0}`,
			want:     "",
		},
		{
			name:     "a Cloud DNS record set's contents",
			document: `{"kind": "dns#resourceRecordSet", "name": "api.shop.example.com.", "type": "A", "ttl": 300, "rrdatas": ["192.0.2.10"]}`,
			want:     `"ttl"`,
		},
		{
			name:     "a Cloud DNS record set's routing policy",
			document: `{"kind": "dns#resourceRecordSet", "name": "shop-api.example.com.", "type": "A", "routingPolicy": {"wrrPolicy": {}}}`,
			want:     `"routingPolicy"`,
		},
		{
			name:     "a Cloud DNS zone's number, name, visibility and signed state",
			document: `{"kind": "dns#managedZone", "id": "1234567890123456789", "name": "shop-zone", "visibility": "public", "dnssecConfig": {"state": "on"}, "project": "example-project"}`,
			want:     "",
		},
		{
			name:     "a Cloud DNS zone's routed record-set count",
			document: `{"project": "example-project", "id": "1234567890123456789", "routedRecordSets": 3}`,
			want:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var document any
			if err := json.Unmarshal([]byte(tc.document), &document); err != nil {
				t.Fatalf("the planted document is not JSON: %v", err)
			}

			problems := recordDataProblems(document)

			if tc.want == "" {
				if len(problems) != 0 {
					t.Errorf("the clean document is reported as holding a zone's contents: %s", strings.Join(problems, "; "))
				}

				return
			}

			if !slices.ContainsFunc(problems, func(problem string) bool { return strings.Contains(problem, tc.want) }) {
				t.Errorf("the planted record is not reported, want a problem naming %s: this check would let a zone's contents through\nproblems: %s",
					tc.want, strings.Join(problems, "\n"))
			}
		})
	}
}
