// Package testutil_test pins the JSON Schema validator the tests that hold a
// document to a schema share: it accepts what the report schemas and the
// OpenAPI document allow, names what a document gets wrong, and refuses a
// schema keyword it cannot check.
package testutil_test

import (
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/testutil"
)

// validatorSchema uses every keyword the validator implements.
const validatorSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://example.com/validator.schema.json",
  "title": "validator self-test",
  "description": "one of every supported keyword",
  "type": "object",
  "additionalProperties": false,
  "required": ["name", "kind", "items"],
  "properties": {
    "name": {"type": "string", "pattern": "^[a-z]+$"},
    "label": {"type": "string", "minLength": 3},
    "kind": {"enum": ["assessment", "comparison"]},
    "version": {"const": 1},
    "count": {"type": "integer", "minimum": 0},
    "ratio": {"type": "number"},
    "items": {"type": "array", "minItems": 1, "items": {"$ref": "#/$defs/item"}},
    "nothing": {"type": "null"}
  },
  "$defs": {
    "item": {
      "type": "object",
      "additionalProperties": false,
      "required": ["id"],
      "properties": {"id": {"type": "string"}}
    }
  }
}`

// TestSchemaAcceptsAValidDocument pins that a document the schema allows has no
// mismatches: every keyword the validator implements is checked, and a value
// the schema describes passes them all.
func TestSchemaAcceptsAValidDocument(t *testing.T) {
	t.Parallel()

	_, schema, err := testutil.ParseSchema([]byte(validatorSchema))
	if err != nil {
		t.Fatalf("ParseSchema() returned unexpected error: %v", err)
	}

	document := `{"name":"report","label":"abc","kind":"assessment","version":1,"count":0,"ratio":1.5,` +
		`"items":[{"id":"a"}],"nothing":null}`

	mismatches, err := schema.Mismatches([]byte(document))
	if err != nil {
		t.Fatalf("Mismatches() returned unexpected error: %v", err)
	}

	if len(mismatches) != 0 {
		t.Errorf("Mismatches() = %v, want none for a document the schema allows", mismatches)
	}
}

// TestSchemaRefusesInvalidDocuments pins that every keyword reports the document
// it refuses, so a schema check cannot pass a body it should reject.
func TestSchemaRefusesInvalidDocuments(t *testing.T) {
	t.Parallel()

	_, schema, err := testutil.ParseSchema([]byte(validatorSchema))
	if err != nil {
		t.Fatalf("ParseSchema() returned unexpected error: %v", err)
	}

	tests := []struct {
		name     string
		document string
		want     []string
	}{
		{
			name:     "a missing required property",
			document: `{"kind":"assessment","items":[{"id":"a"}]}`,
			want:     []string{"missing required property \"name\""},
		},
		{
			name:     "an unexpected property",
			document: `{"name":"report","kind":"assessment","items":[{"id":"a"}],"extra":true}`,
			want:     []string{"unexpected property \"extra\""},
		},
		{
			name:     "a value outside the enum",
			document: `{"name":"report","kind":"other","items":[{"id":"a"}]}`,
			want:     []string{"is not one of"},
		},
		{
			name:     "another constant",
			document: `{"name":"report","kind":"assessment","version":2,"items":[{"id":"a"}]}`,
			want:     []string{"want the constant 1, got 2"},
		},
		{
			name:     "a string for an integer",
			document: `{"name":"report","kind":"assessment","count":"many","items":[{"id":"a"}]}`,
			want:     []string{"$.count: want integer, got string"},
		},
		{
			name:     "a number below the minimum",
			document: `{"name":"report","kind":"assessment","count":-1,"items":[{"id":"a"}]}`,
			want:     []string{"is less than the minimum 0"},
		},
		{
			name:     "a string that misses the pattern",
			document: `{"name":"Report","kind":"assessment","items":[{"id":"a"}]}`,
			want:     []string{"does not match the pattern"},
		},
		{
			name:     "an array with too few items",
			document: `{"name":"report","kind":"assessment","items":[]}`,
			want:     []string{"want at least 1 items, got 0"},
		},
		{
			// minLength counts characters, not bytes: two runes of two bytes each
			// are two characters, and still fewer than three.
			name:     "a string shorter than the minimum length",
			document: `{"name":"report","label":"éé","kind":"assessment","items":[{"id":"a"}]}`,
			want:     []string{"want at least 3 characters, got 2"},
		},
		{
			name:     "an empty string where a minimum length is declared",
			document: `{"name":"report","label":"","kind":"assessment","items":[{"id":"a"}]}`,
			want:     []string{"want at least 3 characters, got 0"},
		},
		{
			name:     "a referred schema the element breaks",
			document: `{"name":"report","kind":"assessment","items":[{"id":2}]}`,
			want:     []string{"$.items[0].id: want string, got integer"},
		},
		{
			name:     "null where a string belongs",
			document: `{"name":null,"kind":"assessment","items":[{"id":"a"}]}`,
			want:     []string{"$.name: want string, got null"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mismatches, err := schema.Mismatches([]byte(tc.document))
			if err != nil {
				t.Fatalf("Mismatches() returned unexpected error for %s: %v", tc.document, err)
			}

			if len(mismatches) == 0 {
				t.Fatalf("Mismatches(%s) = none, want a mismatch naming %q", tc.document, tc.want)
			}

			joined := strings.Join(mismatches, "\n")
			for _, want := range tc.want {
				if !strings.Contains(joined, want) {
					t.Errorf("Mismatches(%s) = %q, want it to name %q", tc.document, joined, want)
				}
			}
		})
	}
}

// TestSchemaAcceptsWholeNumbersAsNumbers pins that a "number" property takes a
// whole number, which encoding/json decodes as a float that is an integer.
func TestSchemaAcceptsWholeNumbersAsNumbers(t *testing.T) {
	t.Parallel()

	_, schema, err := testutil.ParseSchema([]byte(validatorSchema))
	if err != nil {
		t.Fatalf("ParseSchema() returned unexpected error: %v", err)
	}

	document := `{"name":"report","kind":"assessment","ratio":2,"items":[{"id":"a"}]}`

	mismatches, err := schema.Mismatches([]byte(document))
	if err != nil {
		t.Fatalf("Mismatches() returned unexpected error: %v", err)
	}

	if len(mismatches) != 0 {
		t.Errorf("Mismatches() = %v, want none for a whole number in a number property", mismatches)
	}
}

// TestSchemaRefusesADocumentThatIsNotJSON pins that an unreadable document is an
// error, not a match: a checker must never pass a body it could not decode.
func TestSchemaRefusesADocumentThatIsNotJSON(t *testing.T) {
	t.Parallel()

	_, schema, err := testutil.ParseSchema([]byte(validatorSchema))
	if err != nil {
		t.Fatalf("ParseSchema() returned unexpected error: %v", err)
	}

	if _, err := schema.Mismatches([]byte(`{"name":`)); err == nil {
		t.Fatal("Mismatches() returned no error for a document that is not JSON")
	}

	if _, err := schema.Mismatches(nil); err == nil {
		t.Fatal("Mismatches() returned no error for an empty document")
	}
}

// TestParseSchemaRefusesWhatItCannotCheck pins the validator's two closed doors:
// a keyword it does not implement and a reference it cannot resolve are errors,
// so a schema construct would never pass unvalidated.
func TestParseSchemaRefusesWhatItCannotCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema string
		want   string
	}{
		{
			name:   "an unsupported keyword",
			schema: `{"type":"object","oneOf":[{"type":"string"}]}`,
			want:   `unsupported keyword "oneOf"`,
		},
		{
			name:   "a reference to a missing definition",
			schema: `{"$ref":"#/$defs/missing"}`,
			want:   `"#/$defs/missing" names no schema`,
		},
		{
			// The validator applies a reference instead of the keywords beside it,
			// so a schema that holds both would leave its own keywords unchecked:
			// it refuses the object rather than pass a document the sibling
			// keyword should have rejected.
			name:   "a reference beside a validation keyword",
			schema: `{"$ref":"#/$defs/id","type":"object","$defs":{"id":{"type":"string"}}}`,
			want:   "is unsupported",
		},
		{
			name: "a reference beside required",
			schema: `{"type":"object","properties":{"item":{"$ref":"#/$defs/id","required":["id"]}},` +
				`"$defs":{"id":{"type":"object"}}}`,
			want: `$ref beside "required" is unsupported`,
		},
		{
			name:   "a reference outside the document",
			schema: `{"$ref":"https://example.com/other.schema.json"}`,
			want:   "is not a local JSON pointer",
		},
		{
			name:   "a required name no property describes",
			schema: `{"type":"object","required":["id"],"properties":{}}`,
			want:   `required names "id", which properties does not describe`,
		},
		{
			name:   "additionalProperties that is no boolean",
			schema: `{"type":"object","additionalProperties":{"type":"string"}}`,
			want:   "additionalProperties must be a boolean",
		},
		{
			name:   "an unknown type name",
			schema: `{"type":"money"}`,
			want:   `unknown type "money"`,
		},
		{
			name:   "a pattern that is no regular expression",
			schema: `{"type":"string","pattern":"["}`,
			want:   "is not a regular expression",
		},
		{
			name:   "a schema that is no object",
			schema: `[]`,
			want:   "must be a JSON object",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := testutil.ParseSchema([]byte(tc.schema))
			if err == nil {
				t.Fatalf("ParseSchema(%s) returned no error, want one naming %q", tc.schema, tc.want)
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ParseSchema(%s) = %q, want an error naming %q", tc.schema, err, tc.want)
			}
		})
	}
}

// TestParseOpenAPISchemasResolvesComponentReferences pins the OpenAPI form: a
// component schema refers to another as "#/components/schemas/<name>", and
// Require names a component or fails the test.
func TestParseOpenAPISchemasResolvesComponentReferences(t *testing.T) {
	t.Parallel()

	document := `{"components":{"schemas":{
		"List":{"type":"object","additionalProperties":false,"required":["entries"],
			"properties":{"entries":{"type":"array","items":{"$ref":"#/components/schemas/Entry"}}}},
		"Entry":{"type":"object","additionalProperties":false,"required":["id"],
			"properties":{"id":{"type":"string"}}}
	}}}`

	doc, err := testutil.ParseOpenAPISchemas([]byte(document))
	if err != nil {
		t.Fatalf("ParseOpenAPISchemas() returned unexpected error: %v", err)
	}

	list := doc.Require(t, "#/components/schemas/List")
	entry := doc.Require(t, "#/components/schemas/Entry")

	for _, tc := range []struct {
		name     string
		schema   *testutil.Schema
		document string
		want     string
	}{
		{name: "the component", schema: list, document: `{"entries":[{"id":"a"}]}`},
		{
			name:     "the referred component",
			schema:   list,
			document: `{"entries":[{"id":2}]}`,
			want:     "$.entries[0].id: want string, got integer",
		},
		{name: "an entry", schema: entry, document: `{"id":"a"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mismatches, err := tc.schema.Mismatches([]byte(tc.document))
			if err != nil {
				t.Fatalf("Mismatches() returned unexpected error: %v", err)
			}

			joined := strings.Join(mismatches, "\n")
			if tc.want == "" && len(mismatches) != 0 {
				t.Errorf("Mismatches(%s) = %q, want none", tc.document, joined)
			}

			if tc.want != "" && !strings.Contains(joined, tc.want) {
				t.Errorf("Mismatches(%s) = %q, want it to name %q", tc.document, joined, tc.want)
			}
		})
	}
}

// TestParseOpenAPISchemasRefusesADocumentWithoutSchemas pins that a document
// whose components declare no schema is an error instead of a document that
// validates anything.
func TestParseOpenAPISchemasRefusesADocumentWithoutSchemas(t *testing.T) {
	t.Parallel()

	for _, document := range []string{`{}`, `{"components":{}}`, `{"components":{"schemas":{}}}`, `not json`} {
		if _, err := testutil.ParseOpenAPISchemas([]byte(document)); err == nil {
			t.Errorf("ParseOpenAPISchemas(%s) returned no error, want one", document)
		}
	}
}
