// Package snapshot_test pins the shared target snapshot reader: the document
// format every target with an embedded snapshot reads (schemaVersion 2), the
// strictness a document must meet, the declared region list Parse returns, the
// default region estimates use (ADR 0024), the plans, prices and optional fixed
// monthly fees an offering may carry (ADR 0026) and the exact decimals they are
// written as, and the reader's own error
// contract - one line, the sentinel it wraps, every value shown through
// text.Quote, the keys a document must carry and spell exactly, the nulls and
// duplicate keys it must refuse rather than default, the lone surrogates it must
// refuse rather than read as U+FFFD, the byte order marks (UTF-8 and UTF-16) it
// refuses by name, the line and column it places each problem at, the declared
// regions it must declare once and as valid text (UTF-8 included), and the size
// limit above which a document is refused undecoded. Errors are worded for every
// target: the reader is shared, so no message names one. It also pins that every
// offering of every embedded snapshot has a section in the SOURCES.md beside it,
// and that every plan name and price amount is cited there (see sources_test.go).
// It is black-box: only the package's exported API is used.
package snapshot_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/target/snapshot"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// validDocument is a complete, minimal catalog document: one offering in one
// declared region, which sells no plan and publishes no storage price yet. The
// error table replaces one line of it to break exactly one thing. It uses a
// made-up target and domain, so no case can pass by agreeing with one provider's
// snapshot.
const validDocument = `{
  "schemaVersion": 2,
  "target": "Example Cloud",
  "sourceURL": "https://www.example.com/docs/",
  "retrievedOn": "2026-11-10",
  "regions": ["fr-par"],
  "defaultRegion": "fr-par",
  "offerings": [
    {
      "name": "Example Compute",
      "categories": ["compute.vm"],
      "capabilities": ["ipv6"],
      "regions": ["fr-par"],
      "plans": [],
      "storagePrices": []
    }
  ]
}`

// pricedDocument is validDocument with what a real snapshot will hold once a
// target lists its prices (ADR 0024): two plans and a storage price, written out
// one key per line so a case can break one value at a known line and column. The
// first plan's prices are deliberately out of order, and one repeats a region, so
// a case can pin the canonical order and de-duplication Parse gives them.
const pricedDocument = `{
  "schemaVersion": 2,
  "target": "Example Cloud",
  "sourceURL": "https://www.example.com/docs/",
  "retrievedOn": "2026-11-10",
  "regions": ["fr-par", "nl-ams"],
  "defaultRegion": "fr-par",
  "offerings": [
    {
      "name": "Example Compute",
      "categories": ["compute.vm"],
      "capabilities": ["ipv6"],
      "regions": ["fr-par", "nl-ams"],
      "plans": [
        {
          "name": "EC-2-4",
          "vcpus": 2,
          "memoryGiB": 4,
          "storageGiB": 20,
          "prices": [
            { "amount": 12.5, "currency": "EUR", "regions": ["nl-ams"] },
            { "amount": 13.0125, "currency": "EUR", "regions": ["fr-par", "fr-par"] }
          ]
        },
        {
          "name": "EC-1-0.5",
          "vcpus": 1,
          "memoryGiB": 0.5,
          "storageGiB": 0,
          "prices": [
            { "amount": 3.65, "currency": "EUR", "regions": [] }
          ]
        }
      ],
      "storagePrices": [
        { "amount": 0.04, "currency": "EUR", "regions": [] }
      ]
    }
  ]
}`

// doc returns validDocument with each old, new pair applied in order, so a case
// shows the one thing it breaks and leaves every other field valid. A pair whose
// old text is absent fails the test rather than leaving the document valid by
// accident.
func doc(t *testing.T, replacements ...string) string {
	t.Helper()

	return docIn(t, validDocument, replacements...)
}

// docIn is doc over any base document, so a case can break one value of
// pricedDocument, which holds the plans and prices validDocument does not.
func docIn(t *testing.T, base string, replacements ...string) string {
	t.Helper()

	data := base

	for i := 0; i+1 < len(replacements); i += 2 {
		old, replacement := replacements[i], replacements[i+1]

		if !strings.Contains(data, old) {
			t.Fatalf("docIn(): %s is not in the document", text.Quote(old))
		}

		data = strings.Replace(data, old, replacement, 1)
	}

	return data
}

// fixedFeesDocument returns pricedDocument with the one optional key of the
// format on its offering (ADR 0026): a fixed monthly fee per instance that
// covers both of the offering's regions, a paid one in fr-par and a fee of 0 in
// nl-ams, written out of order and with a region repeated, so a case can pin
// their canonical order and de-duplication. The first fee is 65.7 EUR, the
// control plane of one cluster: 65.7 x 10000 is 657000 ten-thousandths.
func fixedFeesDocument(t *testing.T) string {
	t.Helper()

	return docIn(t, pricedDocument, `      "storagePrices": [`,
		`      "fixedFees": [
        { "amount": 65.7, "currency": "EUR", "regions": ["nl-ams", "nl-ams"] },
        { "amount": 0, "currency": "EUR", "regions": ["fr-par"] }
      ],
      "storagePrices": [`)
}

// planBillingCapDocument returns pricedDocument with the format's second
// optional key on its offering (ADR 0029): the offering bills one instance of
// one of its plans for at most 672 hours a month, 24 h × 28 days, so its plan
// prices are the price of that many hours.
func planBillingCapDocument(t *testing.T) string {
	t.Helper()

	return offeringDoc(t, func(offering map[string]json.RawMessage) {
		offering["planBillingCapHours"] = json.RawMessage("672")
	})
}

// padTo returns data padded with trailing spaces to exactly n bytes. White space
// after the catalog object is allowed, so the result is the same document.
func padTo(t *testing.T, data string, n int) []byte {
	t.Helper()

	if len(data) > n {
		t.Fatalf("padTo(): the document is %d bytes, longer than %d", len(data), n)
	}

	return append([]byte(data), bytes.Repeat([]byte(" "), n-len(data))...)
}

// topFields returns validDocument's top-level keys and their raw values, so a
// case can change or remove one without touching the rest. Re-encoding a map of
// raw fields keeps every nested value as it is; only the order of the top-level
// keys changes, which no test here depends on.
func topFields(t *testing.T) map[string]json.RawMessage {
	t.Helper()

	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(validDocument), &fields); err != nil {
		t.Fatalf("unmarshalling validDocument: %v", err)
	}

	return fields
}

// marshalFields renders the top-level fields back as a document.
func marshalFields(t *testing.T, fields map[string]json.RawMessage) string {
	t.Helper()

	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshalling the document's top-level fields: %v", err)
	}

	return string(data)
}

// without returns validDocument with the top-level key removed, so a case pins a
// key the format requires.
func without(t *testing.T, key string) string {
	t.Helper()

	fields := topFields(t)

	if _, ok := fields[key]; !ok {
		t.Fatalf("without(): validDocument has no %s key", text.Quote(key))
	}

	delete(fields, key)

	return marshalFields(t, fields)
}

// with returns validDocument with the top-level key set to the raw JSON value,
// so a case pins a value the whole key takes, such as null.
func with(t *testing.T, key, raw string) string {
	t.Helper()

	fields := topFields(t)

	if _, ok := fields[key]; !ok {
		t.Fatalf("with(): validDocument has no %s key", text.Quote(key))
	}

	fields[key] = json.RawMessage(raw)

	return marshalFields(t, fields)
}

// offeringDoc returns validDocument with edit applied to the raw fields of its
// one offering, so a case changes or removes exactly one offering key and leaves
// the header and every other offering key valid.
func offeringDoc(t *testing.T, edit func(offering map[string]json.RawMessage)) string {
	t.Helper()

	fields := topFields(t)

	var offerings []map[string]json.RawMessage
	if err := json.Unmarshal(fields["offerings"], &offerings); err != nil {
		t.Fatalf("unmarshalling validDocument's offerings: %v", err)
	}

	if len(offerings) != 1 {
		t.Fatalf("validDocument has %d offerings, want 1", len(offerings))
	}

	edit(offerings[0])

	raw, err := json.Marshal(offerings)
	if err != nil {
		t.Fatalf("marshalling the offering's fields: %v", err)
	}

	fields["offerings"] = raw

	return marshalFields(t, fields)
}

// withOffering returns validDocument with the offering's key set to the raw JSON
// value, so a case pins a value the whole key takes, such as null.
func withOffering(t *testing.T, key, raw string) string {
	t.Helper()

	return offeringDoc(t, func(offering map[string]json.RawMessage) {
		if _, ok := offering[key]; !ok {
			t.Fatalf("withOffering(): validDocument's offering has no %s key", text.Quote(key))
		}

		offering[key] = json.RawMessage(raw)
	})
}

// withoutOffering returns validDocument with the offering's key removed, so a
// case pins an offering key the format requires.
func withoutOffering(t *testing.T, key string) string {
	t.Helper()

	return offeringDoc(t, func(offering map[string]json.RawMessage) {
		if _, ok := offering[key]; !ok {
			t.Fatalf("withoutOffering(): validDocument's offering has no %s key", text.Quote(key))
		}

		delete(offering, key)
	})
}

// The places pricedDocument's plans and prices sit at, as editAt takes them.
var (
	planPath         = []any{"offerings", 0, "plans", 0}
	secondPlanPath   = []any{"offerings", 0, "plans", 1}
	pricePath        = []any{"offerings", 0, "plans", 0, "prices", 0}
	storagePricePath = []any{"offerings", 0, "storagePrices", 0}
	fixedFeePath     = []any{"offerings", 0, "fixedFees", 0}
)

// editAt returns base with edit applied to the JSON object at path, re-encoding
// only the levels above it: path is a list of object keys and array indexes,
// starting at base, which must hold that object. It is how a case breaks exactly
// one key of a plan or a price and leaves every other value as written.
func editAt(t *testing.T, base string, path []any, edit func(map[string]json.RawMessage)) string {
	t.Helper()

	return string(editLevel(t, json.RawMessage(base), path, edit))
}

// editLevel is editAt's step: it descends one level of path and re-encodes the
// value it changed.
func editLevel(t *testing.T, raw json.RawMessage, path []any, edit func(map[string]json.RawMessage)) json.RawMessage {
	t.Helper()

	if len(path) == 0 {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatalf("editAt(): unmarshalling the object to edit: %v", err)
		}

		edit(fields)

		return marshalJSON(t, fields)
	}

	switch step := path[0].(type) {
	case string:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatalf("editAt(): unmarshalling an object: %v", err)
		}

		value, ok := fields[step]
		if !ok {
			t.Fatalf("editAt(): the document has no %s key", text.Quote(step))
		}

		fields[step] = editLevel(t, value, path[1:], edit)

		return marshalJSON(t, fields)
	case int:
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			t.Fatalf("editAt(): unmarshalling an array: %v", err)
		}

		if step >= len(values) {
			t.Fatalf("editAt(): the document has %d array elements, want one at index %d", len(values), step)
		}

		values[step] = editLevel(t, values[step], path[1:], edit)

		return marshalJSON(t, values)
	default:
		t.Fatalf("editAt(): a path step is %T, want a key or an index", step)

		return nil
	}
}

