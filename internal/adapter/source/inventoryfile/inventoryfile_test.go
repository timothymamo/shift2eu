// Package inventoryfile_test pins the JSON inventory-file source adapter: the
// source contract in internal/adapter/source/sourcetest, and the adapter's own
// error contract - one line, the file named through text.Quote, the sentinel it
// wraps. It is black-box: only the package's exported API is used.
//
// Every fixture under testdata/ is sanitised: resource IDs such as
// i-0123456789abcdef0 and db-example, the region eu-west-1.
package inventoryfile_test

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/source/inventoryfile"
	"github.com/timothymamo/shift2eu/internal/adapter/source/sourcetest"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// The adapter implements the inventory source port: pin that at compile time.
var _ analysis.InventorySource = inventoryfile.New(validPath)

// validPath is the sanitised inventory the fixture-driven tests read. It lists
// its resources out of ID order on purpose, so reading it also pins that the
// inventory, not the file, decides the order.
const validPath = "testdata/valid.json"

// fixture returns a function producing the path of a sanitised fixture, for the
// table-driven error tests, which build their path inside the subtest.
func fixture(name string) func(t *testing.T) string {
	return func(t *testing.T) string {
		t.Helper()

		return filepath.Join("testdata", name)
	}
}

// inventoryOf reads path with a fresh source and returns the inventory.
func inventoryOf(t *testing.T, path string) inventory.Inventory {
	t.Helper()

	inv, err := inventoryfile.New(path).Inventory(t.Context())
	if err != nil {
		t.Fatalf("Inventory(%q) returned unexpected error: %v", path, err)
	}

	return inv
}

// resourceIDs returns the IDs of resources, in the order given.
func resourceIDs(resources []inventory.Resource) []string {
	ids := make([]string, 0, len(resources))
	for _, r := range resources {
		ids = append(ids, r.ID)
	}

	return ids
}

// mentions reports whether s contains at least one of keywords. The adapter's
// exact wording is not part of the contract; naming the problem is.
func mentions(s string, keywords ...string) bool {
	return slices.ContainsFunc(keywords, func(keyword string) bool {
		return strings.Contains(s, keyword)
	})
}

// writeFile writes content to a fresh file in dir and returns its path.
func writeFile(t *testing.T, dir, content string) string {
	t.Helper()

	path := filepath.Join(dir, "inventory.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the fixture %s: %v", text.Quote(path), err)
	}

	return path
}

// paddedInventory returns a valid inventory document of exactly size bytes: an
// empty inventory padded with trailing spaces, which JSON allows.
func paddedInventory(t *testing.T, size int) string {
	t.Helper()

	const document = `{"schemaVersion":1,"source":"aws","resources":[]}`
	if size < len(document) {
		t.Fatalf("paddedInventory(%d): smaller than the %d-byte document", size, len(document))
	}

	return document + strings.Repeat(" ", size-len(document))
}

// emptyResourcesDocument returns an inventory document holding n empty resource
// objects: the shortest resource JSON allows, for the tests that need far more
// resources than a document small enough to read can carry with full ones.
func emptyResourcesDocument(n int) string {
	var b strings.Builder
	b.WriteString(`{"schemaVersion":1,"source":"aws","resources":[`)

	for i := range n {
		if i > 0 {
			b.WriteString(",")
		}

		b.WriteString("{}")
	}

	b.WriteString("]}")

	return b.String()
}

// resourcesDocument returns an inventory document holding n resources: input
// too large for a fixture.
func resourcesDocument(t *testing.T, n int) string {
	t.Helper()

	var b strings.Builder
	b.WriteString(`{"schemaVersion":1,"source":"aws","resources":[`)

	for i := range n {
		if i > 0 {
			b.WriteString(",")
		}

		b.WriteString(`{"id":"i-`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`","category":"compute.vm","region":"eu-west-1"}`)
	}

	b.WriteString("]}")

	return b.String()
}

// missingFileInDir returns the path of a file that does not exist, in a fresh
// directory under t.TempDir() named name.
func missingFileInDir(t *testing.T, name string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("creating the directory %s: %v", text.Quote(dir), err)
	}

	return filepath.Join(dir, "inventory.json")
}

// wantValidInventory is the inventory testdata/valid.json must produce. It is
// built with inventory.New from the same data, so the domain's own rules
// (capabilities sorted, resources in ID order) apply to the expectation too: a
// direct-able compute.vm with a capability, a partial-able database with two,
// a storage.block with no capabilities, and an explicitly unclassified resource
// with neither raw type nor region.
func wantValidInventory(t *testing.T) inventory.Inventory {
	t.Helper()

	inv, err := inventory.New("aws", []inventory.Resource{
		{
			ID:           "i-0123456789abcdef0",
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
		},
		{
			ID:           "db-example",
			RawType:      "AWS::RDS::DBInstance",
			Category:     taxonomy.DatabasePostgreSQL,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.ReadReplicas, taxonomy.PointInTimeRestore},
		},
		{
			ID:       "volume-example",
			RawType:  "AWS::EC2::Volume",
			Category: taxonomy.StorageBlock,
			Region:   "eu-west-1",
		},
		{
			ID:       "global-example",
			Category: taxonomy.Unclassified,
		},
	})
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return inv
}

// TestInventoryContract runs the source contract suite every source adapter must
// pass: the recorded inventory, the same one on every call, and cancellation.
func TestInventoryContract(t *testing.T) {
	t.Parallel()

	sourcetest.Run(t, inventoryfile.New(validPath), wantValidInventory(t))
}

// TestInventoryContractVersion2 runs the same contract suite on the version 2
// fixture, whose resources carry every version 2 field: a source that drops a
// size, a usage, a workload, a relation or a cost between calls fails it.
func TestInventoryContractVersion2(t *testing.T) {
	t.Parallel()

	sourcetest.Run(t, inventoryfile.New(validV2Path), wantVersion2Inventory(t))
}

