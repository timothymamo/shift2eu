package openapibundle_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/internal/openapibundle"
)

// update rewrites the committed bundle instead of comparing with it:
// go test ./test/openapibundle -update.
var update = flag.Bool("update", false, "rewrite api/openapi.bundled.json")

// repoRoot is the module root, as the package directory go test runs in is
// test/openapibundle.
const repoRoot = "../.."

// The document the bundle is generated from, relative to the root, and the
// bundle every release publishes as shift2eu-server_openapi.json (issue #139).
// The bundle's path is openapibundle.Path, the one source the bundler, this
// package and the release's .goreleaser.yaml glob all read: a second copy
// here would let the release publish a file the bundler never writes.
const (
	source = "api/openapi.json"

	bundled = openapibundle.Path
)

// updateCommand is what a stale or missing bundle tells the reader to run.
const updateCommand = "go test ./test/openapibundle -update"

// reportSchemas are the schemas api/openapi.json refers to by a
// repository-relative path, and the name each one must have in the bundle:
// the base name of the file, its kebab parts title-cased, Report inserted
// before the version.
var reportSchemas = map[string]string{
	"AssessmentReportV2": "assessment-v2.schema.json",
	"ComparisonReportV2": "comparison-v2.schema.json",
}

// TestBundleIsCommitted pins the self-contained OpenAPI document of issue
// #139: api/openapi.bundled.json is exactly what openapibundle.Bundle makes of
// api/openapi.json with the report schemas, so it cannot drift from the
// document or the schemas it is generated from. A missing or stale file fails
// with the command that writes it.
func TestBundleIsCommitted(t *testing.T) {
	// Not parallel: -update writes the file the other test reads.
	got, err := openapibundle.Bundle(os.DirFS(repoRoot), source)
	if err != nil {
		t.Fatalf("bundle %s: %v", source, err)
	}

	path := filepath.Join(repoRoot, filepath.FromSlash(bundled))

	if *update {
		// 0o600: the golden file internal/testutil/golden.go writes as well; git
		// stores the mode as 644 anyway.
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("write %s: %v", bundled, err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run %s to write it)", bundled, err, updateCommand)
	}

	if diff := cmp.Diff(string(want), string(got)); diff != "" {
		t.Errorf("%s is not %s bundled: run %s and commit the result (-want +got):\n%s",
			bundled, source, updateCommand, diff)
	}
}

// TestBundleIsRegularFile pins the auditor's finding L-1 of issue #139: the
// release copies api/openapi.bundled.json into shift2eu-server_openapi.json and
// attests it. A symbolic link at that path would make the copy, GoReleaser and
// the attestation publish and attest whatever it points to, a file outside the
// repository and outside review, so the path holds a regular file.
func TestBundleIsRegularFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repoRoot, filepath.FromSlash(bundled))

	if err := regularFile(path); err != nil {
		t.Fatalf("%s: %v: the release publishes and attests this file, so a symlink at its path would publish and attest its target; run %s to write it as a regular file", bundled, err, updateCommand)
	}
}

// TestRegularFileChecksTheFileItself pins the guard TestBundleIsRegularFile
// uses, exercised both ways: a regular file passes, and a symbolic link to one,
// a directory and a missing path are each refused. The link is the case the
// guard exists for, so its failure names the mode rather than the target.
func TestRegularFileChecksTheFileItself(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	file := filepath.Join(dir, "regular.json")
	if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}

	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(file, link); err != nil {
		t.Fatalf("symlink %s: %v", link, err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "regular file", path: file},
		{name: "symbolic link to a regular file", path: link, want: true},
		{name: "directory", path: dir, want: true},
		{name: "missing path", path: filepath.Join(dir, "absent.json"), want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := regularFile(tc.path)
			if tc.want != (err != nil) {
				t.Fatalf("regularFile(%q) error = %v, want error: %t", tc.path, err, tc.want)
			}

			if tc.want && !strings.Contains(err.Error(), "regular file") && !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("regularFile(%q) error = %v, want the mode guard's or fs.ErrNotExist", tc.path, err)
			}
		})
	}
}