// marshalJSON renders v back as JSON, failing the test rather than returning a
// document no case meant to write.
func marshalJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshalling an edited value: %v", err)
	}

	return raw
}

// withNested returns base with the key of the object at path set to the raw JSON
// value, so a case pins a value a whole plan or price key takes, such as null.
func withNested(t *testing.T, base string, path []any, key, raw string) string {
	t.Helper()

	if !json.Valid([]byte(raw)) {
		t.Fatalf("withNested(): %s is not a JSON value", text.Quote(raw))
	}

	return editAt(t, base, path, func(fields map[string]json.RawMessage) {
		if _, ok := fields[key]; !ok {
			t.Fatalf("withNested(): the object at %v has no %s key", path, text.Quote(key))
		}

		fields[key] = json.RawMessage(raw)
	})
}

// withoutNested returns base with the key of the object at path removed, so a
// case pins a key of a plan or a price the format requires.
func withoutNested(t *testing.T, base string, path []any, key string) string {
	t.Helper()

	return editAt(t, base, path, func(fields map[string]json.RawMessage) {
		if _, ok := fields[key]; !ok {
			t.Fatalf("withoutNested(): the object at %v has no %s key", path, text.Quote(key))
		}

		delete(fields, key)
	})
}

// TestParseErrors pins every way parsing a catalog document fails: the sentinel
// the error wraps, and that the message is one line naming the offending value
// through text.Quote. A document the reader cannot understand is never silently
// dropped or defaulted: an offering in a region the file does not declare or
// with an unknown capability must fail, and so must a key given twice, a key
// spelt differently from the format, a null or missing field, and a string with
// an escaped lone surrogate, which the JSON decoder on its own would read as
// U+FFFD.
func TestParseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		// wantErr lists every sentinel the error must wrap. An empty list pins
		// that the document parses.
		wantErr []error
		// wantNotErr lists sentinels the error must not wrap, so a case pins
		// which of two plausible sentinels is the right one.
		wantNotErr []error
		// wantQuoted lists values the message must show through text.Quote.
		wantQuoted []string
		// wantContains lists substrings the message must contain and wantAbsent
		// ones it must not, for wording no single text.Quote value carries.
		wantContains []string
		wantAbsent   []string
		// wantPosition is where the message must place the problem: a 1-based
		// line and column in the document, never a byte offset. It is counted
		// from the document's own text, in characters, so a multi-byte one
		// counts once.
		wantPosition string
	}{
		{
			name: "valid document",
			data: validDocument,
		},
		{
			name:    "not JSON",
			data:    "not json",
			wantErr: []error{snapshot.ErrDecode},
		},
		{
			name: "unknown top-level field",
			data: doc(t, `  "schemaVersion": 2,`,
				"  \"schemaVersion\": 2,\n  \"bogusField\": true,"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"bogusField"},
			wantContains: []string{"unknown key"},
		},
		{
			name: "unknown offering field",
			data: doc(t, `      "name": "Example Compute",`,
				"      \"name\": \"Example Compute\",\n      \"bogusOfferingField\": \"x\","),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"bogusOfferingField"},
			wantContains: []string{"unknown key"},
		},
		{
			// encoding/json keeps the last of two equal keys, so without a check
			// the first value would be dropped without a word.
			name: "top-level key given twice",
			data: doc(t, `  "target": "Example Cloud",`,
				"  \"target\": \"Example Cloud\",\n  \"target\": \"Example Cloud\","),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"target"},
			wantContains: []string{"repeated key"},
			// The second of the two lines is the one refused, and its key
			// starts after the two spaces that indent it.
			wantPosition: "line 4, column 3",
		},
		{
			name: "offering key given twice",
			data: doc(t, `      "regions": ["fr-par"]`,
				"      \"regions\": [\"fr-par\"],\n      \"regions\": [\"fr-par\"]"),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].regions"},
			// The repeated line is the offering's first regions line, indented six
			// spaces, so its key starts in column 7.
			wantContains: []string{"repeated key"},
			wantPosition: "line 14, column 7",
		},
		{
			// encoding/json matches field names without regard to case, so a key
			// spelt differently from the format would be read as the right one
			// instead of being refused.
			name:         "top-level key not spelt as the format spells it",
			data:         doc(t, `  "regions": ["fr-par"],`, `  "Regions": ["fr-par"],`),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"Regions", "regions"},
			wantContains: []string{"unknown key", "keys are exact"},
		},
		{
			name: "top-level URL key not spelt as the format spells it",
			data: doc(t, `  "sourceURL": "https://www.example.com/docs/",`,
				`  "sourceUrl": "https://www.example.com/docs/",`),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"sourceUrl", "sourceURL"},
			wantContains: []string{"unknown key", "keys are exact"},
		},
		{
			name: "offering key not spelt as the format spells it",
			data: doc(t, `      "name": "Example Compute",`,
				"      \"name\": \"Example Compute\",\n      \"Name\": \"Example Compute\","),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings[0].Name", "name"},
			wantContains: []string{"unknown key", "keys are exact"},
		},
		{
			// What follows is described by what it follows: the first JSON value,
			// whatever its type, not an object the document never had to be. The
			// {} starts on the document's last line, just after the object's brace.
			name:         "trailing data after the object",
			data:         validDocument + "{}",
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"unexpected data after the first JSON value"},
			wantPosition: "line 18, column 2",
			wantAbsent:   []string{"catalog object"},
		},
		{
			// The first value need not be an object for the reader to say what
			// comes after it: "a" is the first JSON value, and the second one
			// starts after the single space between them.
			name:         "first value that is not an object followed by more data",
			data:         `"a" "b"`,
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"unexpected data after the first JSON value"},
			wantPosition: "line 1, column 5",
			wantAbsent:   []string{"catalog object"},
		},
		{
			// A version this package does not read is refused for its version: the
			// reader reads version 2 only, so a version 1 document, which every
			// snapshot held before ADR 0024, is named by the version it is.
			name:         "schema version 1",
			data:         doc(t, `"schemaVersion": 2`, `"schemaVersion": 1`),
			wantErr:      []error{snapshot.ErrSchemaVersion},
			wantNotErr:   []error{snapshot.ErrDecode},
			wantContains: []string{"is 1, want the integer 2"},
		},
		{
			// An integer version is named by its value alone: the message never
			// quotes a number the document spelt as an integer.
			name:         "unsupported schema version",
			data:         doc(t, `"schemaVersion": 2`, `"schemaVersion": 3`),
			wantErr:      []error{snapshot.ErrSchemaVersion},
			wantContains: []string{"is 3, want the integer 2"},
		},
		{
			name:         "schema version written as the integer 0",
			data:         doc(t, `"schemaVersion": 2`, `"schemaVersion": 0`),
			wantErr:      []error{snapshot.ErrSchemaVersion},
			wantContains: []string{"is 0, want the integer 2"},
		},
		{
			// -0 is the integer 0 in a spelling that says more than its value:
			// the message shows the number as written, so the reader says what
			// the document actually holds, not a normalised form of it.
			name:       "schema version written as -0",
			data:       doc(t, `"schemaVersion": 2`, `"schemaVersion": -0`),
			wantErr:    []error{snapshot.ErrSchemaVersion},
			wantQuoted: []string{"-0"},
			wantAbsent: []string{"is 0,"},
		},
		{
			// 2.0 and 2e0 are the integer 2 in another spelling, which the
			// reader refuses just the same, and names as the number it is not
			// allowed to normalise: 2 alone would be a version the document
			// does not contain.
			name:       "schema version written as 2.0",
			data:       doc(t, `"schemaVersion": 2`, `"schemaVersion": 2.0`),
			wantErr:    []error{snapshot.ErrSchemaVersion},
			wantQuoted: []string{"2.0"},
			wantAbsent: []string{"is 2,"},
		},
		{
			name:       "schema version written as 2e0",
			data:       doc(t, `"schemaVersion": 2`, `"schemaVersion": 2e0`),
			wantErr:    []error{snapshot.ErrSchemaVersion},
			wantQuoted: []string{"2e0"},
			wantAbsent: []string{"is 2,"},
		},
		{
			name:    "missing schema version",
			data:    doc(t, "  \"schemaVersion\": 2,\n", ""),
			wantErr: []error{snapshot.ErrSchemaVersion},
		},
		{
			// encoding/json would read SchemaVersion as schemaVersion. The version
			// is refused for itself alone, and the message names the key the
			// writer spelt so the mistake is visible.
			name:         "schema version key not spelt as the format spells it",
			data:         doc(t, `"schemaVersion": 2`, `"SchemaVersion": 2`),
			wantErr:      []error{snapshot.ErrSchemaVersion},
			wantNotErr:   []error{snapshot.ErrDecode},
			wantQuoted:   []string{"SchemaVersion"},
			wantContains: []string{"keys are exact", "is not schemaVersion"},
		},
		{
			// Every declared region is a promise the file keeps: an offering in a
			// region the header does not declare would reach a report claiming a
			// geography the snapshot was never checked for.
			name:       "offering region not in the declared regions",
			data:       doc(t, `      "regions": ["fr-par"]`, `      "regions": ["fr-par", "xx-yy"]`),
			wantErr:    []error{snapshot.ErrUndeclaredRegion},
			wantQuoted: []string{"Example Compute", "xx-yy"},
		},
		{
			name:       "unknown capability",
			data:       doc(t, `"capabilities": ["ipv6"]`, `"capabilities": ["bogus-capability"]`),
			wantErr:    []error{taxonomy.ErrUnknownCapability},
			wantQuoted: []string{"bogus-capability"},
		},
		{
			name:    "retrieval date that is not a date",
			data:    doc(t, `"retrievedOn": "2026-11-10"`, `"retrievedOn": "2026-13-01"`),
			wantErr: []error{snapshot.ErrDecode},
		},
		{
			// The domain rejects a source URL that is not https, without echoing
			// it: a URL may carry credentials.
			name:    "source URL that is not https",
			data:    doc(t, "https://www.example.com/docs/", "http://www.example.com/docs/"),
			wantErr: []error{catalog.ErrInvalidCatalog},
		},
		{
			// A document of a version this package does not read is refused for
			// its version alone: a field only that version defines is no reason
			// to report it as unreadable.
			name: "schema version 3 carrying a field version 2 lacks",
			data: doc(t,
				`"schemaVersion": 2`, `"schemaVersion": 3`,
				`  "target": "Example Cloud",`,
				"  \"target\": \"Example Cloud\",\n  \"account\": \"123456789012\","),
			wantErr:    []error{snapshot.ErrSchemaVersion},
			wantNotErr: []error{snapshot.ErrDecode},
		},
		{
			// A duplicate key in a document of another version is the version's
			// fault alone for the same reason: the strictness checks never run.
			name: "schema version 3 with a key given twice",
			data: doc(t,
				`"schemaVersion": 2`, `"schemaVersion": 3`,
				`  "target": "Example Cloud",`,
				"  \"target\": \"Example Cloud\",\n  \"target\": \"Example Cloud\","),
			wantErr:    []error{snapshot.ErrSchemaVersion},
			wantNotErr: []error{snapshot.ErrDecode},
		},
		{
			// The version is a JSON number: a quoted number is another value, and
			// naming it must not quote the quotes.
			name:         "schema version as a string",
			data:         doc(t, `"schemaVersion": 2`, `"schemaVersion": "2"`),
			wantErr:      []error{snapshot.ErrSchemaVersion},
			wantContains: []string{"string", "\"2\""},
			wantAbsent:   []string{`\"`},
		},
		{
			// A field of the wrong type names the field, not the decoder's text.
			name:         "field of the wrong type",
			data:         doc(t, `"target": "Example Cloud"`, `"target": 5`),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"target"},
			wantContains: []string{"is a number", "want a string"},
		},
		{
			name:    "top-level array",
			data:    `[{"schemaVersion": 2}]`,
			wantErr: []error{snapshot.ErrDecode},
		},
		{
			name:    "top-level string",
			data:    `"a catalog"`,
			wantErr: []error{snapshot.ErrDecode},
		},
		{
			// null is a document of no schema version at all, not a document of
			// an unsupported one: the version check never runs.
			name:       "top-level null",
			data:       "null",
			wantErr:    []error{snapshot.ErrDecode},
			wantNotErr: []error{snapshot.ErrSchemaVersion},
		},
		{
			name:    "missing retrieval date",
			data:    doc(t, "  \"retrievedOn\": \"2026-11-10\",\n", ""),
			wantErr: []error{snapshot.ErrDecode},
		},
		{
			// The two header keys a catalog cannot exist without: a document
			// that omits them describes no offerings or declares no regions,
			// which is a mistake in the document, not an empty catalog.
			name:         "missing offerings key",
			data:         without(t, "offerings"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings"},
			wantContains: []string{"required key", "is missing"},
		},
		{
			name:         "missing regions key",
			data:         without(t, "regions"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"regions"},
			wantContains: []string{"required key", "is missing"},
		},
		{
			// A header field the document leaves out says nothing: read as the
			// empty value, it would be a document the reader silently filled in.
			name:         "missing target",
			data:         without(t, "target"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"target"},
			wantContains: []string{"required key", "is missing"},
		},
		{
			name:         "missing source URL",
			data:         without(t, "sourceURL"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"sourceURL"},
			wantContains: []string{"required key", "is missing"},
		},
		{
			// null is not an empty value either: a field given as null would be
			// read as the zero value, which is a value the document never said.
			name:         "null target",
			data:         with(t, "target", "null"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"target"},
			wantContains: []string{"is null", "want a string", "at line ", ", column "},
		},
		{
			name:         "null source URL",
			data:         with(t, "sourceURL", "null"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"sourceURL"},
			wantContains: []string{"is null", "want a string"},
		},
		{
			name:         "null retrieval date",
			data:         with(t, "retrievedOn", "null"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"retrievedOn"},
			wantContains: []string{"is null", "want a string"},
		},
		{
			// null is not an empty list: a header that says regions: null
			// says nothing it means to say, which is a mistake in the document.
			name:         "null regions list",
			data:         with(t, "regions", "null"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"regions"},
			wantContains: []string{"is null", "want an array of strings"},
		},
		{
			name:         "null offerings list",
			data:         with(t, "offerings", "null"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings"},
			wantContains: []string{"is null", "want an array of objects"},
		},
		{
			name:         "missing offering name",
			data:         withoutOffering(t, "name"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings[0]"},
			wantContains: []string{"required key", "is missing"},
		},
		{
			name:         "missing offering categories",
			data:         withoutOffering(t, "categories"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings[0]"},
			wantContains: []string{"required key", "is missing"},
		},
		{
			// catalog.New accepts an offering with no capabilities, so without
			// this check a document that forgot the key would read as an offering
			// with none: silently defaulted. The message says which offering it is
			// by where that offering starts, the brace on line 8 in column 5.
			name:         "missing offering capabilities",
			data:         doc(t, "      \"capabilities\": [\"ipv6\"],\n", ""),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings[0]"},
			wantContains: []string{"required key", "is missing", "which starts at line 9, column 5"},
			wantPosition: "line 9, column 5",
		},
		{
			name:         "missing offering regions",
			data:         withoutOffering(t, "regions"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings[0]"},
			wantContains: []string{"required key", "is missing"},
		},
		{
			name:         "null offering name",
			data:         doc(t, `      "name": "Example Compute",`, `      "name": null,`),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings[0].name"},
			wantContains: []string{"is null", "want a string"},
			// null is placed where it is written: after the six spaces, the
			// quoted key and the colon, which leaves column 15.
			wantPosition: "line 10, column 15",
		},
		{
			name:         "null offering categories",
			data:         withOffering(t, "categories", "null"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings[0].categories"},
			wantContains: []string{"is null", "want an array of strings"},
		},
		{
			name:         "null offering capabilities",
			data:         withOffering(t, "capabilities", "null"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings[0].capabilities"},
			wantContains: []string{"is null", "want an array of strings"},
		},
		{
			name:         "null offering regions",
			data:         withOffering(t, "regions", "null"),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings[0].regions"},
			wantContains: []string{"is null", "want an array of strings"},
		},
		{
			// An explicit empty list is a value the document stated, and an
			// offering with no capability is a real offering: it is accepted.
			name: "offering with an empty capabilities list",
			data: withOffering(t, "capabilities", "[]"),
		},
		{
			// A complete version 2 document with plans, prices and a storage
			// price is what every target will publish once it lists its prices.
			name: "valid document with plans and prices",
			data: pricedDocument,
		},
		{
			// A plan may hold a price in some of the offering's regions only: the
			// provider's coverage is a fact of the price list, not an error.
			name: "price for some of the offering's regions",
			data: withNested(t, pricedDocument, secondPlanPath, "prices", `[{ "amount": 3.65, "currency": "EUR", "regions": ["nl-ams"] }]`),
		},
		{
			name: "memory with three fractional digits",
			data: docIn(t, pricedDocument, `"memoryGiB": 4,`, `"memoryGiB": 1.875,`),
		},
		{
			// The default region is one of the declared regions, spelt as the
			// document declares it: anything else would estimate in a region the
			// snapshot never declared (ADR 0024).
			name:         "default region that is not declared",
			data:         docIn(t, validDocument, `"defaultRegion": "fr-par",`, `"defaultRegion": "fr-paris",`),
			wantErr:      []error{snapshot.ErrUndeclaredDefaultRegion},
			wantNotErr:   []error{snapshot.ErrUndeclaredRegion, snapshot.ErrDecode},
			wantQuoted:   []string{"fr-paris"},
			wantContains: []string{"declared region"},
		},
		{
			name:       "default region in another case than the declaration",
			data:       docIn(t, validDocument, `"defaultRegion": "fr-par",`, `"defaultRegion": "FR-PAR",`),
			wantErr:    []error{snapshot.ErrUndeclaredDefaultRegion},
			wantQuoted: []string{"FR-PAR"},
		},
		{
			// A snapshot that declares no region has no region to estimate in, so
			// it cannot be read at all (ADR 0024).
			name:       "default region when the document declares no region",
			data:       docIn(t, validDocument, `"regions": ["fr-par"],`, `"regions": [],`),
			wantErr:    []error{snapshot.ErrUndeclaredDefaultRegion},
			wantNotErr: []error{snapshot.ErrUndeclaredRegion},
		},
		{
			name:       "blank default region",
			data:       docIn(t, validDocument, `"defaultRegion": "fr-par",`, `"defaultRegion": "",`),
			wantErr:    []error{snapshot.ErrUndeclaredDefaultRegion},
			wantNotErr: []error{snapshot.ErrDecode},
		},
		// A price is an exact decimal read from its JSON text, never a float64: an
		// exponent, a fraction where an integer belongs, more fractional digits
		// than the format allows and an amount outside int64 are refused with the
		// line and column of the number and its path in the document.
		{
			name:         "price amount with an exponent",
			data:         docIn(t, pricedDocument, `"amount": 3.65,`, `"amount": 1e2,`),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings[0].plans[1].prices[0].amount", "1e2"},
			wantContains: []string{"the number"},
			wantPosition: "line 31, column 25",
		},
		{
			name:       "price amount with five fractional digits",
			data:       docIn(t, pricedDocument, `"amount": 12.5,`, `"amount": 12.34567,`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].plans[0].prices[0].amount", "12.34567"},
		},
		{
			// Trailing zeros count towards the limit: 1.00000 says five fractional
			// digits even though it is the integer 1.
			name:       "price amount with a trailing zero past four fractional digits",
			data:       docIn(t, pricedDocument, `"amount": 12.5,`, `"amount": 1.00000,`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].plans[0].prices[0].amount", "1.00000"},
		},
		{
			name:       "memory with four fractional digits",
			data:       docIn(t, pricedDocument, `"memoryGiB": 4,`, `"memoryGiB": 0.5000,`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].plans[0].memoryGiB", "0.5000"},
		},
		{
			name:       "vcpus written as a decimal",
			data:       docIn(t, pricedDocument, `"vcpus": 2,`, `"vcpus": 1.0,`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].plans[0].vcpus", "1.0"},
		},
		{
			name:       "vcpus written with an exponent",
			data:       docIn(t, pricedDocument, `"vcpus": 2,`, `"vcpus": 1e0,`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].plans[0].vcpus", "1e0"},
		},
		{
			name:       "included storage written as a decimal",
			data:       docIn(t, pricedDocument, `"storageGiB": 20,`, `"storageGiB": 1.0,`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].plans[0].storageGiB", "1.0"},
		},
		{
			// -0 is never what a price list means, and an exact reader that read it
			// as 0 would drop the sign without a word, so the reader refuses every
			// spelling of it.
			name:       "price amount written as -0",
			data:       docIn(t, pricedDocument, `"amount": 12.5,`, `"amount": -0,`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].plans[0].prices[0].amount", "-0"},
		},
		{
			name:       "price amount written as -0.0",
			data:       docIn(t, pricedDocument, `"amount": 12.5,`, `"amount": -0.0,`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].plans[0].prices[0].amount", "-0.0"},
		},
		{
			name:       "memory written as -0.000",
			data:       docIn(t, pricedDocument, `"memoryGiB": 4,`, `"memoryGiB": -0.000,`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].plans[0].memoryGiB", "-0.000"},
		},
		{
			// -0 is refused whatever the number holds: the sign on a zero integer
			// is as meaningless as one on a decimal, and a reader that read it as 0
			// would drop the sign without a word (ADR 0024).
			name:         "vcpus written as -0",
			data:         docIn(t, pricedDocument, `"vcpus": 2,`, `"vcpus": -0,`),
			wantErr:      []error{snapshot.ErrDecode},
			wantNotErr:   []error{catalog.ErrInvalidPlan},
			wantQuoted:   []string{"offerings[0].plans[0].vcpus", "-0"},
			wantContains: []string{"the number"},
		},
		{
			name:         "included storage written as -0",
			data:         docIn(t, pricedDocument, `"storageGiB": 20,`, `"storageGiB": -0,`),
			wantErr:      []error{snapshot.ErrDecode},
			wantNotErr:   []error{catalog.ErrInvalidPlan},
			wantQuoted:   []string{"offerings[0].plans[0].storageGiB", "-0"},
			wantContains: []string{"the number"},
		},
		{
			// The amount is held as a count of ten-thousandths, so an amount whose
			// count does not fit an int64 is refused even though its integer part
			// alone would.
			name:       "price amount outside int64",
			data:       docIn(t, pricedDocument, `"amount": 12.5,`, `"amount": 922337203685477.5808,`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].plans[0].prices[0].amount"},
		},
		{
			name:       "vcpus outside int64",
			data:       docIn(t, pricedDocument, `"vcpus": 2,`, `"vcpus": 9223372036854775808,`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].plans[0].vcpus"},
		},
		{
			name:       "memory outside int64",
			data:       docIn(t, pricedDocument, `"memoryGiB": 4,`, `"memoryGiB": 9223372036854775.808,`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].plans[0].memoryGiB"},
		},
		// fixedFees is the format's one optional key (ADR 0026): left out, the
		// offering charges no fixed fee; present, it holds at least one price,
		// and every number and price rule of ADR 0024 holds for each of them.
		{
			name: "valid document with a fixed fee per region",
			data: fixedFeesDocument(t),
		},
		{
			// An empty array is a second spelling of "no fee": the one is to
			// leave the key out.
			name:         "empty fixed fee list",
			data:         withNested(t, fixedFeesDocument(t), offeringPath, "fixedFees", `[]`),
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"offerings[0].fixedFees", "empty"},
		},
		{
			name:         "null fixed fee list",
			data:         withNested(t, fixedFeesDocument(t), offeringPath, "fixedFees", `null`),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings[0].fixedFees"},
			wantContains: []string{"is null"},
		},
		{
			name:       "fixed fee list that is not an array",
			data:       withNested(t, fixedFeesDocument(t), offeringPath, "fixedFees", `{}`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].fixedFees"},
		},
		{
			name:       "fixed fee that is not a price object",
			data:       withNested(t, fixedFeesDocument(t), offeringPath, "fixedFees", `[null]`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].fixedFees[0]"},
		},
		{
			name:         "fixed fee amount with an exponent",
			data:         withNested(t, fixedFeesDocument(t), fixedFeePath, "amount", `6.57e1`),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings[0].fixedFees[0].amount", "6.57e1"},
			wantContains: []string{"the number"},
		},
		{
			name:       "fixed fee amount with five fractional digits",
			data:       withNested(t, fixedFeesDocument(t), fixedFeePath, "amount", `65.70000`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].fixedFees[0].amount", "65.70000"},
		},
		{
			name:       "fixed fee amount written as -0",
			data:       withNested(t, fixedFeesDocument(t), fixedFeePath, "amount", `-0`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].fixedFees[0].amount", "-0"},
		},
		{
			name:       "fixed fee amount given as a string",
			data:       withNested(t, fixedFeesDocument(t), fixedFeePath, "amount", `"65.7"`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"offerings[0].fixedFees[0].amount"},
		},
		{
			name: "fixedFees key given twice",
			data: docIn(t, fixedFeesDocument(t),
				`      "fixedFees": [`, `      "fixedFees": [], "fixedFees": [`),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings[0].fixedFees"},
			wantContains: []string{"repeated key"},
		},
		{
			// A negative fee is read: the sign is part of the grammar, and the rule
			// that a fee is not negative lives once, in the domain.
			name:         "negative fixed fee amount reaches the domain",
			data:         withNested(t, fixedFeesDocument(t), fixedFeePath, "amount", `-0.01`),
			wantErr:      []error{catalog.ErrInvalidPrice},
			wantNotErr:   []error{snapshot.ErrDecode},
			wantQuoted:   []string{"Example Compute"},
			wantContains: []string{"fixedFees[0]", "amount"},
		},
		{
			name:         "fixed fee in a currency that is not EUR",
			data:         withNested(t, fixedFeesDocument(t), fixedFeePath, "currency", `"USD"`),
			wantErr:      []error{catalog.ErrInvalidPrice},
			wantQuoted:   []string{"USD"},
			wantContains: []string{"fixedFees[0]", "currency"},
		},
		{
			// A fee region is checked against the offering's regions, as a price
			// region is: the reader has already checked those against the declared
			// ones.
			name:         "fixed fee region the offering is not in",
			data:         withNested(t, fixedFeesDocument(t), fixedFeePath, "regions", `["de-fra"]`),
			wantErr:      []error{catalog.ErrPriceRegion},
			wantNotErr:   []error{snapshot.ErrUndeclaredRegion, snapshot.ErrDecode},
			wantQuoted:   []string{"de-fra"},
			wantContains: []string{"fixedFees[0]"},
		},
		{
			// Never zero for unknown: a region of the offering that no fee covers
			// would estimate as a free control plane (ADR 0026).
			name: "offering region left without a fixed fee",
			data: withNested(t, fixedFeesDocument(t), offeringPath, "fixedFees",
				`[{ "amount": 65.7, "currency": "EUR", "regions": ["fr-par"] }]`),
			wantErr:      []error{catalog.ErrMissingFee},
			wantNotErr:   []error{snapshot.ErrDecode},
			wantQuoted:   []string{"nl-ams"},
			wantContains: []string{"fixedFees"},
		},
		// planBillingCapHours is the format's second optional key (ADR 0029): the
		// hours a month the offering bills one instance of one of its plans for, an
		// integer from 1 to 744 that its plan prices are the price of. Zero is
		// refused, as an empty fixedFees is, so "uncapped" has one spelling: leave
		// the key out. Every value but an integer is refused with the path it sits
		// at.
		{
			name:         "plan billing cap of zero",
			data:         withNested(t, planBillingCapDocument(t), offeringPath, "planBillingCapHours", `0`),
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"offerings[0].planBillingCapHours", "leave the key out"},
		},
		{
			name:         "null plan billing cap",
			data:         withNested(t, planBillingCapDocument(t), offeringPath, "planBillingCapHours", `null`),
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"offerings[0].planBillingCapHours"},
		},
		{
			name:         "plan billing cap given as a string",
			data:         withNested(t, planBillingCapDocument(t), offeringPath, "planBillingCapHours", `"672"`),
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"offerings[0].planBillingCapHours"},
		},
		{
			name:         "plan billing cap given as a boolean",
			data:         withNested(t, planBillingCapDocument(t), offeringPath, "planBillingCapHours", `true`),
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"offerings[0].planBillingCapHours"},
		},
		{
			name:         "plan billing cap given as an object",
			data:         withNested(t, planBillingCapDocument(t), offeringPath, "planBillingCapHours", `{}`),
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"offerings[0].planBillingCapHours"},
		},
		{
			name:         "plan billing cap given as an array",
			data:         withNested(t, planBillingCapDocument(t), offeringPath, "planBillingCapHours", `[]`),
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"offerings[0].planBillingCapHours"},
		},
		{
			name:         "plan billing cap with a fraction",
			data:         withNested(t, planBillingCapDocument(t), offeringPath, "planBillingCapHours", `672.5`),
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"offerings[0].planBillingCapHours"},
		},
		{
			// 672.0 is an integer written as a decimal, which the format's
			// integer grammar does not allow: a cap is written 672.
			name:         "plan billing cap with a zero fraction",
			data:         withNested(t, planBillingCapDocument(t), offeringPath, "planBillingCapHours", `672.0`),
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"offerings[0].planBillingCapHours"},
		},
		{
			name:         "plan billing cap with an exponent",
			data:         withNested(t, planBillingCapDocument(t), offeringPath, "planBillingCapHours", `6.72e2`),
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"offerings[0].planBillingCapHours"},
		},
		{
			name:         "plan billing cap written as -0",
			data:         withNested(t, planBillingCapDocument(t), offeringPath, "planBillingCapHours", `-0`),
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"offerings[0].planBillingCapHours"},
		},
		{
			name:         "plan billing cap outside int64",
			data:         withNested(t, planBillingCapDocument(t), offeringPath, "planBillingCapHours", `9223372036854775808`),
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"offerings[0].planBillingCapHours"},
		},
		{
			// A negative cap is read: the sign is part of the grammar, and the
			// rule that a cap is at least 1 lives once, in the domain, so Parse
			// returns catalog.New's error as it is.
			name:         "negative plan billing cap reaches the domain",
			data:         withNested(t, planBillingCapDocument(t), offeringPath, "planBillingCapHours", `-1`),
			wantErr:      []error{catalog.ErrInvalidBillingCap},
			wantNotErr:   []error{snapshot.ErrDecode},
			wantQuoted:   []string{"Example Compute"},
			wantContains: []string{"planBillingCapHours"},
		},
		{
			name:         "plan billing cap above the maximum reaches the domain",
			data:         withNested(t, planBillingCapDocument(t), offeringPath, "planBillingCapHours", `745`),
			wantErr:      []error{catalog.ErrInvalidBillingCap},
			wantNotErr:   []error{snapshot.ErrDecode},
			wantQuoted:   []string{"Example Compute"},
			wantContains: []string{"planBillingCapHours"},
		},
		{
			// A negative number is read: the sign is part of the grammar, and the
			// rule that a size or a price is not negative lives once, in the
			// domain, so the reader does not report a different sentinel than
			// catalog.New does.
			name:         "negative vcpus reaches the domain",
			data:         docIn(t, pricedDocument, `"vcpus": 2,`, `"vcpus": -1,`),
			wantErr:      []error{catalog.ErrInvalidPlan},
			wantNotErr:   []error{snapshot.ErrDecode},
			wantQuoted:   []string{"EC-2-4"},
			wantContains: []string{"vcpus"},
		},
		{
			name:         "negative included storage reaches the domain",
			data:         docIn(t, pricedDocument, `"storageGiB": 20,`, `"storageGiB": -1,`),
			wantErr:      []error{catalog.ErrInvalidPlan},
			wantNotErr:   []error{snapshot.ErrDecode},
			wantQuoted:   []string{"EC-2-4"},
			wantContains: []string{"storageGiB"},
		},
		{
			name:         "negative price amount reaches the domain",
			data:         docIn(t, pricedDocument, `"amount": 3.65,`, `"amount": -0.01,`),
			wantErr:      []error{catalog.ErrInvalidPrice},
			wantNotErr:   []error{snapshot.ErrDecode},
			wantQuoted:   []string{"EC-1-0.5"},
			wantContains: []string{"amount"},
		},
		// The PRD's four refusals reach the domain's sentinels through Parse: the
		// reader turns JSON text into domain values and leaves the rules to
		// catalog.New, whose error Parse returns as it is.
		{
			name:       "plan without a price",
			data:       withNested(t, pricedDocument, planPath, "prices", "[]"),
			wantErr:    []error{catalog.ErrUnpricedPlan},
			wantQuoted: []string{"EC-2-4"},
		},
		{
			name:         "price in a currency that is not EUR",
			data:         withNested(t, pricedDocument, pricePath, "currency", `"USD"`),
			wantErr:      []error{catalog.ErrInvalidPrice},
			wantQuoted:   []string{"EC-2-4", "USD"},
			wantContains: []string{"currency"},
		},
		{
			name:       "two plans of one offering with the same name",
			data:       withNested(t, pricedDocument, secondPlanPath, "name", `"EC-2-4"`),
			wantErr:    []error{catalog.ErrDuplicatePlan},
			wantQuoted: []string{"EC-2-4"},
		},
		{
			// A price region is checked against the offering's regions, which the
			// reader has already checked against the declared ones (ADR 0024).
			name:         "price region the offering is not in",
			data:         withNested(t, pricedDocument, pricePath, "regions", `["de-fra"]`),
			wantErr:      []error{catalog.ErrPriceRegion},
			wantNotErr:   []error{snapshot.ErrUndeclaredRegion, snapshot.ErrDecode},
			wantQuoted:   []string{"de-fra"},
			wantContains: []string{"region"},
		},
		{
			// The domain names a field bare, so the empty region is named by the
			// index it sits at, not by a quoted path.
			name:         "blank price region",
			data:         withNested(t, pricedDocument, pricePath, "regions", `[""]`),
			wantErr:      []error{catalog.ErrInvalidPrice},
			wantContains: []string{"regions[0]"},
		},
		{
			name:       "storage price region the offering is not in",
			data:       withNested(t, pricedDocument, storagePricePath, "regions", `["de-fra"]`),
			wantErr:    []error{catalog.ErrPriceRegion},
			wantQuoted: []string{"de-fra"},
		},
		{
			// encoding/json reads an escaped lone surrogate as U+FFFD and reports
			// nothing, so the name would reach a report as a different string
			// from the one the document wrote.
			name:         "escaped lone high surrogate in an offering name",
			data:         doc(t, `"name": "Example Compute"`, `"name": "\ud800"`),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings[0].name"},
			wantContains: []string{"surrogate on its own", "U+D800", "at line ", ", column "},
		},
		{
			name:         "escaped lone low surrogate in the target",
			data:         doc(t, `"target": "Example Cloud"`, `"target": "\udc00"`),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"target"},
			wantContains: []string{"surrogate on its own", "U+DC00"},
		},
		{
			// The scan of the raw text looks only for lone surrogates: a string
			// whose author wrote U+FFFD on purpose is text like any other, and
			// refusing it would refuse a document the format allows.
			name: "offering name holding U+FFFD",
			data: doc(t, `"name": "Example Compute"`, `"name": "Example\ufffdCompute"`),
		},
		{
			// The name is U+FFFD, then an escaped backslash, then the text ud800.
			// The JSON text \\ud800 is a backslash and four letters, no escape of
			// its own: a scan that read the second backslash as the start of an
			// escape would refuse a document the format allows.
			name: "offering name holding U+FFFD before an escaped backslash and ud800",
			data: doc(t, `      "name": "Example Compute",`, `      "name": "\ufffd\\ud800",`),
		},
		{
			// A lone surrogate is placed at the escape that spells it, counted in
			// characters from the start of the line: after the two-byte é, the
			// backslash of \ud800 is line 9's character column 17.
			name:         "escaped lone surrogate after a multi-byte character",
			data:         doc(t, `      "name": "Example Compute",`, `      "name": "é\ud800",`),
			wantErr:      []error{snapshot.ErrDecode},
			wantQuoted:   []string{"offerings[0].name"},
			wantContains: []string{"surrogate on its own", "U+D800"},
			wantPosition: "line 10, column 17",
		},
		{
			// A surrogate pair is one character, not two lone surrogates: the
			// high one is followed at once by the low one.
			name: "offering name holding a surrogate pair",
			data: doc(t, `"name": "Example Compute"`, `"name": "Example \ud83d\ude00"`),
		},
		{
			// Every declared region is a promise too: an entry that is no region
			// name would be declared for nothing. No offering uses the broken
			// entry, so the declaration itself is what has to be checked.
			name:       "declared region that is empty",
			data:       doc(t, `  "regions": ["fr-par"],`, `  "regions": ["fr-par", ""],`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{""},
		},
		{
			name:       "declared region that is only white space",
			data:       doc(t, `  "regions": ["fr-par"],`, `  "regions": ["fr-par", " "],`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{" "},
		},
		{
			name:       "declared region that is listed twice",
			data:       doc(t, `  "regions": ["fr-par"],`, `  "regions": ["fr-par", "fr-par"],`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"fr-par"},
		},
		{
			name:       "declared region with a control character",
			data:       doc(t, `  "regions": ["fr-par"],`, "  \"regions\": [\"fr-par\", \"bad\\u0000x\"],"),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"bad\x00x"},
		},
		{
			// A region name is text that reaches reports and terminals, and a
			// byte that is not UTF-8 has no text to show there. The JSON decoder
			// on its own would turn it into U+FFFD and report nothing, so the
			// document's own bytes have to be checked.
			name:         "declared region with an invalid UTF-8 byte",
			data:         doc(t, `  "regions": ["fr-par"],`, "  \"regions\": [\"fr-par\", \"bad\xffx\"],"),
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"UTF-8"},
		},
		// A region code decides whether an offering is available somewhere and
		// reaches every report as it stands, so it is a code and not free text:
		// ASCII letters and digits in groups joined by single hyphens. A value
		// that only looks like a declared region (a Cyrillic homoglyph, a
		// no-break space, an underscore) would declare a region no reader can
		// tell from the real one, and an upper-case variant is the same region
		// written twice.
		{
			// The 'р' and 'а' are U+0440 and U+0430: a look-alike code that a
			// reader cannot tell from fr-par.
			name:       "declared region with Cyrillic homoglyphs",
			data:       doc(t, `  "regions": ["fr-par"],`, `  "regions": ["fr-par", "fr-`+"\u0440\u0430"+`r"],`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"fr-" + "\u0440\u0430" + "r"},
		},
		{
			// The space is U+00A0: text.Validate accepts it inside a value, and a
			// reader cannot see that it is not an ASCII space, so the code shape
			// has to refuse it.
			name:       "declared region with a no break space",
			data:       doc(t, `  "regions": ["fr-par"],`, "  \"regions\": [\"fr-par\", \"fr\\u00a0ams\"],"),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"fr\u00a0ams"},
		},
		{
			name:       "declared region with an ASCII space",
			data:       doc(t, `  "regions": ["fr-par"],`, `  "regions": ["fr-par", "fr ams"],`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"fr ams"},
		},
		{
			name:       "declared region with an underscore",
			data:       doc(t, `  "regions": ["fr-par"],`, `  "regions": ["fr-par", "fr_ams"],`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"fr_ams"},
		},
		{
			name:       "declared region with a dot",
			data:       doc(t, `  "regions": ["fr-par"],`, `  "regions": ["fr-par", "fr.ams"],`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"fr.ams"},
		},
		{
			name:       "declared region with a leading hyphen",
			data:       doc(t, `  "regions": ["fr-par"],`, `  "regions": ["fr-par", "-ams"],`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"-ams"},
		},
		{
			name:       "declared region with a trailing hyphen",
			data:       doc(t, `  "regions": ["fr-par"],`, `  "regions": ["fr-par", "ams-"],`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"ams-"},
		},
		{
			// Groups are joined by one hyphen: two in a row are a hyphen left
			// behind by a group that was removed.
			name:       "declared region with a double hyphen",
			data:       doc(t, `  "regions": ["fr-par"],`, `  "regions": ["fr-par", "nl--ams"],`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"nl--ams"},
		},
		{
			// Region codes are compared ignoring ASCII case, so this declares
			// fr-par a second time: an offering listing fr-par would otherwise be
			// reported in whichever of the two spellings won.
			name:       "declared region in upper case",
			data:       doc(t, `  "regions": ["fr-par"],`, `  "regions": ["fr-par", "FR-PAR"],`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"FR-PAR"},
		},
		{
			name:       "declared region in mixed case",
			data:       doc(t, `  "regions": ["fr-par"],`, `  "regions": ["fr-par", "Fr-Par"],`),
			wantErr:    []error{snapshot.ErrDecode},
			wantQuoted: []string{"Fr-Par"},
		},
		{
			// An invalid byte is refused where it is, not at the start of the
			// document: the FF of the offering name is line 9's character column
			// 19, which is where the name's fourth character begins.
			name:         "invalid UTF-8 in an offering name",
			data:         doc(t, `      "name": "Example Compute",`, "      \"name\": \"bad\xffname\","),
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"not valid UTF-8"},
			wantPosition: "line 10, column 19",
			wantAbsent:   []string{"line 1, column 1"},
		},
		{
			// A byte order mark is not JSON, and the UTF-8 one is valid UTF-8, so
			// only naming it stops the reader reporting it as malformed JSON at
			// the first byte.
			name:         "UTF-8 byte order mark",
			data:         "\xef\xbb\xbf" + validDocument,
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"byte order mark"},
		},
		{
			// The UTF-16 byte order marks are not UTF-8 either, and are refused by
			// name like the UTF-8 one rather than as a byte that is not UTF-8.
			name:         "UTF-16LE byte order mark",
			data:         "\xff\xfe" + validDocument,
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"byte order mark"},
			wantAbsent:   []string{"not valid UTF-8"},
		},
		{
			name:         "UTF-16BE byte order mark",
			data:         "\xfe\xff" + validDocument,
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"byte order mark"},
			wantAbsent:   []string{"not valid UTF-8"},
		},
		{
			// A multi-byte character where the next key belongs is malformed JSON,
			// and the JSON decoder counts the rune's last byte: the message must
			// place the character at its first byte, which is line 3's column 2.
			name:         "malformed two-byte character where a key is expected",
			data:         "{\n \"schemaVersion\": 2,\n é\n}",
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"malformed JSON"},
			wantPosition: "line 3, column 2",
			wantAbsent:   []string{"column 3"},
		},
		{
			// The same for a character of three bytes: the step back to the first
			// byte of the rune covers every byte of it.
			name:         "malformed three-byte character where a key is expected",
			data:         "{\n \"schemaVersion\": 2,\n €\n}",
			wantErr:      []error{snapshot.ErrDecode},
			wantContains: []string{"malformed JSON"},
			wantPosition: "line 3, column 2",
			wantAbsent:   []string{"column 4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := snapshot.Parse([]byte(tt.data))

			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("Parse() returned unexpected error: %v", err)
				}

				return
			}

			if err == nil {
				t.Fatalf("Parse() returned no error, want one wrapping %v", tt.wantErr)
			}

			for _, sentinel := range tt.wantErr {
				if !errors.Is(err, sentinel) {
					t.Errorf("Parse() error = %v, want it to wrap %v", err, sentinel)
				}
			}

			for _, sentinel := range tt.wantNotErr {
				if errors.Is(err, sentinel) {
					t.Errorf("Parse() error = %v, want it not to wrap %v", err, sentinel)
				}
			}

			msg := err.Error()

			if strings.ContainsAny(msg, "\r\n") {
				t.Errorf("Parse() error = %q, want a single line", msg)
			}

			if errors.Is(err, snapshot.ErrDecode) {
				// The decoder's own text says where it gave up, in its terms, and
				// is not the package's error contract: the message describes the
				// problem in the package's words.
				for _, decoderText := range []string{"json:", "cannot unmarshal", "invalid character"} {
					if strings.Contains(msg, decoderText) {
						t.Errorf("Parse() error = %q, want it without the decoder's %s",
							msg, text.Quote(decoderText))
					}
				}
			}

			for _, value := range tt.wantQuoted {
				if quoted := text.Quote(value); !strings.Contains(msg, quoted) {
					t.Errorf("Parse() error = %q, want it to name %s through text.Quote", msg, quoted)
				}
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(msg, want) {
					t.Errorf("Parse() error = %q, want it to contain %s", msg, text.Quote(want))
				}
			}

			for _, unwanted := range tt.wantAbsent {
				if strings.Contains(msg, unwanted) {
					t.Errorf("Parse() error = %q, want it without %s", msg, text.Quote(unwanted))
				}
			}

			if tt.wantPosition != "" && !strings.Contains(msg, tt.wantPosition) {
				t.Errorf("Parse() error = %q, want it to report the position as %q", msg, tt.wantPosition)
			}
		})
	}
}