// TestInventoryReadsNullAsAbsent pins the null policy ADR 0006 gives the
// optional fields: a resource whose rawType, region and capabilities are null is
// the same resource as one whose JSON leaves those keys out. The policy is the
// inventory reader's own - the shared decoder refuses a null no format asks for -
// so it is pinned here, on the inventory that comes out.
func TestInventoryReadsNullAsAbsent(t *testing.T) {
	t.Parallel()

	withNulls := `{"schemaVersion":1,"source":"aws","resources":[` +
		`{"id":"i-0123456789abcdef0","rawType":null,"category":"compute.vm",` +
		`"region":null,"capabilities":null}]}`
	withoutKeys := `{"schemaVersion":1,"source":"aws","resources":[` +
		`{"id":"i-0123456789abcdef0","category":"compute.vm"}]}`

	read := func(document string) inventory.Inventory {
		t.Helper()

		return inventoryOf(t, writeFile(t, t.TempDir(), document))
	}

	want := read(withoutKeys)
	got := read(withNulls)

	if diff := cmp.Diff(want.Source(), got.Source()); diff != "" {
		t.Errorf("Source() of the document with nulls differs (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(want.Resources(), got.Resources()); diff != "" {
		t.Errorf("Resources() of the document with nulls differ (-want +got):\n%s", diff)
	}
}

// Documents that are written by the test instead of installed as fixtures.
//
// A fixture cannot hold them reliably: an editor, a formatter or a git filter
// drops a byte order mark or re-encodes invalid UTF-8 without saying so, and a
// repeated key is easy to "fix" by accident. Spelling the bytes out here keeps
// each case exact.
const (
	// duplicateCapabilitiesDocument repeats a resource's capabilities key. The
	// first value carries a capability, the second is empty, so a decoder that
	// accepts duplicates silently decides which one the resource has.
	duplicateCapabilitiesDocument = `{"schemaVersion":1,"source":"aws","resources":[` +
		`{"id":"db-example","category":"database.postgresql",` +
		`"capabilities":["point-in-time-restore"],"capabilities":[]}]}`
	// duplicateCategoryDocument repeats a resource's category key.
	duplicateCategoryDocument = `{"schemaVersion":1,"source":"aws","resources":[` +
		`{"id":"db-example","category":"database.postgresql","category":"unclassified"}]}`
	// duplicateResourcesDocument repeats the top-level resources key.
	duplicateResourcesDocument = `{"schemaVersion":1,"source":"aws","resources":[],"resources":[]}`
	// inexactCapabilitiesDocument capitalises a resource's capabilities key.
	inexactCapabilitiesDocument = `{"schemaVersion":1,"source":"aws","resources":[` +
		`{"id":"i-0123456789abcdef0","category":"compute.vm","Capabilities":[]}]}`
	// inexactIDDocument capitalises a resource's id key, and carries a valid
	// lowercase id beside it, so the resource is otherwise complete.
	inexactIDDocument = `{"schemaVersion":1,"source":"aws","resources":[` +
		`{"id":"i-0123456789abcdef0","ID":"i-0123456789abcdef1","category":"compute.vm"}]}`
	// inexactCapabilitiesLongSDocument spells the s of capabilities with U+017F
	// LATIN SMALL LETTER LONG S, which Unicode case-folds to s: a decoder that
	// folds case accepts it as the right key.
	inexactCapabilitiesLongSDocument = `{"schemaVersion":1,"source":"aws","resources":[` +
		`{"id":"i-0123456789abcdef0","category":"compute.vm","capabilitie` + "\u017f" + `":[]}]}`
	// inexactSourceLongSDocument adds a second source key spelled with the long s.
	inexactSourceLongSDocument = `{"schemaVersion":1,"source":"aws","resources":[],` +
		`"` + "\u017f" + `ource":"aws"}`
	// unknownResourceFieldInSecondDocument carries the unknown key regoin in the
	// second resource only: the message must say which resource it is.
	unknownResourceFieldInSecondDocument = `{"schemaVersion":1,"source":"aws","resources":[` +
		`{"id":"db-example","category":"database.postgresql"},` +
		`{"id":"volume-example","category":"storage.block","regoin":"eu-west-1"}]}`
	// bomUTF8Document starts with the UTF-8 encoding of U+FEFF, EF BB BF, which
	// JSON does not allow: its text is white space, not a token, and a reader
	// that skips it reads a different document than the bytes say.
	bomUTF8Document = "\xef\xbb\xbf" + `{"schemaVersion":1,"source":"aws","resources":[]}`
	// bomUTF16LEDocument starts with the UTF-16LE byte order mark, FF FE, in a
	// file that is not UTF-16: it is refused, never guessed at.
	bomUTF16LEDocument = "\xff\xfe" + `{"schemaVersion":1,"source":"aws","resources":[]}`
	// invalidUTF8Document holds the byte FF, which is never valid UTF-8, in a
	// resource id: encoding/json would replace it with U+FFFD and read on.
	invalidUTF8Document = `{"schemaVersion":1,"source":"aws","resources":[` +
		`{"id":"a` + "\xff" + `","category":"compute.vm"}]}`
	// stringSchemaVersionDocument spells the version as the string "1", which is
	// not the number 1.
	stringSchemaVersionDocument = `{"schemaVersion":"1","source":"aws","resources":[]}`
	// fractionalSchemaVersionDocument spells the version as the number 1.0, which
	// is a different JSON number from 1.
	fractionalSchemaVersionDocument = `{"schemaVersion":1.0,"source":"aws","resources":[]}`
	// misspeltSchemaVersionDocument capitalises the top-level schemaVersion key,
	// so the exact key the format declares is missing.
	misspeltSchemaVersionDocument = `{"SchemaVersion":1,"source":"aws","resources":[]}`
	// nullSourceDocument spells the source as null, which ADR 0006 reads as an
	// absent field: the domain refuses the empty source, not the decoder.
	nullSourceDocument = `{"schemaVersion":1,"source":null,"resources":[]}`
	// nullResourcesDocument spells resources as null: the one field whose null is
	// not an absent value.
	nullResourcesDocument = `{"schemaVersion":1,"source":"aws","resources":null}`
	// nullSchemaVersionDocument spells the version as null, which is a missing
	// version: there is no default.
	nullSchemaVersionDocument = `{"schemaVersion":null,"source":"aws","resources":[]}`
	// capabilityNumberDocument puts the number 42 where a capability, a string,
	// belongs.
	capabilityNumberDocument = `{"schemaVersion":1,"source":"aws","resources":[` +
		`{"id":"i-0123456789abcdef0","category":"compute.vm","capabilities":["ipv6",42]}]}`
	// capabilityNullDocument puts null where a capability belongs.
	capabilityNullDocument = `{"schemaVersion":1,"source":"aws","resources":[` +
		`{"id":"i-0123456789abcdef0","category":"compute.vm","capabilities":[null]}]}`
	// resourceNullDocument puts null where a resource object belongs.
	resourceNullDocument = `{"schemaVersion":1,"source":"aws","resources":[null]}`
	// multibyteThenWrongTypeDocument puts a value of several characters before a
	// wrong type: äöü counts three columns, however many bytes it takes.
	multibyteThenWrongTypeDocument = `{"schemaVersion":1,"source":"äöü","resources":[` +
		`{"id":"i-0","category":42}]}`
	// firstValueArrayDocument is a JSON array followed by a byte that is not
	// JSON: what is wrong with the file is the data after the first value.
	firstValueArrayDocument = `[] x`
	// firstValueArrayThenValueDocument is a JSON array followed by a second value.
	firstValueArrayThenValueDocument = `[1] x`
	// loneHighSurrogateDocument escapes U+D800, a high surrogate, on its own in a
	// resource id: JSON spells the escape so, but it names no character.
	loneHighSurrogateDocument = `{"schemaVersion":1,"source":"aws","resources":[` +
		`{"id":"a\ud800b","category":"compute.vm"}]}`
	// loneLowSurrogateDocument escapes U+DC00, a low surrogate, on its own.
	loneLowSurrogateDocument = `{"schemaVersion":1,"source":"aws","resources":[` +
		`{"id":"a\udc00b","category":"compute.vm"}]}`
	// surrogatePairDocument escapes U+1F600 as the pair of surrogates JSON spells
	// it with: together they name one character, which the id may hold.
	surrogatePairDocument = `{"schemaVersion":1,"source":"aws","resources":[` +
		`{"id":"\ud83d\ude00","category":"compute.vm"}]}`
)

// schemaDocument returns an inventory document of version 1 or 2 holding one
// compute.vm resource with extra appended to it, for the cases whose version 2
// fields are written out here instead of installed as a fixture. extra starts
// with its own comma.
func schemaDocument(version int, extra string) string {
	return `{"schemaVersion":` + strconv.Itoa(version) + `,"source":"aws","resources":[` +
		`{"id":"i-0123456789abcdef0","category":"compute.vm"` + extra + `}]}`
}

// relationDocument returns a version 2 inventory document holding two databases,
// the second carrying relations, the JSON array the case writes. Two resources,
// because a relation's target must name one: the first is what a valid relation
// points at.
func relationDocument(relations string) string {
	return `{"schemaVersion":2,"source":"aws","resources":[` +
		`{"id":"db-primary","category":"database.postgresql"},` +
		`{"id":"db-replica","category":"database.postgresql","relations":` + relations + `}]}`
}

// relationsDocument returns a version 2 inventory document whose first resource
// lists n relations, all pointing at the second: input too large for a fixture.
// The duplicates count toward the cap, and the domain de-duplicates them once
// the document is read.
func relationsDocument(n int) string {
	var b strings.Builder
	b.WriteString(`{"schemaVersion":2,"source":"aws","resources":[`)
	b.WriteString(`{"id":"i-0","category":"compute.vm","relations":[`)

	for i := range n {
		if i > 0 {
			b.WriteString(",")
		}

		b.WriteString(`{"type":"attached-to","target":"i-1"}`)
	}

	b.WriteString(`]},{"id":"i-1","category":"compute.vm"}]}`)

	return b.String()
}

// positionOf returns the "line L, column C" of the first occurrence of token in
// the document at path, as the shared decoder places a value: 1-based, the
// column counted in characters. A case names the token, never a column, so the
// expectation is read from the document itself.
func positionOf(t *testing.T, path, token string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the document %s: %v", text.Quote(path), err)
	}

	i := strings.Index(string(data), token)
	if i < 0 {
		t.Fatalf("the document at %s does not hold %s", text.Quote(path), text.Quote(token))
	}

	return "line 1, column " + strconv.Itoa(utf8.RuneCountInString(string(data[:i]))+1)
}

// capabilitiesDocument returns an inventory document holding one resource whose
// capabilities list repeats the JSON value element n times: input too large for
// a fixture.
func capabilitiesDocument(n int, element string) string {
	var b strings.Builder
	b.WriteString(`{"schemaVersion":1,"source":"aws","resources":[`)
	b.WriteString(`{"id":"i-0","category":"compute.vm","capabilities":[`)

	for i := range n {
		if i > 0 {
			b.WriteString(",")
		}

		b.WriteString(element)
	}

	b.WriteString("]}]}")

	return b.String()
}

// generated returns a function that writes document to a fresh file, for the
// cases whose input is not a fixture.
func generated(document string) func(t *testing.T) string {
	return func(t *testing.T) string {
		t.Helper()

		return writeFile(t, t.TempDir(), document)
	}
}

