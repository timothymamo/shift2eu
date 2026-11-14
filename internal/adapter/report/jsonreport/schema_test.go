package jsonreport_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// draft202012 is the JSON Schema dialect the report's schemas declare.
const draft202012 = "https://json-schema.org/draft/2020-12/schema"

// supportedKeywords are the keywords the validator checks. A schema that uses any
// other keyword fails the test that parses it, so a construct the validator cannot
// check never passes unvalidated.
var supportedKeywords = []string{
	"$schema", "$id", "title", "description", "$defs", "$ref", "type", "properties", "required",
	"additionalProperties", "items", "enum", "const", "minimum", "pattern",
}

// jsonTypes are the JSON type names a schema's "type" may hold.
var jsonTypes = []string{"object", "array", "string", "integer", "number", "boolean", "null"}

// localRef matches the only kind of reference the report's schemas may use: a
// reference to a definition of the same document. The schemas share no
// definitions with another file, so a reference to one cannot be checked here.
var localRef = regexp.MustCompile(`^#/\$defs/[A-Za-z0-9._-]+$`)

// schema is one decoded schema object, holding only the keywords the validator
// checks.
type schema struct {
	// types are the accepted JSON types, empty for any type.
	types []string
	// properties describes an object's members and required names them.
	properties map[string]*schema
	required   []string
	// additional is the "additionalProperties" keyword, which may only be false.
	additional *bool
	// items is the schema of an array's elements.
	items *schema
	// enum and const restrict the value itself.
	enum       []any
	hasEnum    bool
	constValue any
	hasConst   bool
	// minimum and pattern restrict a number and a string.
	minimum *float64
	pattern *regexp.Regexp
	// defs is the document's "$defs" object.
	defs map[string]*schema
	// ref is a local reference to a definition, "#/$defs/<name>", or empty.
	ref string
	// root is the document the object belongs to, which holds defs.
	root *schema
}

// parseSchema decodes one JSON Schema document. It fails on a keyword the
// validator does not implement, on an "additionalProperties" other than false, on
// an unknown type name and on a reference that is not a local definition.
func parseSchema(data []byte) (*schema, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode the schema: %w", err)
	}

	if raw == nil {
		return nil, fmt.Errorf("the schema is not an object")
	}

	root := &schema{}
	if err := root.parseKeywords(raw); err != nil {
		return nil, err
	}

	for _, sub := range root.subschemas() {
		sub.root = root
	}

	for _, ref := range root.refs() {
		if _, ok := root.defs[definitionName(ref)]; !ok {
			return nil, fmt.Errorf("reference %q names no definition in $defs", ref)
		}
	}

	return root, nil
}

// parseKeywords parses every keyword of one schema object. A required name the
// object does not describe is an error: the validator would otherwise check for
// a key it cannot validate, so a schema that names one is not one it can fully
// check.
func (s *schema) parseKeywords(raw map[string]json.RawMessage) error {
	for _, keyword := range slices.Sorted(maps.Keys(raw)) {
		if err := s.parseKeyword(keyword, raw[keyword]); err != nil {
			return err
		}
	}

	for _, name := range s.required {
		if _, ok := s.properties[name]; !ok {
			return fmt.Errorf("required names %s, which properties does not describe: "+
				"the schema must describe every key it requires", strconv.Quote(name))
		}
	}

	return nil
}