// TestParseErrorsNameNoTarget pins that the reader's errors are worded for every
// target: the reader is shared by every target adapter, so a message may not say
// which target's document it was reading. Each document here has the target
// "Example Cloud", and a failing one must name neither of the targets the
// project ships.
func TestParseErrorsNameNoTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
	}{
		{name: "not JSON", data: "not json"},
		{
			name: "duplicate top-level key",
			data: doc(t, `  "target": "Example Cloud",`,
				"  \"target\": \"Example Cloud\",\n  \"target\": \"Example Cloud\","),
		},
		{name: "null target", data: with(t, "target", "null")},
		{
			name: "escaped lone surrogate in the target",
			data: doc(t, `"target": "Example Cloud"`, `"target": "\ud800"`),
		},
		{
			name: "retrieval date that is not a date",
			data: doc(t, `"retrievedOn": "2026-11-10"`, `"retrievedOn": "yesterday"`),
		},
		{
			name: "offering region not in the declared regions",
			data: doc(t, `      "regions": ["fr-par"]`, `      "regions": ["xx-yy"]`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := snapshot.Parse([]byte(tt.data))
			if err == nil {
				t.Fatalf("Parse() of a document with target %s returned no error, want one",
					text.Quote("Example Cloud"))
			}

			for _, target := range []string{"Scaleway", "OVHcloud"} {
				if strings.Contains(err.Error(), target) {
					t.Errorf("Parse() error = %q, want it without the target name %s",
						err.Error(), text.Quote(target))
				}
			}
		})
	}
}