// TestInventoryErrors pins every way reading an inventory file fails: the
// sentinel the error wraps, that it names the file through text.Quote(path), and
// that it is one line that echoes the path quoted, never the OS error holding it
// unquoted. A decode failure also says where in the document it is, as a line
// and a column, and which key or location it is about, with an array element
// written resources[i] as the domain writes it.
func TestInventoryErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// path returns the file to read. Cases a fixture cannot hold write their
		// input here.
		path func(t *testing.T) string
		// wantErr lists every sentinel the error must wrap.
		wantErr []error
		// wantNotErr lists sentinels the error must not wrap: a file refused for
		// one reason is never also reported as another.
		wantNotErr []error
		// wantMentions is a list of alternatives: the message, with the quoted
		// path removed, must contain at least one keyword per group.
		wantMentions [][]string
		// wantQuoted lists values the message must show through text.Quote: the
		// key the problem is about, or where in the document it is.
		wantQuoted []string
		// wantPosition is where the message must place the problem: a 1-based
		// line and column in the document, never a byte offset.
		wantPosition string
		// wantPositionAt is a token of the document whose line and column the
		// message must name: the position is computed from the document the case
		// writes, so a case never repeats a column a reader has to count to.
		wantPositionAt string
		// wantAbsent lists substrings the message must not contain.
		wantAbsent []string
	}{
		{
			name:    "missing file",
			path:    fixture("missing.json"),
			wantErr: []error{inventoryfile.ErrRead},
		},
		{
			// not-json.json holds "not json": the decoder reads n, o, t, then
			// refuses the o on the first line, in its second column.
			name:         "not JSON",
			path:         fixture("not-json.json"),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantPosition: "line 1, column 2",
			wantAbsent:   []string{"byte offset"},
		},
		{
			name:    "trailing data after the object",
			path:    fixture("trailing-data.json"),
			wantErr: []error{inventoryfile.ErrDecode},
			// What follows is described by what it follows: the first JSON value,
			// whatever its type, not an inventory object the file never had to be.
			wantMentions: [][]string{{"first JSON value"}},
			wantAbsent:   []string{"inventory object"},
		},
		{
			// The first value need not be an object for the reader to say what
			// comes after it: an array and then a byte that is not JSON is refused
			// for the data after the first JSON value at column 4.
			name:         "array value then malformed data",
			path:         generated(firstValueArrayDocument),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"first JSON value"}},
			wantAbsent:   []string{"inventory object"},
			wantPosition: "line 1, column 4",
		},
		{
			name:         "array value then a second value",
			path:         generated(firstValueArrayThenValueDocument),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"first JSON value"}},
			wantAbsent:   []string{"inventory object"},
			wantPosition: "line 1, column 5",
		},
		{
			name:       "unknown top-level field",
			path:       fixture("unknown-field.json"),
			wantErr:    []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"bogusField"},
		},
		{
			name:       "unknown resource field",
			path:       fixture("unknown-resource-field.json"),
			wantErr:    []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"bogusResourceField"},
		},
		{
			// Naming the key is not enough: with several resources, the message
			// must say which one the key is in, through text.Quote, as the domain
			// writes an array element - resources[1] - and where in the document it
			// is: "regoin" opens at column 151.
			name:         "unknown field in the second resource",
			path:         generated(unknownResourceFieldInSecondDocument),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantQuoted:   []string{"resources[1].regoin"},
			wantPosition: "line 1, column 151",
		},
		{
			// encoding/json accepts a repeated key and keeps the last value, so a
			// file could give a resource two capabilities lists and one would win
			// silently. The key and where it is repeated are both named.
			name:         "duplicate capabilities key",
			path:         generated(duplicateCapabilitiesDocument),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"duplicate", "twice", "repeated", "again", "more than once"}},
			wantQuoted:   []string{"resources[0].capabilities"},
		},
		{
			name:         "duplicate category key",
			path:         generated(duplicateCategoryDocument),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"duplicate", "twice", "repeated", "again", "more than once"}},
			wantQuoted:   []string{"resources[0].category"},
		},
		{
			name:         "duplicate resources key",
			path:         generated(duplicateResourcesDocument),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"duplicate", "twice", "repeated", "again", "more than once"}},
			wantQuoted:   []string{"resources"},
		},
		{
			// A key must be byte-equal to the one the format declares: encoding/json
			// matches case-insensitively, and Unicode case folding also maps the
			// long s to s. Neither spelling is the capability the format names.
			name:       "inexact capabilities key",
			path:       generated(inexactCapabilitiesDocument),
			wantErr:    []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"Capabilities"},
		},
		{
			name:       "inexact id key",
			path:       generated(inexactIDDocument),
			wantErr:    []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"ID"},
			// Someone wrote ID for id: the message says so.
			wantMentions: [][]string{{"did you mean"}},
		},
		{
			name:       "inexact capabilities key spelled with a long s",
			path:       generated(inexactCapabilitiesLongSDocument),
			wantErr:    []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"capabilitie\u017f"},
		},
		{
			name:       "inexact source key spelled with a long s",
			path:       generated(inexactSourceLongSDocument),
			wantErr:    []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"\u017fource"},
		},
		{
			// wrong-type.json holds 42 where a resource id string belongs. The 42
			// starts the document's only line in column 54.
			name:         "wrong type for a resource field",
			path:         fixture("wrong-type.json"),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantPosition: "line 1, column 54",
			wantAbsent:   []string{"byte offset"},
		},
		{
			// The column counts characters, not bytes: with äöü before it, the 42
			// starts at character column 71, which is the document's byte 74.
			name:         "wrong type after a multi-byte value",
			path:         generated(multibyteThenWrongTypeDocument),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantQuoted:   []string{"resources[0].category"},
			wantPosition: "line 1, column 71",
			wantAbsent:   []string{"column 74", "byte offset"},
		},
		{
			// A capability is a string: the number 42 is refused where it is, so
			// the message names the element of the list, with the index in brackets
			// as the domain writes it, and where the 42 is (column 123).
			name:         "capability that is not a string",
			path:         generated(capabilityNumberDocument),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantQuoted:   []string{"resources[0].capabilities[1]"},
			wantPosition: "line 1, column 123",
		},
		{
			// null is not a string either, and is refused as the element it is.
			name:       "capability that is null",
			path:       generated(capabilityNullDocument),
			wantErr:    []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[0].capabilities[0]"},
		},
		// A version 1 file may not hold a field only version 2 defines: the
		// reader knows each version's keys, so the key is unknown, named by its
		// path, and never quietly ignored.
		{
			name:       "size in a version 1 file",
			path:       generated(schemaDocument(1, `,"size":{"vcpus":2}`)),
			wantErr:    []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[0].size"},
		},
		{
			name:       "usage in a version 1 file",
			path:       generated(schemaDocument(1, `,"usage":{"hoursPerMonth":730}`)),
			wantErr:    []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[0].usage"},
		},
		{
			name:       "workload in a version 1 file",
			path:       generated(schemaDocument(1, `,"workload":"shop"`)),
			wantErr:    []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[0].workload"},
		},
		{
			name:       "relations in a version 1 file",
			path:       generated(schemaDocument(1, `,"relations":[]`)),
			wantErr:    []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[0].relations"},
		},
		{
			name:       "monthlyCost in a version 1 file",
			path:       generated(schemaDocument(1, `,"monthlyCost":{"amount":1,"currency":"EUR"}`)),
			wantErr:    []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[0].monthlyCost"},
		},
		// The version 1 rules hold for every version 2 object too: keys are
		// exact, appear at most once, and their values are of the JSON type the
		// format gives them.
		{
			name:           "unknown key in a size object",
			path:           generated(schemaDocument(2, `,"size":{"vcpu":4}`)),
			wantErr:        []error{inventoryfile.ErrDecode},
			wantQuoted:     []string{"resources[0].size.vcpu"},
			wantPositionAt: "\"vcpu\"",
		},
		{
			name:       "unknown key in a usage object",
			path:       generated(schemaDocument(2, `,"usage":{"hours":730}`)),
			wantErr:    []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[0].usage.hours"},
		},
		{
			name:       "unknown key in a cost object",
			path:       generated(schemaDocument(2, `,"monthlyCost":{"amount":1,"currency":"EUR","unit":"month"}`)),
			wantErr:    []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[0].monthlyCost.unit"},
		},
		{
			name:       "unknown key in a relation object",
			path:       generated(relationDocument(`[{"type":"attached-to","target":"db-primary","kind":"x"}]`)),
			wantErr:    []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[1].relations[0].kind"},
		},
		{
			// encoding/json keeps the last value of a repeated key, so a repeated
			// key would decide a size silently.
			name:           "repeated key in a size object",
			path:           generated(schemaDocument(2, `,"size":{"vcpus":2,"vcpus":4}`)),
			wantErr:        []error{inventoryfile.ErrDecode},
			wantMentions:   [][]string{{"duplicate", "twice", "repeated", "again", "more than once"}},
			wantQuoted:     []string{"resources[0].size.vcpus"},
			wantPositionAt: `"vcpus":4`,
		},
		{
			// A key must be byte-equal to the one the format declares:
			// encoding/json matches case-insensitively, so VCPUs would be read as
			// vcpus by a decoder that does not check.
			name:         "key in a size object that differs in case",
			path:         generated(schemaDocument(2, `,"size":{"VCPUs":2}`)),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"did you mean"}},
			wantQuoted:   []string{"VCPUs"},
		},
		{
			name:         "vcpus that is a string",
			path:         generated(schemaDocument(2, `,"size":{"vcpus":"4"}`)),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantQuoted:   []string{"resources[0].size.vcpus"},
			wantMentions: [][]string{{"line 1, column "}},
			wantAbsent:   []string{"byte offset"},
		},
		{
			// A count is an integer: 1.5 is not one, and is refused where it is.
			name:         "fractional count",
			path:         generated(schemaDocument(2, `,"size":{"count":1.5}`)),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantQuoted:   []string{"resources[0].size.count"},
			wantMentions: [][]string{{"line 1, column "}},
		},
		{
			// A number that overflows float64 is not a number the format reads.
			name:         "size number that overflows float64",
			path:         generated(schemaDocument(2, `,"size":{"vcpus":1e999}`)),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantQuoted:   []string{"resources[0].size.vcpus"},
			wantMentions: [][]string{{"line 1, column "}},
		},
		{
			// An amount that overflows float64 is refused too, wherever it is.
			name:         "cost amount that overflows float64",
			path:         generated(schemaDocument(2, `,"monthlyCost":{"amount":1e999,"currency":"EUR"}`)),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantQuoted:   []string{"resources[0].monthlyCost.amount"},
			wantMentions: [][]string{{"line 1, column "}},
		},
		{
			name:         "relations that is an object",
			path:         generated(schemaDocument(2, `,"relations":{}`)),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantQuoted:   []string{"resources[0].relations"},
			wantMentions: [][]string{{"an array"}},
		},
		{
			// A relation is a value of the list: null is not an absent value
			// here, as null is no absent element of resources or capabilities.
			name:         "relation that is null",
			path:         generated(relationDocument(`[null]`)),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantNotErr:   []error{inventory.ErrEmptyID},
			wantMentions: [][]string{{"null"}},
			wantQuoted:   []string{"resources[1].relations[0]"},
		},
		{
			// Within a relation both keys are required, and the error names the
			// one that is missing.
			name:         "relation without a type",
			path:         generated(relationDocument(`[{"target":"db-primary"}]`)),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"required key"}},
			wantQuoted:   []string{"type"},
		},
		{
			name:         "relation without a target",
			path:         generated(relationDocument(`[{"type":"attached-to"}]`)),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"required key"}},
			wantQuoted:   []string{"target"},
		},
		{
			name:         "relation type that is not a string",
			path:         generated(relationDocument(`[{"type":3,"target":"db-primary"}]`)),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantQuoted:   []string{"resources[1].relations[0].type"},
			wantMentions: [][]string{{"a string"}},
		},
		{
			name:         "cost without an amount",
			path:         generated(schemaDocument(2, `,"monthlyCost":{"currency":"EUR"}`)),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"required key"}},
			wantQuoted:   []string{"amount"},
		},
		{
			name:         "cost without a currency",
			path:         generated(schemaDocument(2, `,"monthlyCost":{"amount":1}`)),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"required key"}},
			wantQuoted:   []string{"currency"},
		},
		// The domain checks the values of the version 2 fields, and its error
		// reaches the reader with the path it names: the reader repeats no rule of
		// its own, so the two cannot drift.
		{
			name:       "relation to a resource that is not in the inventory",
			path:       generated(relationDocument(`[{"type":"replica-of","target":"db-missing"}]`)),
			wantErr:    []error{inventory.ErrUnknownRelationTarget},
			wantNotErr: []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[1].relations[0].target", "db-missing"},
		},
		{
			name:       "relation to the resource itself",
			path:       generated(relationDocument(`[{"type":"replica-of","target":"db-replica"}]`)),
			wantErr:    []error{inventory.ErrSelfRelation},
			wantNotErr: []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[1].relations[0].target"},
		},
		{
			name:       "relation type outside the vocabulary",
			path:       generated(relationDocument(`[{"type":"replica","target":"db-primary"}]`)),
			wantErr:    []error{inventory.ErrUnknownRelationType},
			wantNotErr: []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[1].relations[0].type", "replica"},
		},
		{
			name:       "vcpus of zero",
			path:       generated(schemaDocument(2, `,"size":{"vcpus":0}`)),
			wantErr:    []error{inventory.ErrInvalidSize},
			wantNotErr: []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[0].size.vcpus"},
		},
		{
			name:       "vcpus on a category that takes none",
			path:       generated(`{"schemaVersion":2,"source":"aws","resources":[{"id":"bucket-example","category":"storage.object","size":{"vcpus":2}}]}`),
			wantErr:    []error{inventory.ErrFieldNotForCategory},
			wantNotErr: []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[0].size.vcpus"},
		},
		{
			name:       "hoursPerMonth above the longest month",
			path:       generated(schemaDocument(2, `,"usage":{"hoursPerMonth":745}`)),
			wantErr:    []error{inventory.ErrInvalidUsage},
			wantNotErr: []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[0].usage.hoursPerMonth"},
		},
		{
			name:       "currency that is not three upper-case letters",
			path:       generated(schemaDocument(2, `,"monthlyCost":{"amount":1,"currency":"usd"}`)),
			wantErr:    []error{inventory.ErrInvalidCost},
			wantNotErr: []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[0].monthlyCost.currency", "usd"},
		},
		{
			name:       "workload with a control character",
			path:       generated(schemaDocument(2, `,"workload":"a\u001bb"`)),
			wantErr:    []error{text.ErrInvalid},
			wantNotErr: []error{inventoryfile.ErrDecode},
			wantQuoted: []string{"resources[0].workload"},
		},
		{
			// A null element of the resources list is refused by the reader, as
			// the element of that list, never passed on to the domain as a
			// resource whose ID is empty.
			name:         "resource that is null",
			path:         generated(resourceNullDocument),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantNotErr:   []error{inventory.ErrEmptyID},
			wantMentions: [][]string{{"null"}},
			wantQuoted:   []string{"resources[0]"},
			wantPosition: "line 1, column 48",
		},
		{
			// A byte order mark is not JSON and not UTF-8 text either way: the
			// reader refuses it and says what it found.
			name:         "UTF-8 byte order mark",
			path:         generated(bomUTF8Document),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"byte order mark"}},
		},
		{
			name:         "UTF-16 byte order mark",
			path:         generated(bomUTF16LEDocument),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"byte order mark"}},
		},
		{
			// encoding/json replaces an invalid byte with U+FFFD and decodes on,
			// which would make the id in the report differ from the id in the file.
			// The FF is the document's byte 55, its character column 56.
			name:         "invalid UTF-8 in a value",
			path:         generated(invalidUTF8Document),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantPosition: "line 1, column 56",
		},
		{
			// A multi-byte character where the next key belongs is malformed
			// JSON, and the JSON decoder counts the rune's last byte: the message
			// must place the character at its first byte, which is line 3's
			// column 2.
			name:         "malformed two-byte character where a key is expected",
			path:         generated("{\n \"schemaVersion\": 1,\n é\n}"),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"malformed JSON"}},
			wantPosition: "line 3, column 2",
			wantAbsent:   []string{"column 3"},
		},
		{
			// The same for a character of three bytes: the step back to the
			// first byte of the rune covers every byte of it.
			name:         "malformed three-byte character where a key is expected",
			path:         generated("{\n \"schemaVersion\": 1,\n €\n}"),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"malformed JSON"}},
			wantPosition: "line 3, column 2",
			wantAbsent:   []string{"column 4"},
		},
		{
			// JSON spells a character outside the BMP as a pair of escaped
			// surrogates. A surrogate escape on its own names no character, and a
			// decoder that accepts it writes U+FFFD into the id instead.
			name:         "escaped high surrogate on its own",
			path:         generated(loneHighSurrogateDocument),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"line 1, column "}},
			wantAbsent:   []string{"byte offset"},
		},
		{
			name:         "escaped low surrogate on its own",
			path:         generated(loneLowSurrogateDocument),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"line 1, column "}},
			wantAbsent:   []string{"byte offset"},
		},
		{
			// The version is checked on the envelope before any field is decoded,
			// so a version 3 file that carries a field no version this reader reads
			// has is refused for its version alone. Versions 1 and 2 are read.
			name:         "unsupported schema version",
			path:         fixture("schema-version-3.json"),
			wantErr:      []error{inventoryfile.ErrSchemaVersion},
			wantNotErr:   []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"schemaVersion"}, {"3"}},
		},
		{
			// A version is the integer it is written as: 2.0 is a different JSON
			// number from 2, and is refused for its version, not read as 2.
			name:         "version 2 as a fractional number",
			path:         generated(`{"schemaVersion":2.0,"source":"aws","resources":[]}`),
			wantErr:      []error{inventoryfile.ErrSchemaVersion},
			wantNotErr:   []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"schemaVersion"}},
		},
		{
			// Nor is a version the string "2".
			name:         "version 2 as a string",
			path:         generated(`{"schemaVersion":"2","source":"aws","resources":[]}`),
			wantErr:      []error{inventoryfile.ErrSchemaVersion},
			wantNotErr:   []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"schemaVersion"}, {"string"}},
			wantAbsent:   []string{`\"`},
		},
		{
			// A version is a JSON number: the string "1" is not it. The message
			// says what it found, in the package's own words, so the value from the
			// file is quoted once and never double-escaped.
			name:         "schema version as a string",
			path:         generated(stringSchemaVersionDocument),
			wantErr:      []error{inventoryfile.ErrSchemaVersion},
			wantMentions: [][]string{{"schemaVersion"}, {"string"}},
			wantAbsent:   []string{`\"`},
		},
		{
			name:         "schema version as a fractional number",
			path:         generated(fractionalSchemaVersionDocument),
			wantErr:      []error{inventoryfile.ErrSchemaVersion},
			wantMentions: [][]string{{"schemaVersion"}},
		},
		{
			// The version error is worded once, by the shared decoder, so both
			// readers say a version is an integer: a document without a version is
			// missing the integers this reader reads, not a bare version.
			name:    "missing schema version",
			path:    fixture("no-schema-version.json"),
			wantErr: []error{inventoryfile.ErrSchemaVersion},
			wantMentions: [][]string{
				{"schemaVersion"},
				{"0", "absent", "missing", "not set", "not present"},
				{"1"},
				{"2"},
			},
		},
		{
			// The exact key is missing, so the file is refused for its version,
			// never for a field the version it claims has: the key that was meant
			// is named, quoted, and the sentinel says it is about the version.
			name:       "schema version key misspelt",
			path:       generated(misspeltSchemaVersionDocument),
			wantErr:    []error{inventoryfile.ErrSchemaVersion},
			wantNotErr: []error{inventoryfile.ErrDecode},
			// The wording is the shared decoder's, and the key that was meant is
			// still named.
			wantMentions: [][]string{{"schemaVersion"}, {"missing"}, {"1"}, {"2"}},
			wantQuoted:   []string{"SchemaVersion"},
		},
		{
			name:         "missing resources",
			path:         fixture("missing-resources.json"),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"resources"}},
		},
		{
			// An unknown category is a domain error, never a silent unclassified:
			// the error must name the resource and the category it carries.
			name:         "unknown category",
			path:         fixture("unknown-category.json"),
			wantErr:      []error{taxonomy.ErrUnknownCategory},
			wantMentions: [][]string{{"i-0123456789abcdef0"}, {"compute.mv"}},
		},
		{
			name:    "empty source",
			path:    fixture("empty-source.json"),
			wantErr: []error{inventory.ErrEmptySource},
		},
		{
			// ADR 0006: null is the same as an absent field, so a null source is the
			// domain's empty source, never a decode failure.
			name:       "null source",
			path:       generated(nullSourceDocument),
			wantErr:    []error{inventory.ErrEmptySource},
			wantNotErr: []error{inventoryfile.ErrDecode},
		},
		{
			// resources is the one field a null is not absent for: it is a value of
			// the wrong type, refused where it is, as the array it should be.
			name:         "null resources",
			path:         generated(nullResourcesDocument),
			wantErr:      []error{inventoryfile.ErrDecode},
			wantMentions: [][]string{{"resources"}, {"line 1, column "}},
			wantQuoted:   []string{"resources"},
		},
		{
			// A null version is a missing version: there is no default.
			name:       "null schema version",
			path:       generated(nullSchemaVersionDocument),
			wantErr:    []error{inventoryfile.ErrSchemaVersion},
			wantNotErr: []error{inventoryfile.ErrDecode},
		},
		{
			name: "file larger than MaxFileSize",
			path: func(t *testing.T) string {
				t.Helper()

				return writeFile(t, t.TempDir(), paddedInventory(t, inventoryfile.MaxFileSize+1))
			},
			wantErr: []error{inventoryfile.ErrTooLarge},
		},
		{
			name: "more resources than MaxResources",
			path: func(t *testing.T) string {
				t.Helper()

				return writeFile(t, t.TempDir(), resourcesDocument(t, inventoryfile.MaxResources+1))
			},
			wantErr: []error{inventoryfile.ErrTooManyResources},
		},
		{
			// The path is untrusted too: a newline in a directory name must not
			// put a line break in an error, which a terminal shows as two errors.
			name:    "path whose directory name holds a newline",
			path:    func(t *testing.T) string { t.Helper(); return missingFileInDir(t, "line\nbreak") },
			wantErr: []error{inventoryfile.ErrRead},
		},
		{
			// An escape sequence in a path must be escaped too, never repeated raw
			// into a terminal.
			name:    "path whose directory name holds an escape sequence",
			path:    func(t *testing.T) string { t.Helper(); return missingFileInDir(t, "ansi\x1bseq") },
			wantErr: []error{inventoryfile.ErrRead},
		},
		{
			// A directory opens, and only reading it fails: the failure is still a
			// read failure, and its path must not put a newline into the message.
			name: "path that is a directory whose name holds a newline",
			path: func(t *testing.T) string {
				t.Helper()

				path := filepath.Join(t.TempDir(), "dir\nname")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatalf("creating the directory %s: %v", text.Quote(path), err)
				}

				return path
			},
			wantErr: []error{inventoryfile.ErrRead},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := tt.path(t)

			_, err := inventoryfile.New(path).Inventory(t.Context())
			if err == nil {
				t.Fatalf("Inventory() for %s returned no error, want one wrapping %v", text.Quote(path), tt.wantErr)
			}

			for _, sentinel := range tt.wantErr {
				if !errors.Is(err, sentinel) {
					t.Errorf("Inventory() error = %v, want it to wrap %v", err, sentinel)
				}
			}

			for _, sentinel := range tt.wantNotErr {
				if errors.Is(err, sentinel) {
					t.Errorf("Inventory() error = %v, want it not to wrap %v", err, sentinel)
				}
			}

			msg := err.Error()
			quotedPath := text.Quote(path)

			if !strings.Contains(msg, quotedPath) {
				t.Errorf("Inventory() error = %q, want it to name the file through text.Quote(path) = %s", msg, quotedPath)
			}

			if strings.ContainsAny(msg, "\r\n") {
				t.Errorf("Inventory() error = %q, want a single line", msg)
			}

			if strings.Contains(msg, "\x1b") {
				t.Errorf("Inventory() error = %q, want no raw escape sequence", msg)
			}

			// A fixture name may hold a word a case looks for (resources, for one),
			// so the wording check looks at the message without the quoted path.
			rest := strings.ReplaceAll(msg, quotedPath, "")

			for i, group := range tt.wantMentions {
				if !mentions(rest, group...) {
					t.Errorf("Inventory() error = %q, want it to name problem %d (one of %v)", msg, i+1, group)
				}
			}

			for _, value := range tt.wantQuoted {
				if quoted := text.Quote(value); !strings.Contains(msg, quoted) {
					t.Errorf("Inventory() error = %q, want it to name %s through text.Quote", msg, quoted)
				}
			}

			for _, absent := range tt.wantAbsent {
				if strings.Contains(msg, absent) {
					t.Errorf("Inventory() error = %q, want it not to contain %q", msg, absent)
				}
			}

			// Where the problem is in the document is a line and a column a reader
			// of the file can look up, not a byte offset it has to count to.
			if tt.wantPosition != "" && !strings.Contains(msg, tt.wantPosition) {
				t.Errorf("Inventory() error = %q, want it to report the position as %q", msg, tt.wantPosition)
			}

			if tt.wantPositionAt != "" {
				want := positionOf(t, path, tt.wantPositionAt)
				if !strings.Contains(msg, want) {
					t.Errorf("Inventory() error = %q, want it to report the position of %q as %q",
						msg, tt.wantPositionAt, want)
				}
			}

			// An adapter that passes the OS error through echoes the path unquoted:
			// every OS error spells it "<path>: <reason>". The quoted form the
			// contract requires ends the path with a quote, so an unquoted
			// "<path>: " must never appear, whatever operation produced the error.
			// (A decoder's wording cannot be told apart from its quoted form -
			// text.Quote keeps ASCII unchanged - so decoder failures are pinned by
			// ErrDecode alone.)
			if strings.Contains(msg, path+": ") {
				t.Errorf("Inventory() error = %q, want it not to pass the OS error for the path through unquoted", msg)
			}
		})
	}
}

