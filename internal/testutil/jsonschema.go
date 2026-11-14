package testutil

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// This file holds the JSON Schema validator the tests that hold a document to a
// schema share: internal/httpapi checks response bodies against the OpenAPI
// document's schemas and test/apiparity checks the API's report against
// jsonreport's schemas. It implements the draft 2020-12 subset those documents
// use, and refuses every keyword it does not implement, so a construct it
// cannot check never passes unvalidated.

// schemaKeywords lists every keyword the validator checks. A schema that uses
// any other keyword is refused.
var schemaKeywords = []string{
	"$schema", "$id", "title", "description", "$defs", "$ref", "type", "properties",
	"required", "additionalProperties", "items", "minItems", "minLength", "enum", "const", "minimum", "pattern",
}

// schemaTypes are the type names a schema's "type" may hold.
var schemaTypes = []string{"object", "array", "string", "integer", "number", "boolean", "null"}

// Schema is one JSON Schema object, parsed by a SchemaDoc. It is read only, so
// one schema may validate any number of documents, concurrently too.
type Schema struct {
	// doc is the document the schema belongs to, which resolves its references.
	doc *SchemaDoc
	// types are the accepted JSON types, empty for any type.
	types []string
	// properties describes an object's members and required names them.
	properties map[string]*Schema
	required   []string
	// additional is the "additionalProperties" keyword, which the schemas use
	// only as false.
	additional *bool
	// items is the schema of an array's elements and minItems its least length.
	items    *Schema
	minItems *int
	// minLength is the least number of characters of a string.
	minLength *int
	// enum and const restrict the value itself.
	enum       []any
	hasEnum    bool
	constValue any
	hasConst   bool
	// minimum and pattern restrict a number and a string.
	minimum *float64
	pattern *regexp.Regexp
	// ref is a reference to another schema of the document, "#/...", or empty.
	ref string
	// defs are the schema's "$defs", registered in the document by pointer.
	defs map[string]*Schema
}

// SchemaDoc is a set of JSON Schema objects that refer to each other by local
// JSON pointer: "#/$defs/<name>" in a schema file, "#/components/schemas/<name>"
// in an OpenAPI document.
type SchemaDoc struct {
	nodes map[string]*Schema
}

// NewSchemaDoc returns an empty document.
func NewSchemaDoc() *SchemaDoc {
	return &SchemaDoc{nodes: map[string]*Schema{}}
}

// ParseSchema parses data as one JSON Schema document and returns the document
// and its root schema. The document's "$defs" become resolvable as
// "#/$defs/<name>".
func ParseSchema(data []byte) (*SchemaDoc, *Schema, error) {
	raw, err := decodeJSONObject("the schema", data)
	if err != nil {
		return nil, nil, err
	}

	doc := NewSchemaDoc()

	root, err := doc.parseSchema(raw)
	if err != nil {
		return nil, nil, err
	}

	if err := doc.checkReferences(root); err != nil {
		return nil, nil, err
	}

	return doc, root, nil
}

// ParseOpenAPISchemas parses the components.schemas of an OpenAPI 3.1 document.
// Its schemas refer to each other as "#/components/schemas/<name>"; Require
// returns one by that pointer.
func ParseOpenAPISchemas(data []byte) (*SchemaDoc, error) {
	var document struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}

	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode the OpenAPI document: %w", err)
	}

	if len(document.Components.Schemas) == 0 {
		return nil, fmt.Errorf("the OpenAPI document declares no components.schemas")
	}

	doc := NewSchemaDoc()
	schemas := make([]*Schema, 0, len(document.Components.Schemas))

	for _, name := range slices.Sorted(maps.Keys(document.Components.Schemas)) {
		raw, err := decodeJSONObject(name, document.Components.Schemas[name])
		if err != nil {
			return nil, err
		}

		schema, err := doc.parseSchema(raw)
		if err != nil {
			return nil, err
		}

		doc.nodes[openAPISchemaPointer(name)] = schema
		schemas = append(schemas, schema)
	}

	if err := doc.checkReferences(schemas...); err != nil {
		return nil, err
	}

	return doc, nil
}

// openAPISchemaPointer is the pointer a component schema is registered under.
func openAPISchemaPointer(name string) string {
	return "#/components/schemas/" + name
}

// Require returns the schema the document registered at pointer, failing tb
// when it registered none: a body checked against a missing schema would
// otherwise pass unvalidated.
func (d *SchemaDoc) Require(tb testing.TB, pointer string) *Schema {
	tb.Helper()

	schema, ok := d.nodes[pointer]
	if !ok {
		tb.Fatalf("the schema document declares no schema at %q", pointer)
	}

	return schema
}