// TestParsePlansAndPrices pins what the plans and prices of a version 2 document
// become in the catalog: the exact decimals converted to whole units (memoryGiB
// to thousandths of a GiB, amount to ten-thousandths of EUR), plans sorted by
// name, every price's regions sorted, de-duplicated and nil when empty, the
// prices sorted by first region with the every-region price first, and the
// document's default region carried into the catalog's snapshot.
func TestParsePlansAndPrices(t *testing.T) {
	t.Parallel()

	document, err := snapshot.Parse([]byte(pricedDocument))
	if err != nil {
		t.Fatalf("Parse() of a document with plans and prices returned unexpected error: %v", err)
	}

	if got, want := document.Catalog.Snapshot().DefaultRegion, "fr-par"; got != want {
		t.Errorf("Parse().Catalog.Snapshot().DefaultRegion = %s, want %s", text.Quote(got), text.Quote(want))
	}

	offerings := document.Catalog.Offerings()
	if len(offerings) != 1 {
		t.Fatalf("Parse().Catalog.Offerings() returned %d offerings, want 1", len(offerings))
	}

	wantPlans := []catalog.Plan{
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
				{Amount: 130125, Currency: catalog.EUR, Regions: []string{"fr-par"}},
				{Amount: 125000, Currency: catalog.EUR, Regions: []string{"nl-ams"}},
			},
		},
	}

	if diff := cmp.Diff(wantPlans, offerings[0].Plans); diff != "" {
		t.Errorf("Plans mismatch (-want +got):\n%s", diff)
	}

	wantStoragePrices := []catalog.Price{{Amount: 400, Currency: catalog.EUR}}
	if diff := cmp.Diff(wantStoragePrices, offerings[0].StoragePrices); diff != "" {
		t.Errorf("StoragePrices mismatch (-want +got):\n%s", diff)
	}

	// The stored amount and the document's decimal are the same price, at the
	// precision the format stores it: 12.5 EUR is 125000 ten-thousandths.
	if got, want := offerings[0].Plans[1].Prices[1].Amount.String(), "12.50"; got != want {
		t.Errorf("parsed amount = %s, want %s", text.Quote(got), text.Quote(want))
	}

	t.Run("an amount with four fractional digits", func(t *testing.T) {
		t.Parallel()

		// The last digit the format allows is stored exactly: 12.3456 EUR is
		// 123456 ten-thousandths, never a rounded or floating-point value.
		document, err := snapshot.Parse([]byte(docIn(t, pricedDocument, `"amount": 12.5,`, `"amount": 12.3456,`)))
		if err != nil {
			t.Fatalf("Parse() returned unexpected error: %v", err)
		}

		plans := document.Catalog.Offerings()[0].Plans
		if len(plans) != 2 || len(plans[1].Prices) != 2 {
			t.Fatalf("the document's plans are not the two plans with their prices")
		}

		if got, want := plans[1].Prices[1].Amount, catalog.Amount(123456); got != want {
			t.Errorf("stored amount = %d, want %d for 12.3456 EUR", got, want)
		}
	})
}