// TestInventoryErrorKeepsLongPathBaseName pins that an error quotes a path
// longer than 128 runes with both of its ends, not only its start: the base name
// is what says which file failed, and a deep checkout path must not hide it. The
// quoted form is text.QuotePath's: the first 32 runes, an ellipsis, the last 96
// runes, then the original byte length. It is written out here because this
// package's tests must compile while text.QuotePath does not exist yet - an
// undefined function would hide every other failure in the table above.
func TestInventoryErrorKeepsLongPathBaseName(t *testing.T) {
	t.Parallel()

	const base = "inventory-long-path.json"

	// The file does not exist, so the long directory is only joined, never made.
	path := filepath.Join(t.TempDir(), strings.Repeat("d", 150), base)

	// The path is ASCII, so its runes are its bytes, and 32 + 96 is the number of
	// runes the quoted form keeps.
	if len(path) <= 128 {
		t.Fatalf("the test path is %d bytes, want more than 128", len(path))
	}

	wantQuoted := strconv.Quote(path[:32]) + "..." + strconv.Quote(path[len(path)-96:]) +
		"(" + strconv.Itoa(len(path)) + " bytes)"

	_, err := inventoryfile.New(path).Inventory(t.Context())
	if err == nil {
		t.Fatalf("Inventory() for %s returned no error, want one wrapping %v", text.Quote(path), inventoryfile.ErrRead)
	}

	if !errors.Is(err, inventoryfile.ErrRead) {
		t.Errorf("Inventory() error = %v, want it to wrap %v", err, inventoryfile.ErrRead)
	}

	msg := err.Error()

	if !strings.Contains(msg, wantQuoted) {
		t.Errorf("Inventory() error = %q, want it to name the file through text.QuotePath(path) = %s", msg, wantQuoted)
	}

	// text.Quote keeps the first 128 runes only, so its form of the path loses the
	// base name: the error must not hold that form any more.
	if truncated := text.Quote(path); strings.Contains(msg, truncated) {
		t.Errorf("Inventory() error = %q, want it without text.Quote(path) = %s", msg, truncated)
	}

	if !strings.Contains(msg, base+`"`) {
		t.Errorf("Inventory() error = %q, want it to keep the base name %s", msg, text.Quote(base))
	}

	if strings.ContainsAny(msg, "\r\n") {
		t.Errorf("Inventory() error = %q, want a single line", msg)
	}
}