// parseKeyword parses one keyword of a schema object.
func (s *schema) parseKeyword(keyword string, value json.RawMessage) error {
	switch keyword {
	case "$schema", "$id", "title", "description":
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return fmt.Errorf("%s must be a string: %w", keyword, err)
		}

		return nil
	case "$defs":
		return decodeObject(keyword, value, func(members map[string]json.RawMessage) error {
			s.defs = make(map[string]*schema, len(members))

			for _, name := range slices.Sorted(maps.Keys(members)) {
				definition := &schema{}
				if err := decodeObject(name, members[name], definition.parseKeywords); err != nil {
					return err
				}

				s.defs[name] = definition
			}

			return nil
		})
	case "$ref":
		var ref string
		if err := json.Unmarshal(value, &ref); err != nil {
			return fmt.Errorf("$ref must be a string: %w", err)
		}

		if !localRef.MatchString(ref) {
			return fmt.Errorf("$ref %q is not a local definition reference \"#/$defs/<name>\"", ref)
		}

		s.ref = ref

		return nil
	case "type":
		var single string

		if err := json.Unmarshal(value, &single); err == nil {
			s.types = []string{single}
		} else if err := json.Unmarshal(value, &s.types); err != nil {
			return fmt.Errorf("type must be a type name or a list of them: %w", err)
		}

		for _, name := range s.types {
			if !slices.Contains(jsonTypes, name) {
				return fmt.Errorf("unknown type %q: the validator supports %s",
					name, strings.Join(jsonTypes, ", "))
			}
		}

		return nil
	case "properties":
		return decodeObject(keyword, value, func(members map[string]json.RawMessage) error {
			s.properties = make(map[string]*schema, len(members))

			for _, name := range slices.Sorted(maps.Keys(members)) {
				property := &schema{}
				if err := decodeObject(name, members[name], property.parseKeywords); err != nil {
					return err
				}

				s.properties[name] = property
			}

			return nil
		})
	case "required":
		if err := json.Unmarshal(value, &s.required); err != nil {
			return fmt.Errorf("required must be a list of property names: %w", err)
		}

		if s.required == nil {
			return fmt.Errorf("required must be a list of property names, not null")
		}

		return nil
	case "additionalProperties":
		var allowed bool
		if err := json.Unmarshal(value, &allowed); err != nil {
			return fmt.Errorf("additionalProperties must be false: %w", err)
		}

		if allowed {
			return fmt.Errorf("additionalProperties must be false; the report's schemas allow no extra key")
		}

		s.additional = &allowed

		return nil
	case "items":
		items := &schema{}
		if err := decodeObject(keyword, value, items.parseKeywords); err != nil {
			return err
		}

		s.items = items

		return nil
	case "enum":
		if err := json.Unmarshal(value, &s.enum); err != nil {
			return fmt.Errorf("enum must be a list: %w", err)
		}

		if s.enum == nil {
			return fmt.Errorf("enum must be a list, not null")
		}

		s.hasEnum = true

		return nil
	case "const":
		if err := json.Unmarshal(value, &s.constValue); err != nil {
			return fmt.Errorf("const: %w", err)
		}

		s.hasConst = true

		return nil
	case "minimum":
		var minimum float64
		if err := json.Unmarshal(value, &minimum); err != nil {
			return fmt.Errorf("minimum must be a number: %w", err)
		}

		s.minimum = &minimum

		return nil
	case "pattern":
		var pattern string
		if err := json.Unmarshal(value, &pattern); err != nil {
			return fmt.Errorf("pattern must be a regular expression: %w", err)
		}

		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("pattern %q: %w", pattern, err)
		}

		s.pattern = compiled

		return nil
	default:
		return fmt.Errorf("unsupported keyword %q: the validator checks only %s",
			keyword, strings.Join(supportedKeywords, ", "))
	}
}

// decodeObject decodes value as a JSON object and parses it with parse.
func decodeObject(what string, value json.RawMessage, parse func(map[string]json.RawMessage) error) error {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(value, &members); err != nil {
		return fmt.Errorf("%s must be an object: %w", what, err)
	}

	if members == nil {
		return fmt.Errorf("%s must be an object, not null", what)
	}

	return parse(members)
}

// subschemas returns every schema object below s, s included, in a fixed order.
func (s *schema) subschemas() []*schema {
	out := []*schema{s}

	for _, name := range slices.Sorted(maps.Keys(s.properties)) {
		out = append(out, s.properties[name].subschemas()...)
	}

	if s.items != nil {
		out = append(out, s.items.subschemas()...)
	}

	for _, name := range slices.Sorted(maps.Keys(s.defs)) {
		out = append(out, s.defs[name].subschemas()...)
	}

	return out
}

// refs returns every reference the document holds.
func (s *schema) refs() []string {
	var refs []string

	for _, sub := range s.subschemas() {
		if sub.ref != "" {
			refs = append(refs, sub.ref)
		}
	}

	return refs
}

// definitionName returns the definition a local reference names.
func definitionName(ref string) string {
	return strings.TrimPrefix(ref, "#/$defs/")
}