// TestParseFixedFees pins what the optional fixedFees key of a version 2
// document becomes in the catalog (ADR 0026): each amount is an exact monthly
// EUR amount in ten-thousandths (65.7 is Amount(657000), 0 stays a fee of 0), the
// fees are sorted by their first region and their regions sorted and
// de-duplicated, and a document without the key reads as before, with no fee on
// any offering.
func TestParseFixedFees(t *testing.T) {
	t.Parallel()

	document, err := snapshot.Parse([]byte(fixedFeesDocument(t)))
	if err != nil {
		t.Fatalf("Parse() of a document with fixed fees returned unexpected error: %v", err)
	}

	offerings := document.Catalog.Offerings()
	if len(offerings) != 1 {
		t.Fatalf("Parse().Catalog.Offerings() returned %d offerings, want 1", len(offerings))
	}

	// The document's own order is nl-ams then fr-par, and it lists nl-ams twice:
	// the catalog sorts the fees by first region and de-duplicates each fee's
	// regions.
	want := []catalog.Price{
		{Amount: 0, Currency: catalog.EUR, Regions: []string{"fr-par"}},
		{Amount: 657000, Currency: catalog.EUR, Regions: []string{"nl-ams"}},
	}

	if diff := cmp.Diff(want, offerings[0].FixedFees); diff != "" {
		t.Errorf("FixedFees mismatch (-want +got):\n%s", diff)
	}

	// The stored amount and the document's decimal are the same price, at the
	// precision the format stores it: 65.7 EUR a month is 657000 ten-thousandths.
	for _, tc := range []struct {
		region string
		want   catalog.Amount
	}{
		{region: "fr-par", want: 0},
		{region: "nl-ams", want: 657000},
	} {
		got, ok := offerings[0].FixedFee(tc.region)
		if !ok {
			t.Errorf("FixedFee(%s) reports no fee, want %s", text.Quote(tc.region), tc.want)

			continue
		}

		if got != tc.want {
			t.Errorf("FixedFee(%s) = %s, want %s", text.Quote(tc.region), got, tc.want)
		}
	}

	if fee, ok := offerings[0].FixedFee("de-fra"); ok {
		t.Errorf("FixedFee(\"de-fra\") = (%s, true), want (0.00, false): the offering is not available there", fee)
	}

	t.Run("a document without the key charges no fixed fee", func(t *testing.T) {
		t.Parallel()

		// Every snapshot written before ADR 0026 has no fixedFees key, and must
		// read as it did: no fee on any offering, which is not a fee of 0.
		document, err := snapshot.Parse([]byte(pricedDocument))
		if err != nil {
			t.Fatalf("Parse() of a document without fixed fees returned unexpected error: %v", err)
		}

		for _, offering := range document.Catalog.Offerings() {
			if offering.FixedFees != nil {
				t.Errorf("offering %s has FixedFees %v, want nil", text.Quote(offering.Name), offering.FixedFees)
			}

			for _, region := range offering.Regions {
				if fee, ok := offering.FixedFee(region); ok {
					t.Errorf("offering %s reports a fee %s in region %s, want none",
						text.Quote(offering.Name), fee, text.Quote(region))
				}
			}
		}
	})

	t.Run("a fee that applies to every region", func(t *testing.T) {
		t.Parallel()

		data := withNested(t, fixedFeesDocument(t), offeringPath, "fixedFees",
			`[{ "amount": 65.7, "currency": "EUR", "regions": [] }]`)

		document, err := snapshot.Parse([]byte(data))
		if err != nil {
			t.Fatalf("Parse() returned unexpected error: %v", err)
		}

		want := []catalog.Price{{Amount: 657000, Currency: catalog.EUR}}

		if diff := cmp.Diff(want, document.Catalog.Offerings()[0].FixedFees); diff != "" {
			t.Errorf("FixedFees mismatch (-want +got):\n%s", diff)
		}
	})
}