// TestInventoryHonoursCancelledContext pins that a cancelled run reads nothing:
// the file does not even have to exist.
func TestInventoryHonoursCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// The file does not exist, so an adapter that read it before checking the
	// context would fail with ErrRead instead of the cancellation.
	path := filepath.Join("testdata", "missing.json")

	_, err := inventoryfile.New(path).Inventory(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Inventory() with a cancelled context returned error %v, want one wrapping %v", err, context.Canceled)
	}

	if errors.Is(err, inventoryfile.ErrRead) {
		t.Errorf("Inventory() with a cancelled context returned error %v, want it not to wrap ErrRead: the file was read before the context was checked", err)
	}
}

// TestInventoryKeepsUnclassifiedResource pins that an explicitly unclassified
// resource is a fact about the resource, not a value to drop.
func TestInventoryKeepsUnclassifiedResource(t *testing.T) {
	t.Parallel()

	resources := inventoryOf(t, validPath).Resources()

	i := slices.IndexFunc(resources, func(r inventory.Resource) bool {
		return r.ID == "global-example"
	})
	if i < 0 {
		t.Fatalf("Resources() dropped the unclassified resource global-example: got IDs %v", resourceIDs(resources))
	}

	want := inventory.Resource{ID: "global-example", Category: taxonomy.Unclassified}
	if diff := cmp.Diff(want, resources[i]); diff != "" {
		t.Errorf("unclassified resource differs (-want +got):\n%s", diff)
	}
}