// TestBundledDocumentIsSelfContained pins what the committed bundle is for: it
// resolves outside the repository. Every $ref it holds is a pointer into the
// document itself, #/..., that resolves as a JSON pointer, so no ref is
// repository-relative (../...) any more, and components.schemas holds the two
// report schemas the published document must carry.
func TestBundledDocumentIsSelfContained(t *testing.T) {
	t.Parallel()

	doc := readBundled(t)

	found := refs(doc)
	if len(found) == 0 {
		t.Fatalf("%s holds no $ref, want the document's refs", bundled)
	}

	for _, ref := range found {
		if strings.Contains(ref, "../") {
			t.Errorf("%s: the ref %q is relative to the repository, want a pointer into the document: the bundle is published on its own", bundled, ref)

			continue
		}

		if !strings.HasPrefix(ref, "#/") {
			t.Errorf("%s: the ref %q is not a pointer into the document, want one starting with #/", bundled, ref)

			continue
		}

		if _, err := resolve(doc, ref); err != nil {
			t.Errorf("%s: the ref %q does not resolve: %v", bundled, ref, err)
		}
	}

	schemas := schemasOf(t, doc)

	for name, file := range reportSchemas {
		if _, ok := schemas[name]; !ok {
			t.Errorf("%s: components.schemas holds no %s, want the %s schema, the one api/openapi.json refers to by a path", bundled, name, file)
		}
	}
}

// TestBundledDocumentEscapesNoHTML pins the bundle's form where the review of
// issue #139 found it: api/openapi.bundled.json holds the text of
// api/openapi.json as it is written. A JSON encoder escapes <, > and & for
// HTML by default, which turned the API's own "Authorization: Bearer <token>"
// into Bearer \u003ctoken\u003e, so every one of the three the source holds
// appears literally in the bundle and none appears as a \u escape.
func TestBundledDocumentEscapesNoHTML(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(bundled)))
	if err != nil {
		t.Fatalf("read %s: %v (run %s to write it)", bundled, err, updateCommand)
	}

	doc, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(source)))
	if err != nil {
		t.Fatalf("read %s: %v", source, err)
	}

	held := false

	for _, literal := range []string{"<", ">", "&"} {
		if !bytes.Contains(doc, []byte(literal)) {
			continue
		}

		held = true

		if !bytes.Contains(data, []byte(literal)) {
			t.Errorf("%s does not hold %q, which %s does: want the text as it is written, escaped for no format", bundled, literal, source)
		}
	}

	if !held {
		t.Fatalf("%s holds none of <, > and &, so this test checks nothing", source)
	}

	for _, escaped := range []string{`\u003c`, `\u003C`, `\u003e`, `\u003E`, `\u0026`} {
		if bytes.Contains(data, []byte(escaped)) {
			t.Errorf("%s holds %s, want the character it stands for: an HTML-escaping encoder wrote the bundle, run %s", bundled, escaped, updateCommand)
		}
	}
}

// readBundled returns the committed bundle, decoded as one JSON document.
func readBundled(t *testing.T) any {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(bundled)))
	if err != nil {
		t.Fatalf("read %s: %v (run %s to write it)", bundled, err, updateCommand)
	}

	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s is not one JSON document: %v (run %s to write it)", bundled, err, updateCommand)
	}

	return doc
}

// schemasOf returns components.schemas of doc.
func schemasOf(t *testing.T, doc any) map[string]any {
	t.Helper()

	root, ok := doc.(map[string]any)
	if !ok {
		t.Fatalf("%s is not a JSON object", bundled)
	}

	components, ok := root["components"].(map[string]any)
	if !ok {
		t.Fatalf("%s holds no components object", bundled)
	}

	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatalf("%s holds no components.schemas object", bundled)
	}

	return schemas
}

// refs returns every $ref value doc holds, in a stable order: the keys of an
// object are walked sorted, so a failure names the same ref every run.
func refs(doc any) []string {
	var found []string

	var walk func(v any)

	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(v))
			for key := range v {
				keys = append(keys, key)
			}

			sort.Strings(keys)

			for _, key := range keys {
				if key == "$ref" {
					if ref, ok := v[key].(string); ok {
						found = append(found, ref)
					}

					continue
				}

				walk(v[key])
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}

	walk(doc)

	return found
}

// regularFile returns nil when path is a regular file and an error otherwise.
// os.Lstat does not follow a symbolic link, so a link is refused as the link it
// is, before anything reads or copies its target.
func regularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("has mode %s, want a regular file", info.Mode())
	}

	return nil
}

// resolve returns the value of doc that the JSON pointer ref names, and an
// error when it names none: every step is an object key or an array index, and
// ~1 and ~0 stand for / and ~.
func resolve(doc any, ref string) (any, error) {
	cur := doc

	for _, step := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		step = strings.ReplaceAll(strings.ReplaceAll(step, "~1", "/"), "~0", "~")

		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[step]
			if !ok {
				return nil, fmt.Errorf("no key %q", step)
			}

			cur = next
		case []any:
			i, err := strconv.Atoi(step)
			if err != nil || i < 0 || i >= len(node) {
				return nil, fmt.Errorf("no entry %q, want one of the %d the array holds", step, len(node))
			}

			cur = node[i]
		default:
			return nil, fmt.Errorf("%q is below a %T", step, cur)
		}
	}

	return cur, nil
}