// offeringPath is where pricedDocument's offering sits, as editAt takes it.
var offeringPath = []any{"offerings", 0}

// TestParsePlanBillingCapHours pins what the optional planBillingCapHours key of
// a version 2 document becomes in the catalog (ADR 0029): the hours a month the
// offering bills one instance of one of its plans for, whose monthly prices are
// then the price of that many hours. A document without the key reads as it
// always did, with no cap on any offering, which is not a cap of 0 hours.
func TestParsePlanBillingCapHours(t *testing.T) {
	t.Parallel()

	document, err := snapshot.Parse([]byte(planBillingCapDocument(t)))
	if err != nil {
		t.Fatalf("Parse() of a document with a plan billing cap returned unexpected error: %v", err)
	}

	offerings := document.Catalog.Offerings()
	if len(offerings) != 1 {
		t.Fatalf("Parse().Catalog.Offerings() returned %d offerings, want 1", len(offerings))
	}

	if got, want := offerings[0].PlanBillingCapHours, int64(672); got != want {
		t.Errorf("Offerings()[0].PlanBillingCapHours = %d, want %d", got, want)
	}

	t.Run("a document without the key is uncapped", func(t *testing.T) {
		t.Parallel()

		// Every snapshot written before ADR 0029 has no planBillingCapHours key,
		// and must read as it did: no cap on any offering.
		document, err := snapshot.Parse([]byte(pricedDocument))
		if err != nil {
			t.Fatalf("Parse() of a document without a plan billing cap returned unexpected error: %v", err)
		}

		for _, offering := range document.Catalog.Offerings() {
			if got := offering.PlanBillingCapHours; got != 0 {
				t.Errorf("offering %s has PlanBillingCapHours %d, want 0: a document without the key is uncapped",
					text.Quote(offering.Name), got)
			}
		}
	})

	t.Run("the smallest and the largest cap", func(t *testing.T) {
		t.Parallel()

		for _, hours := range []string{"1", "744"} {
			data := withNested(t, planBillingCapDocument(t), offeringPath, "planBillingCapHours", hours)

			document, err := snapshot.Parse([]byte(data))
			if err != nil {
				t.Fatalf("Parse() of a document with a plan billing cap of %s returned unexpected error: %v", hours, err)
			}

			if got := document.Catalog.Offerings()[0].PlanBillingCapHours; strconv.FormatInt(got, 10) != hours {
				t.Errorf("Offerings()[0].PlanBillingCapHours = %d, want %s", got, hours)
			}
		}
	})
}