// TestInventorySortsResourcesByID pins that file order never reaches the report:
// valid.json lists its resources out of ID order and every read returns them in
// ID order.
func TestInventorySortsResourcesByID(t *testing.T) {
	t.Parallel()

	want := []string{"db-example", "global-example", "i-0123456789abcdef0", "volume-example"}

	for range 2 {
		if diff := cmp.Diff(want, resourceIDs(inventoryOf(t, validPath).Resources())); diff != "" {
			t.Errorf("Resources() IDs differ (-want +got):\n%s", diff)
		}
	}
}

// TestInventoryCapsResourcesBeforeDecoding pins that the resource count is
// checked while the document is read, before its resources are decoded: a file
// holding many times MaxResources resources is refused for its count, and the
// call allocates memory in proportion to the file, not to the resources the
// file claims.
//
// The test is not parallel: TotalAlloc counts the whole process, so a test
// allocating at the same time would be counted here.
func TestInventoryCapsResourcesBeforeDecoding(t *testing.T) {
	// MaxResources*70 empty resources is a 10.5 MB document, larger than
	// MaxFileSize, so the file would be refused for its size and never reach the
	// resource cap. 60 * MaxResources stays under the size cap and still writes
	// 3000000 resources, which decoding all of would allocate far more than the
	// budget below.
	const multiplier = 60

	document := emptyResourcesDocument(inventoryfile.MaxResources * multiplier)
	if len(document) >= inventoryfile.MaxFileSize {
		t.Fatalf("the generated document is %d bytes, want fewer than MaxFileSize (%d) so the file is refused for its resource count, not its size",
			len(document), inventoryfile.MaxFileSize)
	}

	path := writeFile(t, t.TempDir(), document)

	runtime.GC()

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	_, err := inventoryfile.New(path).Inventory(t.Context())

	runtime.ReadMemStats(&after)

	if !errors.Is(err, inventoryfile.ErrTooManyResources) {
		t.Fatalf("Inventory() error = %v, want one wrapping %v", err, inventoryfile.ErrTooManyResources)
	}

	// A decoder that reads every resource before counting them allocates about
	// 1.6 GiB for this document; stopping at the cap allocates the file and very
	// little else.
	const budget = 64 << 20
	if grew := after.TotalAlloc - before.TotalAlloc; grew >= budget {
		t.Errorf("Inventory() allocated %d MiB, want less than %d MiB: the resource cap must refuse the document before its resources are decoded",
			grew>>20, budget>>20)
	}
}

// TestInventoryAcceptsMaxResources pins the boundary of the resource cap:
// exactly MaxResources resources is accepted, so the cap rejects only what is
// more.
func TestInventoryAcceptsMaxResources(t *testing.T) {
	t.Parallel()

	path := writeFile(t, t.TempDir(), resourcesDocument(t, inventoryfile.MaxResources))

	inv, err := inventoryfile.New(path).Inventory(t.Context())
	if err != nil {
		t.Fatalf("Inventory() returned an error for exactly MaxResources (%d) resources: %v",
			inventoryfile.MaxResources, err)
	}

	if got := len(inv.Resources()); got != inventoryfile.MaxResources {
		t.Errorf("len(Resources()) = %d, want %d", got, inventoryfile.MaxResources)
	}
}

// TestInventoryAcceptsFileOfMaxSize pins the boundary of the size cap: exactly
// MaxFileSize bytes is accepted, so the cap rejects only what is larger.
func TestInventoryAcceptsFileOfMaxSize(t *testing.T) {
	t.Parallel()

	path := writeFile(t, t.TempDir(), paddedInventory(t, inventoryfile.MaxFileSize))

	inv, err := inventoryfile.New(path).Inventory(t.Context())
	if err != nil {
		t.Fatalf("Inventory() returned an error for a file of exactly MaxFileSize (%d) bytes: %v",
			inventoryfile.MaxFileSize, err)
	}

	if got := inv.Source(); got != "aws" {
		t.Errorf("Source() = %q, want %q", got, "aws")
	}
}

// capabilityCap is the most capabilities a resource may carry. The adapter
// declares it as the exported constant MaxCapabilities; the tests that need its
// value cannot refer to the identifier while the branch pinning it still has to
// build, so the value is repeated here and TestMaxCapabilitiesIsExported pins
// the declaration.
const capabilityCap = 32

// TestMaxCapabilitiesIsExported pins the capability cap as part of the
// adapter's API: an exported constant named MaxCapabilities whose value is
// capabilityCap. A reader of the package - and its documentation - depends on
// the name and the value, not only on the refusal a file with more capabilities
// meets.
//
// The test reads the package's own source instead of referring to the
// identifier: on a branch where the constant does not exist yet, a reference
// would fail to compile, and a package that does not compile shows nothing
// about any other pin here.
func TestMaxCapabilitiesIsExported(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing the package source %s: %v", text.Quote(name), err)
		}

		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}

			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok || len(value.Names) != 1 || value.Names[0].Name != "MaxCapabilities" {
					continue
				}

				if len(value.Values) != 1 {
					t.Fatalf("%s: MaxCapabilities has no value of its own, want the constant %d",
						fset.Position(value.Names[0].Pos()), capabilityCap)
				}

				lit, ok := value.Values[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.INT {
					t.Fatalf("%s: MaxCapabilities is not an integer literal, want the constant %d",
						fset.Position(value.Values[0].Pos()), capabilityCap)
				}

				if lit.Value != strconv.Itoa(capabilityCap) {
					t.Fatalf("%s: MaxCapabilities = %s, want %d", fset.Position(lit.Pos()), lit.Value, capabilityCap)
				}

				return
			}
		}
	}

	t.Fatalf("the package declares no exported constant MaxCapabilities, want one whose value is %d", capabilityCap)
}

// TestInventoryCapsCapabilities pins the capability cap: a resource carries at
// most MaxCapabilities capabilities, so exactly that many are accepted and the
// next one is refused as a document that cannot be decoded, naming the resource
// and the limit it is over.
func TestInventoryCapsCapabilities(t *testing.T) {
	t.Parallel()

	const resourcePath = "resources[0]"

	t.Run("exactly the cap is accepted", func(t *testing.T) {
		t.Parallel()

		// The vocabulary holds nine capabilities, so the list repeats one of
		// them: the domain de-duplicates capabilities, and the cap is about how
		// many the file lists, not how many survive.
		path := writeFile(t, t.TempDir(), capabilitiesDocument(capabilityCap, `"ipv6"`))

		inv, err := inventoryfile.New(path).Inventory(t.Context())
		if err != nil {
			t.Fatalf("Inventory() returned an error for a resource with exactly %d capabilities: %v", capabilityCap, err)
		}

		if got := len(inv.Resources()); got != 1 {
			t.Errorf("len(Resources()) = %d, want 1", got)
		}
	})

	t.Run("one past the cap is refused", func(t *testing.T) {
		t.Parallel()

		path := writeFile(t, t.TempDir(), capabilitiesDocument(capabilityCap+1, `"ipv6"`))

		_, err := inventoryfile.New(path).Inventory(t.Context())
		if !errors.Is(err, inventoryfile.ErrDecode) {
			t.Fatalf("Inventory() for a resource with %d capabilities returned error %v, want one wrapping %v",
				capabilityCap+1, err, inventoryfile.ErrDecode)
		}

		msg := err.Error()
		if !strings.Contains(msg, resourcePath) {
			t.Errorf("Inventory() error = %q, want it to name the resource %s", msg, text.Quote(resourcePath))
		}

		if !strings.Contains(msg, strconv.Itoa(capabilityCap)) {
			t.Errorf("Inventory() error = %q, want it to name the limit %d", msg, capabilityCap)
		}
	})
}