// ParseSchemaObject parses one schema object of the document, for a schema the
// document uses inline, such as a response body's. Its references resolve
// against the document's registered schemas, and a reference to one it does not
// register fails tb.
func (d *SchemaDoc) ParseSchemaObject(tb testing.TB, data []byte) *Schema {
	tb.Helper()

	raw, err := decodeJSONObject("the schema", data)
	if err != nil {
		tb.Fatalf("%v", err)
	}

	schema, err := d.parseSchema(raw)
	if err != nil {
		tb.Fatalf("the schema is not one the validator implements: %v", err)
	}

	if err := d.checkReferences(schema); err != nil {
		tb.Fatalf("%v", err)
	}

	return schema
}

// Mismatches returns every way data, one JSON document, fails schema, each as
// "path: problem". An empty result means data matches. A document that does not
// decode as JSON is an error, not a mismatch.
func (s *Schema) Mismatches(data []byte) ([]string, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decode the JSON document: %w", err)
	}

	return s.validate(value, "$"), nil
}

// ValidateJSON fails tb unless data is one JSON document that matches s.
func (s *Schema) ValidateJSON(tb testing.TB, data []byte) {
	tb.Helper()

	mismatches, err := s.Mismatches(data)
	if err != nil {
		tb.Fatalf("%v; body: %s", err, data)
	}

	if len(mismatches) > 0 {
		tb.Fatalf("the JSON document does not match the schema:\n%s\nbody: %s",
			strings.Join(mismatches, "\n"), data)
	}
}

// parseSchema parses one schema object into the document, registering the
// "$defs" it holds.
func (d *SchemaDoc) parseSchema(raw map[string]json.RawMessage) (*Schema, error) {
	schema := &Schema{doc: d, defs: map[string]*Schema{}}
	if err := schema.parseKeywords(raw); err != nil {
		return nil, err
	}

	d.registerDefs(schema)

	return schema, nil
}

// registerDefs makes every "$defs" definition below schema resolvable by its
// "#/$defs/<name>" pointer.
func (d *SchemaDoc) registerDefs(schema *Schema) {
	for _, sub := range schema.subschemas() {
		for name, definition := range sub.defs {
			d.nodes["#/$defs/"+name] = definition
		}
	}
}

// checkReferences fails when a reference in schemas names no schema the
// document registered.
func (d *SchemaDoc) checkReferences(schemas ...*Schema) error {
	for _, schema := range schemas {
		for _, sub := range schema.subschemas() {
			if sub.ref == "" {
				continue
			}

			if _, ok := d.nodes[sub.ref]; !ok {
				return fmt.Errorf("the reference %q names no schema in the document", sub.ref)
			}
		}
	}

	return nil
}