// TestParseRequiresEveryVersion2Key pins that every key schema version 2 adds is
// required and spelt exactly: defaultRegion, an offering's plans and
// storagePrices, every plan key and every price key is refused with ErrDecode
// when it is missing, null, of the wrong type, repeated or spelt inexactly. A
// key that a document forgot would otherwise read as a value the document never
// stated (ADR 0007, ADR 0024).
func TestParseRequiresEveryVersion2Key(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// base is the document the case edits.
		base string
		// path is where the object that holds the key sits, or nil when the key
		// is the document's own; holder is that object's path, spelt as an error
		// names it, or "" for the document object.
		path   []any
		holder string
		key    string
		// wrong is a JSON value of a type the key does not have.
		wrong string
		// misspelt and repeated are the old and new text of the edit that spells
		// the key differently or writes it a second time.
		misspelt [2]string
		repeated [2]string
	}{
		{
			name:     "defaultRegion",
			base:     validDocument,
			key:      "defaultRegion",
			wrong:    "5",
			misspelt: [2]string{`"defaultRegion"`, `"DefaultRegion"`},
			repeated: [2]string{
				`"defaultRegion": "fr-par",`,
				`"defaultRegion": "fr-par", "defaultRegion": "fr-par",`,
			},
		},
		{
			name:     "offering plans",
			base:     validDocument,
			path:     offeringPath,
			holder:   "offerings[0]",
			key:      "plans",
			wrong:    `{}`,
			misspelt: [2]string{`"plans": [],`, `"Plans": [],`},
			repeated: [2]string{`"plans": [],`, `"plans": [], "plans": [],`},
		},
		{
			name:     "offering storagePrices",
			base:     validDocument,
			path:     offeringPath,
			holder:   "offerings[0]",
			key:      "storagePrices",
			wrong:    `"none"`,
			misspelt: [2]string{`"storagePrices": []`, `"StoragePrices": []`},
			repeated: [2]string{`"storagePrices": []`, `"storagePrices": [], "storagePrices": []`},
		},
		{
			name:     "plan name",
			base:     pricedDocument,
			path:     planPath,
			holder:   "offerings[0].plans[0]",
			key:      "name",
			wrong:    "5",
			misspelt: [2]string{`"name": "EC-2-4",`, `"Name": "EC-2-4",`},
			repeated: [2]string{`"name": "EC-2-4",`, `"name": "EC-2-4", "name": "EC-2-4",`},
		},
		{
			name:     "plan vcpus",
			base:     pricedDocument,
			path:     planPath,
			holder:   "offerings[0].plans[0]",
			key:      "vcpus",
			wrong:    `"2"`,
			misspelt: [2]string{`"vcpus": 2,`, `"Vcpus": 2,`},
			repeated: [2]string{`"vcpus": 2,`, `"vcpus": 2, "vcpus": 2,`},
		},
		{
			name:     "plan memoryGiB",
			base:     pricedDocument,
			path:     planPath,
			holder:   "offerings[0].plans[0]",
			key:      "memoryGiB",
			wrong:    `[]`,
			misspelt: [2]string{`"memoryGiB": 4,`, `"MemoryGiB": 4,`},
			repeated: [2]string{`"memoryGiB": 4,`, `"memoryGiB": 4, "memoryGiB": 4,`},
		},
		{
			name:     "plan storageGiB",
			base:     pricedDocument,
			path:     planPath,
			holder:   "offerings[0].plans[0]",
			key:      "storageGiB",
			wrong:    `true`,
			misspelt: [2]string{`"storageGiB": 20,`, `"StorageGiB": 20,`},
			repeated: [2]string{`"storageGiB": 20,`, `"storageGiB": 20, "storageGiB": 20,`},
		},
		{
			name:     "plan prices",
			base:     pricedDocument,
			path:     planPath,
			holder:   "offerings[0].plans[0]",
			key:      "prices",
			wrong:    `{}`,
			misspelt: [2]string{`"prices": [`, `"Prices": [`},
			repeated: [2]string{`"prices": [`, `"prices": [], "prices": [`},
		},
		{
			name:     "price amount",
			base:     pricedDocument,
			path:     pricePath,
			holder:   "offerings[0].plans[0].prices[0]",
			key:      "amount",
			wrong:    `"12.50"`,
			misspelt: [2]string{`"amount": 12.5,`, `"Amount": 12.5,`},
			repeated: [2]string{`"amount": 12.5,`, `"amount": 12.5, "amount": 12.5,`},
		},
		{
			name:     "price currency",
			base:     pricedDocument,
			path:     pricePath,
			holder:   "offerings[0].plans[0].prices[0]",
			key:      "currency",
			wrong:    "5",
			misspelt: [2]string{`"currency": "EUR",`, `"Currency": "EUR",`},
			repeated: [2]string{`"currency": "EUR",`, `"currency": "EUR", "currency": "EUR",`},
		},
		{
			name:     "price regions",
			base:     pricedDocument,
			path:     pricePath,
			holder:   "offerings[0].plans[0].prices[0]",
			key:      "regions",
			wrong:    `"nl-ams"`,
			misspelt: [2]string{`"currency": "EUR", "regions": ["nl-ams"]`, `"currency": "EUR", "Regions": ["nl-ams"]`},
			repeated: [2]string{
				`"currency": "EUR", "regions": ["nl-ams"]`,
				`"currency": "EUR", "regions": ["nl-ams"], "regions": ["nl-ams"]`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			full := tc.key
			if tc.holder != "" {
				full = tc.holder + "." + tc.key
			}

			// set gives the key a raw value; remove leaves it out of the document.
			set := func(t *testing.T, raw string) string {
				t.Helper()

				if tc.path == nil {
					return with(t, tc.key, raw)
				}

				return withNested(t, tc.base, tc.path, tc.key, raw)
			}

			remove := func(t *testing.T) string {
				t.Helper()

				if tc.path == nil {
					return without(t, tc.key)
				}

				return withoutNested(t, tc.base, tc.path, tc.key)
			}

			cases := []struct {
				name   string
				data   string
				quoted []string
				// want contains the substrings the message must hold besides the
				// key's own name, which every case requires.
				want []string
			}{
				{
					name:   "missing",
					data:   remove(t),
					quoted: []string{tc.key},
					want:   []string{"required key", "is missing"},
				},
				{
					name:   "null",
					data:   set(t, "null"),
					quoted: []string{full},
					want:   []string{"null"},
				},
				{
					name:   "wrong type",
					data:   set(t, tc.wrong),
					quoted: []string{full},
				},
				{
					name:   "repeated",
					data:   docIn(t, tc.base, tc.repeated[0], tc.repeated[1]),
					quoted: []string{tc.key},
					want:   []string{"repeated key"},
				},
				{
					name:   "spelt inexactly",
					data:   docIn(t, tc.base, tc.misspelt[0], tc.misspelt[1]),
					quoted: []string{tc.key},
					want:   []string{"unknown key", "keys are exact"},
				},
			}

			for _, tt := range cases {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					if !json.Valid([]byte(tt.data)) {
						t.Fatalf("the case's document is not valid JSON: %s", tt.data)
					}

					_, err := snapshot.Parse([]byte(tt.data))
					if !errors.Is(err, snapshot.ErrDecode) {
						t.Fatalf("Parse() error = %v, want one wrapping %v", err, snapshot.ErrDecode)
					}

					msg := err.Error()

					if strings.ContainsAny(msg, "\r\n") {
						t.Errorf("Parse() error = %q, want a single line", msg)
					}

					for _, value := range tt.quoted {
						if quoted := text.Quote(value); !strings.Contains(msg, quoted) {
							t.Errorf("Parse() error = %q, want it to name %s through text.Quote", msg, quoted)
						}
					}

					for _, want := range tt.want {
						if !strings.Contains(msg, want) {
							t.Errorf("Parse() error = %q, want it to contain %s", msg, text.Quote(want))
						}
					}
				})
			}
		})
	}
}