// validate returns every way value fails s, each as "path: problem". An empty
// result means value matches the schema.
func (s *schema) validate(value any, path string) []string {
	if s.ref != "" {
		target, ok := s.root.defs[definitionName(s.ref)]
		if !ok {
			return []string{path + ": reference " + s.ref + " names no definition"}
		}

		return target.validate(value, path)
	}

	var problems []string

	if len(s.types) > 0 && !s.accepts(value) {
		return []string{fmt.Sprintf("%s: want %s, got %s",
			path, strings.Join(s.types, " or "), jsonTypeOf(value))}
	}

	if s.hasConst && !reflect.DeepEqual(s.constValue, value) {
		problems = append(problems, fmt.Sprintf("%s: want the constant %s, got %s",
			path, jsonText(s.constValue), jsonText(value)))
	}

	if s.hasEnum && !slices.ContainsFunc(s.enum, func(member any) bool { return reflect.DeepEqual(member, value) }) {
		problems = append(problems, fmt.Sprintf("%s: %s is not one of %s",
			path, jsonText(value), jsonText(s.enum)))
	}

	if s.minimum != nil {
		if number, ok := value.(float64); ok && number < *s.minimum {
			problems = append(problems, fmt.Sprintf("%s: %s is less than the minimum %s",
				path, jsonText(number), jsonText(*s.minimum)))
		}
	}

	if s.pattern != nil {
		if text, ok := value.(string); ok && !s.pattern.MatchString(text) {
			problems = append(problems, fmt.Sprintf("%s: %s does not match the pattern %q",
				path, jsonText(text), s.pattern.String()))
		}
	}

	if object, ok := value.(map[string]any); ok {
		problems = append(problems, s.validateObject(object, path)...)
	}

	if array, ok := value.([]any); ok && s.items != nil {
		for i, item := range array {
			problems = append(problems, s.items.validate(item, path+"["+strconv.Itoa(i)+"]")...)
		}
	}

	return problems
}

// validateObject returns every way object fails s, which describes an object.
func (s *schema) validateObject(object map[string]any, path string) []string {
	var problems []string

	for _, name := range s.required {
		if _, ok := object[name]; !ok {
			problems = append(problems, path+": missing required property "+strconv.Quote(name))
		}
	}

	for _, name := range slices.Sorted(maps.Keys(object)) {
		property, ok := s.properties[name]
		if !ok {
			if s.additional != nil && !*s.additional {
				problems = append(problems, path+": unexpected property "+strconv.Quote(name))
			}

			continue
		}

		problems = append(problems, property.validate(object[name], path+"."+name)...)
	}

	return problems
}

// accepts reports whether value has one of the schema's types.
func (s *schema) accepts(value any) bool {
	actual := jsonTypeOf(value)

	for _, want := range s.types {
		if want == actual || (want == "number" && actual == "integer") {
			return true
		}
	}

	return false
}

// jsonTypeOf returns the JSON type of a value encoding/json decoded into the
// generic form. A whole number is an integer, which "number" also accepts.
func jsonTypeOf(value any) string {
	switch number := value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if number == math.Trunc(number) {
			return "integer"
		}

		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", value)
	}
}

// jsonText returns value as JSON text, for a failure message.
func jsonText(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}

	return string(data)
}