// parseKeywords parses every keyword of one schema object. A required name the
// object does not describe is an error: the validator would otherwise check for
// a key it cannot validate, so a schema that names one is not one it can fully
// check. A reference beside another keyword is an error too: validate applies a
// reference instead of the keywords around it, so such a schema would leave its
// own keywords unchecked.
func (s *Schema) parseKeywords(raw map[string]json.RawMessage) error {
	for _, keyword := range slices.Sorted(maps.Keys(raw)) {
		if err := s.parseKeyword(keyword, raw[keyword]); err != nil {
			return err
		}
	}

	if s.ref != "" && len(raw) > 1 {
		return fmt.Errorf("$ref beside %s is unsupported: a schema object that refers to another must hold only $ref",
			siblingKeyword(raw))
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
func (s *Schema) parseKeyword(keyword string, value json.RawMessage) error {
	switch keyword {
	case "$schema", "$id", "title", "description":
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return fmt.Errorf("%s must be a string: %w", keyword, err)
		}

		return nil
	case "$defs":
		return decodeObjectMembers(keyword, value, func(members map[string]json.RawMessage) error {
			for _, name := range slices.Sorted(maps.Keys(members)) {
				definition, err := s.doc.parseRawSchema(members[name], name)
				if err != nil {
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

		if !strings.HasPrefix(ref, "#/") {
			return fmt.Errorf("$ref %q is not a local JSON pointer", ref)
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
			if !slices.Contains(schemaTypes, name) {
				return fmt.Errorf("unknown type %q: the validator supports %s",
					name, strings.Join(schemaTypes, ", "))
			}
		}

		return nil
	case "properties":
		return decodeObjectMembers(keyword, value, func(members map[string]json.RawMessage) error {
			s.properties = make(map[string]*Schema, len(members))

			for _, name := range slices.Sorted(maps.Keys(members)) {
				property, err := s.doc.parseRawSchema(members[name], name)
				if err != nil {
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
			return fmt.Errorf("additionalProperties must be a boolean: %w", err)
		}

		s.additional = &allowed

		return nil
	case "items":
		items, err := s.doc.parseRawSchema(value, "items")
		if err != nil {
			return err
		}

		s.items = items

		return nil
	case "minItems":
		var least int
		if err := json.Unmarshal(value, &least); err != nil {
			return fmt.Errorf("minItems must be an integer: %w", err)
		}

		s.minItems = &least

		return nil
	case "minLength":
		var least int
		if err := json.Unmarshal(value, &least); err != nil {
			return fmt.Errorf("minLength must be an integer: %w", err)
		}

		s.minLength = &least

		return nil
	case "enum":
		if err := json.Unmarshal(value, &s.enum); err != nil {
			return fmt.Errorf("enum must be a list of values: %w", err)
		}

		if s.enum == nil {
			return fmt.Errorf("enum must be a list of values, not null")
		}

		s.hasEnum = true

		return nil
	case "const":
		if err := json.Unmarshal(value, &s.constValue); err != nil {
			return fmt.Errorf("const must be one JSON value: %w", err)
		}

		s.hasConst = true

		return nil
	case "minimum":
		var least float64
		if err := json.Unmarshal(value, &least); err != nil {
			return fmt.Errorf("minimum must be a number: %w", err)
		}

		s.minimum = &least

		return nil
	case "pattern":
		var pattern string
		if err := json.Unmarshal(value, &pattern); err != nil {
			return fmt.Errorf("pattern must be a string: %w", err)
		}

		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("pattern %q is not a regular expression: %w", pattern, err)
		}

		s.pattern = compiled

		return nil
	default:
		return fmt.Errorf("unsupported keyword %q: the validator checks only %s",
			keyword, strings.Join(schemaKeywords, ", "))
	}
}

// siblingKeyword names, quoted, the first keyword of a schema object beside
// $ref, for the error parseKeywords returns for such an object.
func siblingKeyword(raw map[string]json.RawMessage) string {
	for _, keyword := range slices.Sorted(maps.Keys(raw)) {
		if keyword != "$ref" {
			return strconv.Quote(keyword)
		}
	}

	return ""
}

// parseRawSchema decodes one schema object and parses it into the document.
func (d *SchemaDoc) parseRawSchema(value json.RawMessage, what string) (*Schema, error) {
	raw, err := decodeJSONObject(what, value)
	if err != nil {
		return nil, err
	}

	return d.parseSchema(raw)
}

// decodeJSONObject decodes data as a JSON object, never null.
func decodeJSONObject(what string, data []byte) (map[string]json.RawMessage, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return nil, fmt.Errorf("%s must be a JSON object: %w", what, err)
	}

	if members == nil {
		return nil, fmt.Errorf("%s must be a JSON object, not null", what)
	}

	return members, nil
}

// decodeObjectMembers decodes value as a JSON object and parses it with parse.
func decodeObjectMembers(what string, value json.RawMessage, parse func(map[string]json.RawMessage) error) error {
	members, err := decodeJSONObject(what, value)
	if err != nil {
		return err
	}

	return parse(members)
}

// subschemas returns every schema object below s, s included, in a fixed order.
func (s *Schema) subschemas() []*Schema {
	out := []*Schema{s}

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

// validate returns every way value fails s, each as "path: problem".
func (s *Schema) validate(value any, path string) []string {
	if s.ref != "" {
		target, ok := s.doc.nodes[s.ref]
		if !ok {
			return []string{path + ": reference " + s.ref + " names no schema"}
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

	if s.minLength != nil {
		// JSON Schema counts characters, not bytes, so a string of multi-byte
		// runes is as long as its runes.
		if text, ok := value.(string); ok {
			if length := len([]rune(text)); length < *s.minLength {
				problems = append(problems, fmt.Sprintf("%s: want at least %d characters, got %d",
					path, *s.minLength, length))
			}
		}
	}

	if object, ok := value.(map[string]any); ok {
		problems = append(problems, s.validateObject(object, path)...)
	}

	if array, ok := value.([]any); ok {
		if s.minItems != nil && len(array) < *s.minItems {
			problems = append(problems, fmt.Sprintf("%s: want at least %d items, got %d",
				path, *s.minItems, len(array)))
		}

		if s.items != nil {
			for i, item := range array {
				problems = append(problems, s.items.validate(item, path+"["+strconv.Itoa(i)+"]")...)
			}
		}
	}

	return problems
}

// validateObject returns every way object fails s, which describes an object.
func (s *Schema) validateObject(object map[string]any, path string) []string {
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
func (s *Schema) accepts(value any) bool {
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