// TestParseRegions pins the declared region list Parse returns: the order the
// document lists the regions in, not a sorted one (callers report it as the
// snapshot declares it), and the caller's own copy, so changing one result
// cannot change what a later Parse returns.
func TestParseRegions(t *testing.T) {
	t.Parallel()

	// The offering lists the same regions in the same order, which the document
	// requires to be a subset of the declared ones.
	data := doc(t,
		`  "regions": ["fr-par"],`, `  "regions": ["pl-waw", "fr-par", "nl-ams"],`,
		`      "regions": ["fr-par"]`, `      "regions": ["pl-waw", "fr-par", "nl-ams"]`)

	want := []string{"pl-waw", "fr-par", "nl-ams"}

	document, err := snapshot.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse() returned unexpected error: %v", err)
	}

	if !slices.Equal(document.Regions, want) {
		t.Fatalf("Parse().Regions = %v, want the document's order %v", document.Regions, want)
	}

	for i := range document.Regions {
		document.Regions[i] = "tampered"
	}

	again, err := snapshot.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse() after changing the first result returned unexpected error: %v", err)
	}

	if !slices.Equal(again.Regions, want) {
		t.Errorf("Parse().Regions after changing the first result = %v, want %v: the result is not a copy",
			again.Regions, want)
	}
}

// TestParseRegionsAcceptsEveryRegionShape pins the shapes a declared region code
// may have: ASCII letters and digits in groups joined by single hyphens, in any
// case, because a real target declares FR-PAR and also GRA and RBX. The document
// is accepted and its regions come back in the order it declares them.
func TestParseRegionsAcceptsEveryRegionShape(t *testing.T) {
	t.Parallel()

	// The offering uses the upper-case region, so the declaration and the
	// offering are matched as the document spells them.
	data := doc(t,
		`  "regions": ["fr-par"],`, `  "regions": ["fr-par", "GRA", "europe-west1", "eu-central-1"],`,
		`      "regions": ["fr-par"]`, `      "regions": ["GRA"]`)

	want := []string{"fr-par", "GRA", "europe-west1", "eu-central-1"}

	document, err := snapshot.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse() of a document declaring the region shapes %v returned unexpected error: %v", want, err)
	}

	if !slices.Equal(document.Regions, want) {
		t.Errorf("Parse().Regions = %v, want the document's order %v", document.Regions, want)
	}
}

// TestParseRejectsDocumentsOverTheSizeCap pins the size limit on a catalog
// document: a file over snapshot.MaxDocumentBytes is refused before it is
// decoded, and a file of exactly the limit is not, so the boundary is not off by
// one.
func TestParseRejectsDocumentsOverTheSizeCap(t *testing.T) {
	t.Parallel()

	const wantCap = 1 << 20
	if snapshot.MaxDocumentBytes != wantCap {
		t.Fatalf("MaxDocumentBytes = %d, want %d", snapshot.MaxDocumentBytes, wantCap)
	}

	atCap := padTo(t, validDocument, snapshot.MaxDocumentBytes)
	if _, err := snapshot.Parse(atCap); err != nil {
		t.Fatalf("Parse() of a document of exactly %d bytes returned unexpected error: %v",
			snapshot.MaxDocumentBytes, err)
	}

	overCap := padTo(t, validDocument, snapshot.MaxDocumentBytes+1)

	_, err := snapshot.Parse(overCap)
	if !errors.Is(err, snapshot.ErrDecode) {
		t.Fatalf("Parse() of a document of %d bytes returned error %v, want one wrapping %v",
			len(overCap), err, snapshot.ErrDecode)
	}

	if strings.ContainsAny(err.Error(), "\r\n") {
		t.Errorf("Parse() error = %q, want a single line", err.Error())
	}
}

// emptyCatalogDocument is a complete catalog that declares one region, which is
// also its default region, and no offering. Both lists are present and empty,
// which is a catalog of nothing; null (see TestParseErrors) is a document that
// does not say. It declares a region because a snapshot needs one to estimate
// in (ADR 0024): a document declaring none cannot be read.
const emptyCatalogDocument = `{
  "schemaVersion": 2,
  "target": "Example Cloud",
  "sourceURL": "https://www.example.com/docs/",
  "retrievedOn": "2026-11-10",
  "regions": ["fr-par"],
  "defaultRegion": "fr-par",
  "offerings": []
}`

// TestParseEmptyCatalog pins that a document declaring a region, its default
// region and no offering is an empty catalog, not a broken one: an empty list is
// an answer, which is why only null is refused. It is the other side of the two
// null rows above.
func TestParseEmptyCatalog(t *testing.T) {
	t.Parallel()

	document, err := snapshot.Parse([]byte(emptyCatalogDocument))
	if err != nil {
		t.Fatalf("Parse() of a document declaring a region and no offering returned unexpected error: %v", err)
	}

	if got := document.Catalog.Target(); got != "Example Cloud" {
		t.Errorf("Parse().Catalog.Target() = %s, want %s", text.Quote(got), text.Quote("Example Cloud"))
	}

	if got := document.Catalog.Offerings(); len(got) != 0 {
		t.Errorf("Parse().Catalog.Offerings() = %v, want none", got)
	}

	if got := document.Catalog.Snapshot().DefaultRegion; got != "fr-par" {
		t.Errorf("Parse().Catalog.Snapshot().DefaultRegion = %s, want %s", text.Quote(got), text.Quote("fr-par"))
	}

	if want := []string{"fr-par"}; !slices.Equal(document.Regions, want) {
		t.Errorf("Parse().Regions = %v, want %v", document.Regions, want)
	}
}

// repeatedRegionDocument builds a catalog document that declares many short,
// distinct regions and mentions the last of them as often as the size cap allows.
//
// Mentioning the last declared region is what makes the document a worst case: a
// lookup that finds a region by scanning the declared list stops at the first
// match, so a mention of an early region hides the scan. Only the region furthest
// down the list, mentioned a hundred thousand times, makes the scan cost one
// comparison per declared region per mention.
func repeatedRegionDocument(tb testing.TB) []byte {
	tb.Helper()

	// Four hex digits name every region in five bytes quoted, so the declared
	// list stays a fraction of MaxDocumentBytes and leaves the rest for mentions.
	const declaredCount = 60_000

	regions := make([]string, declaredCount)
	for i := range regions {
		regions[i] = strconv.Quote(fmt.Sprintf("%04x", i))
	}

	last := regions[declaredCount-1]

	// compose renders the document around the offering's region mentions, so the
	// size of every other byte can be measured with the mentions empty.
	compose := func(mentions string) string {
		return fmt.Sprintf(`{
  "schemaVersion": 2,
  "target": "Example Cloud",
  "sourceURL": "https://www.example.com/docs/",
  "retrievedOn": "2026-11-10",
  "regions": [%s],
  "defaultRegion": %s,
  "offerings": [
    {
      "name": "Example Compute",
      "categories": ["compute.vm"],
      "capabilities": ["ipv6"],
      "regions": [%s],
      "plans": [],
      "storagePrices": []
    }
  ]
}`, strings.Join(regions, ", "), regions[0], mentions)
	}

	// Every mention but the last is followed by a comma, so a mention costs the
	// region's quoted length plus the comma.
	perMention := len(last) + len(",")

	mentions := (snapshot.MaxDocumentBytes - len(compose(""))) / perMention
	if mentions < 2 {
		tb.Fatalf("the declared regions leave room for %d mentions, want at least 2, under MaxDocumentBytes = %d",
			mentions, snapshot.MaxDocumentBytes)
	}

	data := compose(strings.TrimSuffix(strings.Repeat(last+",", mentions), ","))

	if len(data) > snapshot.MaxDocumentBytes {
		tb.Fatalf("the document is %d bytes, over MaxDocumentBytes = %d: shorten the region names",
			len(data), snapshot.MaxDocumentBytes)
	}

	return []byte(data)
}

// TestParseRepeatedRegionMention parses the worst-case document once, so the
// default test run exercises the builder and the size guard it works to. It
// asserts no duration: the cost is what BenchmarkParseRepeatedRegionMention
// reports, not something a wall-clock budget can pin without flaking.
func TestParseRepeatedRegionMention(t *testing.T) {
	t.Parallel()

	data := repeatedRegionDocument(t)

	if _, err := snapshot.Parse(data); err != nil {
		t.Fatalf("Parse() of the %d-byte document that mentions its last declared region as often as the size cap allows returned unexpected error: %v",
			len(data), err)
	}
}

// BenchmarkParseRepeatedRegionMention measures parsing a document that declares
// 60 000 regions and mentions the last of them as often as MaxDocumentBytes
// allows. It is a benchmark, not a test with a wall-clock budget: a single parse
// is far too quick to assert on without flaking on a loaded machine even at a
// generous margin, whereas the ns/op it reports exposes the regression it guards -
// a lookup that scans the declared regions once per mention instead of validating
// them once. Mentioning the last of many regions is what makes the scan visible:
// a mention of the first region is found on the first comparison.
func BenchmarkParseRepeatedRegionMention(b *testing.B) {
	data := repeatedRegionDocument(b)

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))

	for b.Loop() {
		if _, err := snapshot.Parse(data); err != nil {
			b.Fatalf("Parse() of a %d-byte document mentioning its last declared region as often as the size cap allows returned unexpected error: %v",
				len(data), err)
		}
	}
}