// TestInventoryCapsCapabilitiesBeforeDecoding pins that the capability cap is
// checked while the document is read, token by token: a resource whose
// capabilities list runs to millions of elements is refused at the first one
// past the cap, and the call allocates memory in proportion to the file, not to
// the capabilities the file claims.
//
// The test is not parallel: TotalAlloc counts the whole process, so a test
// allocating at the same time would be counted here.
func TestInventoryCapsCapabilitiesBeforeDecoding(t *testing.T) {
	// Three and a half million empty capabilities would be a document of
	// 10.5 MB, past MaxFileSize, so the file would be refused for its size and
	// never reach the capability cap. 3.4 million stays under the size cap and
	// still lists far more capabilities than any decoder may read at once:
	// decoding all of them allocates hundreds of megabytes, far past the budget
	// below.
	const capabilities = 3400000

	document := capabilitiesDocument(capabilities, `""`)
	if len(document) >= inventoryfile.MaxFileSize {
		t.Fatalf("the generated document is %d bytes, want fewer than MaxFileSize (%d) so the file is refused for its capability count, not its size",
			len(document), inventoryfile.MaxFileSize)
	}

	path := writeFile(t, t.TempDir(), document)

	runtime.GC()

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	_, err := inventoryfile.New(path).Inventory(t.Context())

	runtime.ReadMemStats(&after)

	if !errors.Is(err, inventoryfile.ErrDecode) {
		t.Fatalf("Inventory() error = %v, want one wrapping %v", err, inventoryfile.ErrDecode)
	}

	const budget = 64 << 20
	if grew := after.TotalAlloc - before.TotalAlloc; grew >= budget {
		t.Errorf("Inventory() allocated %d MiB, want less than %d MiB: the capability cap must refuse the document before its capabilities are decoded",
			grew>>20, budget>>20)
	}
}

// TestInventoryAcceptsSurrogatePair pins the other side of the surrogate rule: a
// pair of escapes that together name one character is a character the file can
// hold, so the ID is the character itself, not the two escapes and not U+FFFD.
func TestInventoryAcceptsSurrogatePair(t *testing.T) {
	t.Parallel()

	resources := inventoryOf(t, writeFile(t, t.TempDir(), surrogatePairDocument)).Resources()
	if len(resources) != 1 {
		t.Fatalf("len(Resources()) = %d, want 1", len(resources))
	}

	// U+1F600 GRINNING FACE, which the file spells as the escape pair.
	if got, want := resources[0].ID, "\U0001F600"; got != want {
		t.Errorf("Resources()[0].ID = %q, want %q", got, want)
	}
}

// validV2Path is the sanitised version 2 fixture the schema version 2 tests
// read: every version 2 field on one resource or another, including a relation
// that points at a resource of the same file.
const validV2Path = "testdata/valid-v2.json"

// ptr returns a pointer to v, for a version 2 field the fixture gives: the
// domain keeps nil, a field the file did not give, apart from the zero value.
func ptr[T any](v T) *T {
	return &v
}

// wantVersion2Inventory is the inventory testdata/valid-v2.json must produce. It
// is built with inventory.New from the same data, so the domain's own rules
// (capabilities and relations sorted, resources in ID order) apply to the
// expectation too: a compute.vm with every size field and a usage, a database
// that is the replica of another, another that depends on it, a block volume
// with a storage size and no vcpus, and an unclassified resource with a
// workload and a cost of zero.
func wantVersion2Inventory(t *testing.T) inventory.Inventory {
	t.Helper()

	inv, err := inventory.New("aws", []inventory.Resource{
		{
			ID:           "i-0123456789abcdef0",
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			Size: &inventory.Size{
				VCPUs:      ptr(2.0),
				MemoryGiB:  ptr(8.0),
				StorageGiB: ptr(100.0),
				Count:      ptr(1),
			},
			Usage:       &inventory.Usage{HoursPerMonth: ptr(730.0)},
			Workload:    "shop",
			MonthlyCost: &inventory.Cost{Amount: ptr(123.45), Currency: "USD"},
		},
		{
			ID:           "db-example",
			RawType:      "AWS::RDS::DBInstance",
			Category:     taxonomy.DatabasePostgreSQL,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.ReadReplicas, taxonomy.PointInTimeRestore},
			Size: &inventory.Size{
				VCPUs:      ptr(0.5),
				MemoryGiB:  ptr(4.0),
				StorageGiB: ptr(200.0),
				Count:      ptr(2),
			},
			Usage:       &inventory.Usage{HoursPerMonth: ptr(744.0)},
			Workload:    "shop",
			Relations:   []inventory.Relation{{Type: inventory.ReplicaOf, Target: "db-primary"}},
			MonthlyCost: &inventory.Cost{Amount: ptr(456.78), Currency: "EUR"},
		},
		{
			ID:       "db-primary",
			RawType:  "AWS::RDS::DBInstance",
			Category: taxonomy.DatabasePostgreSQL,
			Region:   "eu-west-1",
			Size: &inventory.Size{
				VCPUs:      ptr(0.5),
				MemoryGiB:  ptr(4.0),
				StorageGiB: ptr(200.0),
				Count:      ptr(1),
			},
			Relations: []inventory.Relation{{Type: inventory.DependsOn, Target: "db-example"}},
		},
		{
			ID:       "volume-example",
			RawType:  "AWS::EC2::Volume",
			Category: taxonomy.StorageBlock,
			Region:   "eu-west-1",
			Size:     &inventory.Size{StorageGiB: ptr(500.0), Count: ptr(1)},
		},
		{
			ID:          "global-example",
			Category:    taxonomy.Unclassified,
			Workload:    "legacy",
			MonthlyCost: &inventory.Cost{Amount: ptr(0.0), Currency: "EUR"},
		},
	})
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return inv
}