// validatorSchema is the schema the validator's own tests run against: it uses
// every keyword the report's schemas may use.
const validatorSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://example.com/validator.schema.json",
  "title": "validator self-test",
  "description": "one of every supported keyword",
  "type": "object",
  "additionalProperties": false,
  "required": ["schemaVersion", "kind", "items"],
  "properties": {
    "schemaVersion": {"const": 1},
    "kind": {"enum": ["assessment", "comparison"]},
    "count": {"type": "integer", "minimum": 0},
    "items": {"type": "array", "items": {"$ref": "#/$defs/item"}}
  },
  "$defs": {
    "item": {"type": "string", "pattern": "^[a-z]+$"}
  }
}`

// TestJSONSchemaValidator pins the validator with schemas of its own: it accepts a
// document that matches and reports the one problem of each document that does
// not, so the golden files below are checked by a validator known to refuse.
func TestJSONSchemaValidator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		instance    string
		wantProblem string
	}{
		{
			name:     "a matching document",
			instance: `{"schemaVersion":1,"kind":"assessment","items":["a","b"]}`,
		},
		{
			name:        "a missing required key",
			instance:    `{"schemaVersion":1,"items":[]}`,
			wantProblem: `missing required property "kind"`,
		},
		{
			name:        "an extra key under additionalProperties false",
			instance:    `{"schemaVersion":1,"kind":"assessment","items":[],"extra":true}`,
			wantProblem: `unexpected property "extra"`,
		},
		{
			name:        "a wrong type",
			instance:    `{"schemaVersion":1,"kind":"assessment","items":"x"}`,
			wantProblem: "want array, got string",
		},
		{
			name:        "a wrong const",
			instance:    `{"schemaVersion":2,"kind":"assessment","items":[]}`,
			wantProblem: "want the constant 1, got 2",
		},
		{
			name:        "a value outside enum",
			instance:    `{"schemaVersion":1,"kind":"report","items":[]}`,
			wantProblem: `"report" is not one of`,
		},
		{
			name:        "a null array",
			instance:    `{"schemaVersion":1,"kind":"assessment","items":null}`,
			wantProblem: "want array, got null",
		},
		{
			name:        "an item of the wrong type",
			instance:    `{"schemaVersion":1,"kind":"assessment","items":[7]}`,
			wantProblem: "want string, got integer",
		},
		{
			name:        "an item outside the pattern",
			instance:    `{"schemaVersion":1,"kind":"assessment","items":["A"]}`,
			wantProblem: "does not match the pattern",
		},
		{
			name:        "a count below the minimum",
			instance:    `{"schemaVersion":1,"kind":"assessment","items":[],"count":-1}`,
			wantProblem: "less than the minimum",
		},
	}

	s := parseSchemaFor(t, validatorSchema)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var instance any
			if err := json.Unmarshal([]byte(test.instance), &instance); err != nil {
				t.Fatalf("the test's own document is not JSON: %v", err)
			}

			problems := s.validate(instance, "$")

			if test.wantProblem == "" {
				if len(problems) != 0 {
					t.Errorf("validation problems = %q, want none", problems)
				}

				return
			}

			if len(problems) != 1 {
				t.Fatalf("validation problems = %q, want exactly one holding %q", problems, test.wantProblem)
			}

			if !strings.Contains(problems[0], test.wantProblem) {
				t.Errorf("the problem %q, want it to hold %q", problems[0], test.wantProblem)
			}
		})
	}
}

// TestJSONSchemaValidatorRefusesUnsupportedSchema pins the other half: a schema
// the validator cannot fully check is an error, never a schema it silently
// ignores.
func TestJSONSchemaValidatorRefusesUnsupportedSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema string
		want   string
	}{
		{
			name:   "an unknown keyword",
			schema: `{"type":"object","oneOf":[{"type":"string"}]}`,
			want:   `unsupported keyword "oneOf"`,
		},
		{
			name:   "a definitions block instead of $defs",
			schema: `{"definitions":{}}`,
			want:   `unsupported keyword "definitions"`,
		},
		{
			name:   "a non-local reference",
			schema: `{"$ref":"https://example.com/schema.json#/$defs/x"}`,
			want:   "not a local definition reference",
		},
		{
			name:   "a reference to another file",
			schema: `{"$ref":"other.schema.json#/$defs/x"}`,
			want:   "not a local definition reference",
		},
		{
			name:   "a reference naming no definition",
			schema: `{"$ref":"#/$defs/missing"}`,
			want:   "names no definition",
		},
		{
			name:   "additionalProperties true",
			schema: `{"additionalProperties":true}`,
			want:   "additionalProperties must be false",
		},
		{
			name:   "a schema for the additional properties",
			schema: `{"additionalProperties":{"type":"string"}}`,
			want:   "additionalProperties must be false",
		},
		{
			name:   "an unknown type name",
			schema: `{"type":["object","any"]}`,
			want:   `unknown type "any"`,
		},
		{
			name:   "a type that is not a type name",
			schema: `{"type":5}`,
			want:   "type must be a type name",
		},
		{
			name:   "a required that is not a list",
			schema: `{"required":"id"}`,
			want:   "required must be a list",
		},
		{
			name:   "a required name properties does not describe",
			schema: `{"type":"object","properties":{"id":{"type":"string"}},"required":["id","name"]}`,
			want:   `required names "name", which properties does not describe`,
		},
		{
			name:   "an invalid pattern",
			schema: `{"pattern":"["}`,
			want:   "pattern",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if _, err := parseSchema([]byte(test.schema)); err == nil {
				t.Fatalf("parseSchema() error = nil, want it to refuse the schema holding %q", test.want)
			} else if !strings.Contains(err.Error(), test.want) {
				t.Errorf("parseSchema() error = %q, want it to hold %q", err, test.want)
			}
		})
	}
}

// e2eGoldenDir is where the binary's end-to-end goldens live, relative to this
// package's directory: they are documents of the same format, written by the same
// reporters through the CLI, so the schemas must accept them too.
const e2eGoldenDir = "../../../../test/e2e/testdata"

// currentSchemaVersion is the schemaVersion every document this release writes
// declares (docs/adr/0018, PRD #190 milestone 8): a new version gets new schema
// files beside the old ones.
const currentSchemaVersion = 2

// TestGoldenDocumentsMatchSchema validates every golden document against the
// schema of its own kind and schemaVersion: the goldens are the specification of
// the format, so a document the schema refuses is not the format the repository
// ships. The reporter's own goldens must declare the current version; the
// binary's end-to-end goldens are validated through the relative path from this
// package at whatever version they declare, so the format the CLI writes is the
// format the schema describes.
func TestGoldenDocumentsMatchSchema(t *testing.T) {
	t.Parallel()

	// The reporter's own goldens: the documents this release writes.
	patterns := []struct {
		name    string
		pattern string
	}{
		{name: "assessment documents", pattern: "report*.golden"},
		{name: "comparison documents", pattern: "comparison*.golden"},
	}

	for _, test := range patterns {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			goldens, err := filepath.Glob(filepath.Join("testdata", test.pattern))
			if err != nil {
				t.Fatalf("globbing testdata/%s: %v", test.pattern, err)
			}

			if len(goldens) == 0 {
				t.Fatalf("no golden file matches testdata/%s: the %s must be pinned",
					test.pattern, test.name)
			}

			for _, path := range goldens {
				t.Run(filepath.Base(path), func(t *testing.T) {
					t.Parallel()

					// A golden of the current release is written by the current
					// reporter, so it declares the current version.
					if version := goldenSchemaVersion(t, path); version != currentSchemaVersion {
						t.Fatalf("%s declares schemaVersion %d, want %d: the reporter's own goldens are "+
							"documents of the version this release writes", path, version, currentSchemaVersion)
					}

					checkGoldenAgainstItsSchema(t, path)
				})
			}
		})
	}

	// The end-to-end goldens, which the CLI wrote: the fixtures' documents of both
	// kinds, and the hostile inventory's assessment, whose values the format has to
	// write as JSON strings.
	e2eGoldens := []string{
		"assess-ovhcloud-json.golden",
		"assess-all-json.golden",
		"assess-hostile-json.golden",
	}

	for _, golden := range e2eGoldens {
		t.Run(golden, func(t *testing.T) {
			t.Parallel()

			checkGoldenAgainstItsSchema(t, filepath.Join(e2eGoldenDir, golden))
		})
	}
}

// goldenHeader is what a document says about itself before anything else.
type goldenHeader struct {
	SchemaVersion int    `json:"schemaVersion"`
	Kind          string `json:"kind"`
}

// goldenSchemaVersion returns the schemaVersion the document at path declares.
func goldenSchemaVersion(tb testing.TB, path string) int {
	tb.Helper()

	return goldenHeaderOf(tb, path).SchemaVersion
}

// goldenHeaderOf reads the schemaVersion and kind of the document at path.
func goldenHeaderOf(tb testing.TB, path string) goldenHeader {
	tb.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("read the golden file %s: %v", path, err)
	}

	var header goldenHeader
	if err := json.Unmarshal(data, &header); err != nil {
		tb.Fatalf("%s is not a JSON document with a schemaVersion and a kind: %v", path, err)
	}

	return header
}

// schemaFileFor returns the schema file of a document kind and schemaVersion, as
// docs/adr/0018 names it: a new schemaVersion gets new files beside the old ones.
func schemaFileFor(kind string, version int) string {
	return kind + "-v" + strconv.Itoa(version) + ".schema.json"
}

// checkGoldenAgainstItsSchema fails the test when the golden document at path does
// not match the schema file of the kind and schemaVersion it declares.
func checkGoldenAgainstItsSchema(t *testing.T, path string) {
	t.Helper()

	header := goldenHeaderOf(t, path)
	checkGoldenAgainstSchema(t, path, schemaFileFor(header.Kind, header.SchemaVersion))
}

// checkGoldenAgainstSchema fails the test when the golden document at path does
// not match the named schema of this package.
func checkGoldenAgainstSchema(t *testing.T, path, schemaFile string) {
	t.Helper()

	s := readSchema(t, filepath.Join("schema", schemaFile))

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the golden file %s: %v", path, err)
	}

	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("%s is not valid JSON: %v", path, err)
	}

	for _, problem := range s.validate(document, "$") {
		t.Errorf("%s does not match schema/%s: %s", path, schemaFile, problem)
	}
}

// optionalProperties are the property names an object schema may declare without
// requiring them, because the document writes them only when it has something to
// say. They are the reason object's names, each written only when the reason sets
// it, the flags an estimate writes only when it estimated the resource
// (countAssumed and hoursAssumed), the cap an estimate writes only when the plan's
// offering bills a plan for at most that many hours a month (billingCapHours,
// ADR 0029), and the schema version 2 keys of a resource that cannot be priced: a
// resource priced by storage has no plan, an estimate that is not estimated has no
// amount, and a plan with no price in the estimate region has no price. Any other
// property a schema declares must be required, so a document can neither omit a
// key the format always writes nor gain one nobody checks.
var optionalProperties = []string{
	"target", "category", "offering", "capability",
	"plan", "estimate", "amount", "breakdown", "reason", "price",
	// The two assumptions an estimate records, written only when the resource is
	// estimated (ADR 0025), and the cap it was scaled by, written only when the
	// plan's offering caps its hours (ADR 0029).
	"countAssumed", "hoursAssumed", "billingCapHours",
	// A document names the region a run chose, at the top level and per
	// comparison target, only when --region chose one; sourceCost is written
	// only with estimates on.
	"region", "sourceCost",
}

// TestSchemaObjectsAreClosed pins that every schema of this package describes
// every key of every object its documents hold, and no other: an object schema
// declares additionalProperties false, describes every name it requires, and
// requires every name it describes apart from the few in optionalProperties. It
// loops over every schema file, so a new schemaVersion's files are held to the
// same rule as the ones before them.
func TestSchemaObjectsAreClosed(t *testing.T) {
	t.Parallel()

	for _, name := range schemaFiles(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := readSchema(t, filepath.Join("schema", name))

			objects := 0

			for i, sub := range s.subschemas() {
				if !slices.Contains(sub.types, "object") {
					continue
				}

				objects++

				where := fmt.Sprintf("subschema %d", i)

				if sub.additional == nil {
					t.Errorf("%s: additionalProperties is not declared, want false", where)
				}

				required := make(map[string]bool, len(sub.required))

				for _, requiredName := range sub.required {
					required[requiredName] = true

					if _, ok := sub.properties[requiredName]; !ok {
						t.Errorf("%s: required names %s, which properties does not describe",
							where, strconv.Quote(requiredName))
					}
				}

				for _, property := range slices.Sorted(maps.Keys(sub.properties)) {
					if required[property] || slices.Contains(optionalProperties, property) {
						continue
					}

					t.Errorf("%s: properties declares %s without requiring it, so a document may omit it",
						where, strconv.Quote(property))
				}
			}

			if objects == 0 {
				t.Fatal("the schema declares no object schema, so nothing was checked")
			}
		})
	}
}

// schemaFiles returns the base names of every schema document of this package,
// in byte order, so a test that must hold each one to a rule loops over all of
// them and a new schemaVersion's files are never skipped.
func schemaFiles(tb testing.TB) []string {
	tb.Helper()

	paths, err := filepath.Glob(filepath.Join("schema", "*.schema.json"))
	if err != nil {
		tb.Fatalf("globbing the schema files: %v", err)
	}

	if len(paths) == 0 {
		tb.Fatal("the package holds no schema file")
	}

	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}

	return names
}

// TestSchemaFilesDeclareDraft202012 pins that each schema file is a JSON document
// declaring the dialect the validator implements.
func TestSchemaFilesDeclareDraft202012(t *testing.T) {
	t.Parallel()

	for _, name := range schemaFiles(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join("schema", name)

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the schema %s: %v", path, err)
			}

			if !json.Valid(data) {
				t.Fatalf("%s is not valid JSON", path)
			}

			var header struct {
				Schema string `json:"$schema"`
			}

			if err := json.Unmarshal(data, &header); err != nil {
				t.Fatalf("decode the $schema of %s: %v", path, err)
			}

			if header.Schema != draft202012 {
				t.Errorf("%s declares $schema %q, want %q", path, header.Schema, draft202012)
			}
		})
	}
}

// parseSchemaFor parses data, failing the test when the schema is not one the
// validator supports.
func parseSchemaFor(tb testing.TB, data string) *schema {
	tb.Helper()

	s, err := parseSchema([]byte(data))
	if err != nil {
		tb.Fatalf("the schema is not one the validator supports: %v", err)
	}

	return s
}

// readSchema reads and parses one of the report's schema documents, failing the
// test when it is missing or uses a keyword the validator does not check.
func readSchema(tb testing.TB, path string) *schema {
	tb.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("read the schema %s: %v", path, err)
	}

	s, err := parseSchema(data)
	if err != nil {
		tb.Fatalf("the schema %s is not one the validator supports: %v", path, err)
	}

	return s
}