// TestInventoryReadsSchemaVersion2 pins that a version 2 file is read, and that
// every field it adds reaches the inventory: the size with each of its fields,
// the usage profile, the workload, the relations and the source's monthly cost.
func TestInventoryReadsSchemaVersion2(t *testing.T) {
	t.Parallel()

	got := inventoryOf(t, validV2Path)
	want := wantVersion2Inventory(t)

	if diff := cmp.Diff(want.Source(), got.Source()); diff != "" {
		t.Errorf("Source() differs (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(want.Resources(), got.Resources()); diff != "" {
		t.Errorf("Resources() differ (-want +got):\n%s", diff)
	}
}

// TestInventoryReadsEveryTaxonomyCategory pins that a file of schema version 1
// or 2 may hold a resource of every category of the taxonomy, the seven PRD #552
// adds included (ADR 0046), and that a version 2 file gives each of them the size
// fields the format allows it. A value the taxonomy does not know is still
// refused: cache.memcached, the mistake a Redis cache invites, since Memcached is
// not the Redis protocol and no target serves it.
func TestInventoryReadsEveryTaxonomyCategory(t *testing.T) {
	t.Parallel()

	categories := []struct {
		name     string
		category taxonomy.Category
		rawType  string
		// size is the body of the version 2 size object, "" for a category that
		// takes no size.
		size string
		// wantSize is the size a version 2 file with that body produces.
		wantSize *inventory.Size
	}{
		{
			name:     "cache.redis",
			category: taxonomy.CacheRedis,
			rawType:  "AWS::ElastiCache::ReplicationGroup",
			size:     `"memoryGiB": 8, "count": 2`,
			wantSize: &inventory.Size{MemoryGiB: ptr(8.0), Count: ptr(2)},
		},
		{
			name:     "database.mongodb",
			category: taxonomy.DatabaseMongoDB,
			rawType:  "AWS::DocDB::DBCluster",
			size:     `"vcpus": 2, "memoryGiB": 8, "storageGiB": 100, "count": 3`,
			wantSize: &inventory.Size{VCPUs: ptr(2.0), MemoryGiB: ptr(8.0), StorageGiB: ptr(100.0), Count: ptr(3)},
		},
		{
			name:     "network.loadbalancer",
			category: taxonomy.NetworkLoadBalancer,
			rawType:  "AWS::ElasticLoadBalancingV2::LoadBalancer",
		},
		{
			name:     "messaging.topic",
			category: taxonomy.MessagingTopic,
			rawType:  "AWS::SNS::Topic",
		},
		{
			name:     "serverless.container",
			category: taxonomy.ServerlessContainer,
			rawType:  "AWS::AppRunner::Service",
		},
		{
			name:     "registry.container",
			category: taxonomy.RegistryContainer,
			rawType:  "AWS::ECR::Repository",
		},
		{
			name:     "storage.file",
			category: taxonomy.StorageFile,
			rawType:  "AWS::EFS::FileSystem",
			size:     `"storageGiB": 500`,
			wantSize: &inventory.Size{StorageGiB: ptr(500.0)},
		},
	}

	for _, tc := range categories {
		for _, version := range []int{1, 2} {
			t.Run(tc.name+", version "+strconv.Itoa(version), func(t *testing.T) {
				t.Parallel()

				document := `{"schemaVersion":` + strconv.Itoa(version) +
					`,"source":"aws","resources":[{"id":"example-resource",` +
					`"rawType":"` + tc.rawType + `","category":"` + string(tc.category) + `",` +
					`"region":"eu-west-1"`

				if version == 2 && tc.size != "" {
					document += `,"size":{` + tc.size + `}`
				}

				document += `}]}`

				inv := inventoryOf(t, writeFile(t, t.TempDir(), document))

				resources := inv.Resources()
				if len(resources) != 1 {
					t.Fatalf("Inventory() read %d resources, want 1", len(resources))
				}

				if got := resources[0].Category; got != tc.category {
					t.Errorf("Category = %q, want %q", got, tc.category)
				}

				// Version 1 has no size at all, and version 2 keeps exactly the size
				// its field table allows.
				wantSize := tc.wantSize
				if version == 1 {
					wantSize = nil
				}

				if diff := cmp.Diff(wantSize, resources[0].Size); diff != "" {
					t.Errorf("Size mismatch (-want +got):\n%s", diff)
				}
			})
		}
	}

	t.Run("cache.memcached is still refused", func(t *testing.T) {
		t.Parallel()

		document := `{"schemaVersion":2,"source":"aws","resources":[` +
			`{"id":"cache-example","rawType":"AWS::ElastiCache::CacheCluster",` +
			`"category":"cache.memcached","region":"eu-west-1"}]}`

		_, err := inventoryfile.New(writeFile(t, t.TempDir(), document)).Inventory(t.Context())
		if !errors.Is(err, taxonomy.ErrUnknownCategory) {
			t.Fatalf("Inventory() error = %v, want an error wrapping %v", err, taxonomy.ErrUnknownCategory)
		}

		if msg := err.Error(); !strings.Contains(msg, "cache.memcached") {
			t.Errorf("Inventory() error = %q, want it to name cache.memcached", msg)
		}
	})
}

// TestInventoryReadsVersion1And2TwinsAlike pins that the version is a fact about
// the file, not about the resources: a version 1 file and the same bytes with
// only the version changed describe the same inventory.
func TestInventoryReadsVersion1And2TwinsAlike(t *testing.T) {
	t.Parallel()

	one := fixtureBytes(t, validPath)

	const versionKey = `"schemaVersion": 1`
	if !bytes.Contains(one, []byte(versionKey)) {
		t.Fatalf("the fixture %s does not spell %s, so the test cannot build its twin", validPath, versionKey)
	}

	two := bytes.Replace(one, []byte(versionKey), []byte(`"schemaVersion": 2`), 1)

	fromOne := inventoryOf(t, validPath)
	fromTwo := inventoryOf(t, writeFile(t, t.TempDir(), string(two)))

	if diff := cmp.Diff(fromOne.Source(), fromTwo.Source()); diff != "" {
		t.Errorf("Source() of the version 2 twin differs (-version 1 +version 2):\n%s", diff)
	}

	if diff := cmp.Diff(fromOne.Resources(), fromTwo.Resources()); diff != "" {
		t.Errorf("Resources() of the version 2 twin differ (-version 1 +version 2):\n%s", diff)
	}
}

// TestInventoryReadsNegativeZeroAsZero pins that a document that writes -0 is
// read as a given 0, not as a given -0: the JSON number parses with its sign
// bit, and an amount a later milestone prints must never print "-0.00". The
// value stays a given field, so the check refuses an absent one.
func TestInventoryReadsNegativeZeroAsZero(t *testing.T) {
	t.Parallel()

	path := writeFile(t, t.TempDir(), `{"schemaVersion":2,"source":"aws","resources":[`+
		`{"id":"volume-example","category":"storage.block",`+
		`"size":{"storageGiB":-0,"count":1},`+
		`"monthlyCost":{"amount":-0,"currency":"EUR"}}]}`)

	resources := inventoryOf(t, path).Resources()
	if len(resources) != 1 {
		t.Fatalf("len(Resources()) = %d, want 1", len(resources))
	}

	sized := resources[0]
	if sized.Size == nil || sized.Size.StorageGiB == nil {
		t.Fatalf("Resources()[0].Size = %v, want the given -0 kept as a given 0", sized.Size)
	}

	if storage := *sized.Size.StorageGiB; storage != 0 || math.Signbit(storage) {
		t.Errorf("Size.StorageGiB = %v, want +0: a negative zero is read as a zero", storage)
	}

	if sized.MonthlyCost == nil || sized.MonthlyCost.Amount == nil {
		t.Fatalf("Resources()[0].MonthlyCost = %v, want the given -0 kept as a given 0", sized.MonthlyCost)
	}

	if amount := *sized.MonthlyCost.Amount; amount != 0 || math.Signbit(amount) {
		t.Errorf("MonthlyCost.Amount = %v, want +0: a negative zero is read as a zero", amount)
	}
}

// TestInventoryReadsAbsentVersion2FieldsAlike pins the null policy of version 2:
// null is the same as an absent field at every level, and an empty size or usage
// object is absent too, so every spelling of "not given" is one inventory.
func TestInventoryReadsAbsentVersion2FieldsAlike(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// extra is appended to a version 2 resource: the version 2 fields the
		// case gives, or null, or empty.
		extra string
	}{
		{name: "null fields", extra: `,"size":null,"usage":null,"workload":null,"relations":null,"monthlyCost":null`},
		{name: "empty objects", extra: `,"size":{},"usage":{}`},
		{name: "empty relations", extra: `,"relations":[]`},
		{name: "empty workload", extra: `,"workload":""`},
		{name: "null members of size and usage", extra: `,"size":{"vcpus":null,"memoryGiB":null,"storageGiB":null,"count":null},"usage":{"hoursPerMonth":null}`},
	}

	want := inventoryOf(t, writeFile(t, t.TempDir(), schemaDocument(2, "")))

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := inventoryOf(t, writeFile(t, t.TempDir(), schemaDocument(2, tc.extra)))

			if diff := cmp.Diff(want.Source(), got.Source()); diff != "" {
				t.Errorf("Source() differs (-absent +this spelling):\n%s", diff)
			}

			if diff := cmp.Diff(want.Resources(), got.Resources()); diff != "" {
				t.Errorf("Resources() differ (-absent +this spelling):\n%s", diff)
			}
		})
	}
}

// TestInventoryCapsRelations pins the relations cap: a resource carries at most
// MaxRelations relations, counted as the document lists them, duplicates
// included, and refused at the first one past the cap.
func TestInventoryCapsRelations(t *testing.T) {
	t.Parallel()

	const (
		wantCap   = 32
		resource  = "resources[0]"
		relations = "relations"
	)

	if inventoryfile.MaxRelations != wantCap {
		t.Fatalf("inventoryfile.MaxRelations = %d, want %d", inventoryfile.MaxRelations, wantCap)
	}

	t.Run("exactly the cap is accepted", func(t *testing.T) {
		t.Parallel()

		path := writeFile(t, t.TempDir(), relationsDocument(wantCap))

		inv, err := inventoryfile.New(path).Inventory(t.Context())
		if err != nil {
			t.Fatalf("Inventory() returned an error for a resource with exactly %d relations: %v", wantCap, err)
		}

		resources := inv.Resources()
		if len(resources) != 2 {
			t.Fatalf("len(Resources()) = %d, want 2", len(resources))
		}

		// The list repeats one relation, and duplicates are dropped once the
		// document is read: the cap counts what the file lists.
		if got := len(resources[0].Relations); got != 1 {
			t.Errorf("len(Relations()) = %d, want 1: the %d duplicates are one relation", got, wantCap)
		}
	})

	t.Run("one past the cap is refused", func(t *testing.T) {
		t.Parallel()

		path := writeFile(t, t.TempDir(), relationsDocument(wantCap+1))

		_, err := inventoryfile.New(path).Inventory(t.Context())
		if !errors.Is(err, inventoryfile.ErrDecode) {
			t.Fatalf("Inventory() for a resource with %d relations returned error %v, want one wrapping %v",
				wantCap+1, err, inventoryfile.ErrDecode)
		}

		msg := err.Error()
		if !strings.Contains(msg, text.Quote(resource)) {
			t.Errorf("Inventory() error = %q, want it to name the resource %s", msg, text.Quote(resource))
		}

		if !strings.Contains(msg, strconv.Itoa(wantCap)) {
			t.Errorf("Inventory() error = %q, want it to name the limit %d", msg, wantCap)
		}

		if !strings.Contains(strings.ToLower(msg), relations) {
			t.Errorf("Inventory() error = %q, want it to name the %s it is about", msg, relations)
		}
	})
}
