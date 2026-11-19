package apiparity_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/report/jsonreport"
	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/adapter/request/assessmentrequest"
	"github.com/timothymamo/shift2eu/internal/adapter/source/inventoryfile"
	"github.com/timothymamo/shift2eu/internal/adapter/target/ovhcloud"
	"github.com/timothymamo/shift2eu/internal/adapter/target/scaleway"
	"github.com/timothymamo/shift2eu/internal/adapter/target/upcloud"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/cli"
	"github.com/timothymamo/shift2eu/internal/httpapi"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// The two sanitised inventories the parity cases read: one with several
// categories and fits, and the hostile one the report tests also use.
const (
	inventoryFixture = "../e2e/testdata/inventory.json"
	hostileFixture   = "../e2e/testdata/inventory-hostile.json"
)

// openAPIPath is the API document, relative to this package: its 200 answer to
// POST /v1/assessments names the report schemas, by path, that a report is
// checked against.
const openAPIPath = "../../api/openapi.json"

// The paths, formats and inventory name both sides share.
const (
	assessmentsPath = "/v1/assessments"
	jsonFormat      = "json"
	markdownFormat  = "markdown"
	// inventoryName is the name both reports must carry: the API names every
	// body inventory.json because a body has no file name, so the CLI is given a
	// file of that name.
	inventoryName = "inventory.json"
)

// parityCase is one inventory, target list and format both driving adapters
// render.
type parityCase struct {
	name    string
	fixture string
	// targets is the --target keys and the API's targets, in order; "all" alone
	// selects every target on both sides.
	targets []string
	// format is the CLI's --format value and accept the API's Accept header.
	format string
	accept string
}

// paritySchemaVersion is the JSON report version both sides ask for: version 2,
// the document every client of both driving adapters receives today. The version a
// run asks for is tested in each adapter's own tests (docs/adr/0018, issue #307).
const paritySchemaVersion = 2

// TestTheAPIAndTheCLIAgreeOnTheReport pins the parity PRD #103 promises: for the
// same inventory and targets, POST /v1/assessments answers the bytes
// `shift2eu assess` writes, for the JSON report and the Markdown report, for one
// target, a list and every target, and for a hostile inventory too. Both sides
// are wired with the real adapters, as their composition roots wire them, so a
// difference in decoding, target rules, ordering or rendering shows up as a
// difference in the bytes.
func TestTheAPIAndTheCLIAgreeOnTheReport(t *testing.T) {
	t.Parallel()

	schemas := reportSchemas(t)

	tests := []parityCase{
		{
			name:    "one target, JSON",
			fixture: inventoryFixture,
			targets: []string{"scaleway"},
			format:  jsonFormat,
			accept:  "application/json",
		},
		{
			name:    "a list of targets, JSON",
			fixture: inventoryFixture,
			targets: []string{"ovhcloud", "upcloud"},
			format:  jsonFormat,
			accept:  "application/json",
		},
		{
			name:    "every target, JSON",
			fixture: inventoryFixture,
			targets: []string{"all"},
			format:  jsonFormat,
			accept:  "application/json",
		},
		{
			name:    "one target, Markdown",
			fixture: inventoryFixture,
			targets: []string{"scaleway"},
			format:  markdownFormat,
			accept:  "text/markdown",
		},
		{
			name:    "every target, Markdown",
			fixture: inventoryFixture,
			targets: []string{"all"},
			format:  markdownFormat,
			accept:  "text/markdown",
		},
		{
			name:    "a hostile inventory, JSON",
			fixture: hostileFixture,
			targets: []string{"scaleway"},
			format:  jsonFormat,
			accept:  "application/json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			document := readFixture(t, tc.fixture)
			inventoryPath := writeInventory(t, document)

			cliReport := runAssess(t, inventoryPath, tc)
			apiReport := postAssessment(t, document, tc.targets, tc.format, tc.accept)

			if !bytes.Equal(cliReport, apiReport) {
				t.Errorf("the API's body differs from `shift2eu assess --target %s --format %s` (-CLI +API):\n%s",
					strings.Join(tc.targets, ","), tc.format, cmp.Diff(string(cliReport), string(apiReport)))
			}

			if tc.format == jsonFormat {
				validateReport(t, schemas, apiReport)
			}
		})
	}
}

// TestTheAPIDocumentsEveryReportVersion pins that the API document describes
// every version the JSON report has: a request may name schemaVersion 3, which
// the API answers with a version 3 document, so the 200 JSON answer must
// reference the schemas of both versions of both kinds beside the version 2 ones
// it referenced before version 3 existed (docs/adr/0018, docs/adr/0030, issue
// #307).
func TestTheAPIDocumentsEveryReportVersion(t *testing.T) {
	t.Parallel()

	schemas := reportSchemas(t)

	for _, version := range []int{2, 3} {
		for _, kind := range []string{"assessment", "comparison"} {
			if _, ok := schemas[reportSchemaKey{kind: kind, version: version}]; !ok {
				t.Errorf("%s references no version %d %s schema for the 200 JSON answer to POST %s, so a client "+
					"that asks for schemaVersion %d holds a document the API document does not describe: want a "+
					"$ref to %s",
					openAPIPath, version, kind, assessmentsPath, version, schemaRefPath(kind, version))
			}
		}
	}
}

// schemaRefPath is where the API document references the schema of a report kind
// and version, relative to its own directory, as the document writes it.
func schemaRefPath(kind string, version int) string {
	return filepath.ToSlash(filepath.Join("..", "internal", "adapter", "report", "jsonreport", "schema",
		kind+"-v"+strconv.Itoa(version)+".schema.json"))
}

// emptyRegionExit is the exit code of a command line the CLI refuses: its usage
// errors use it, and nothing is run.
const emptyRegionExit = 2

// TestTheAPIAndTheCLIRefuseAnEmptyRegion pins that both driving adapters refuse
// an empty region: `--region scaleway=` is a usage error before any inventory is
// read, and `"regions": {"scaleway": ""}` is 400 invalid_request naming the
// member by its index. Neither may read it as "no region" and assess every
// region, which would answer a client that pinned one with something else
// (PRD #190 milestone 8, security-audit follow-up L2).
func TestTheAPIAndTheCLIRefuseAnEmptyRegion(t *testing.T) {
	t.Parallel()

	document := readFixture(t, inventoryFixture)

	// The CLI: a usage error, so it names the region option and writes no report.
	var stdout, stderr bytes.Buffer

	args := []string{
		"assess",
		"--inventory", writeInventory(t, document),
		"--target", "scaleway",
		"--region", "scaleway=",
	}

	if code := cli.Run(t.Context(), args, &stdout, &stderr, cliDeps()); code != emptyRegionExit {
		t.Errorf("shift2eu %s = exit %d, want %d; stderr: %q",
			strings.Join(args, " "), code, emptyRegionExit, stderr.String())
	}

	if stdout.Len() != 0 {
		t.Errorf("shift2eu %s wrote a report for an empty region: %q", strings.Join(args, " "), stdout.String())
	}

	if !strings.Contains(stderr.String(), "region") {
		t.Errorf("stderr = %q, want it to name the region option", stderr.String())
	}

	// The API: 400 invalid_request, the same refusal the decoder makes.
	rec := postAssessmentWithRegions(t, document, []string{"scaleway"}, map[string]string{"scaleway": ""})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST %s = %d, want %d; body: %q",
			assessmentsPath, rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	var doc struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("the error body is not the documented JSON shape: %v; body: %q", err, rec.Body.String())
	}

	if doc.Error.Code != "invalid_request" {
		t.Errorf("error code = %q, want %q; message: %q",
			doc.Error.Code, "invalid_request", doc.Error.Message)
	}

	if !strings.Contains(doc.Error.Message, "regions[0]") {
		t.Errorf("the message = %q, want it to name the member regions[0]", doc.Error.Message)
	}
}

// postAssessmentWithRegions posts document and targets with a regions object to
// the real API handler and returns the recorded response, whatever its status.
func postAssessmentWithRegions(t *testing.T, document []byte, targets []string,
	regions map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(struct {
		Inventory json.RawMessage   `json:"inventory"`
		Targets   []string          `json:"targets"`
		Regions   map[string]string `json:"regions"`
	}{Inventory: document, Targets: targets, Regions: regions})
	if err != nil {
		t.Fatalf("encoding the request body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, assessmentsPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	apiHandler(t).ServeHTTP(rec, req)

	return rec
}

// runAssess runs the real CLI over inventoryPath and returns its report, failing
// the test when the run does not succeed cleanly.
func runAssess(t *testing.T, inventoryPath string, tc parityCase) []byte {
	t.Helper()

	args := []string{
		"assess",
		"--inventory", inventoryPath,
		"--target", strings.Join(tc.targets, ","),
		"--format", tc.format,
	}

	// The CLI writes version 3 unless the run names one, so the parity case asks
	// for the version both sides are compared on.
	if tc.format == jsonFormat {
		args = append(args, "--schema-version", strconv.Itoa(paritySchemaVersion))
	}

	var stdout, stderr bytes.Buffer

	if code := cli.Run(t.Context(), args, &stdout, &stderr, cliDeps()); code != 0 {
		t.Fatalf("shift2eu %s = exit %d, want 0; stderr: %q", strings.Join(args, " "), code, stderr.String())
	}

	if stderr.Len() != 0 {
		t.Errorf("shift2eu %s wrote to stderr on success: %q", strings.Join(args, " "), stderr.String())
	}

	return stdout.Bytes()
}

// postAssessment posts document and targets to the real API handler and returns
// its report body, failing the test when the request is not answered with 200.
// The body asks for paritySchemaVersion when the format is JSON, as the CLI run
// it is compared with does.
func postAssessment(t *testing.T, document []byte, targets []string, format, accept string) []byte {
	t.Helper()

	var schemaVersion *int
	if format == jsonFormat {
		schemaVersion = &[]int{paritySchemaVersion}[0]
	}

	body, err := json.Marshal(struct {
		Inventory     json.RawMessage `json:"inventory"`
		Targets       []string        `json:"targets"`
		SchemaVersion *int            `json:"schemaVersion,omitempty"`
	}{Inventory: document, Targets: targets, SchemaVersion: schemaVersion})
	if err != nil {
		t.Fatalf("encoding the request body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, assessmentsPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	rec := httptest.NewRecorder()
	apiHandler(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s = %d, want %d; body: %q",
			assessmentsPath, rec.Code, http.StatusOK, rec.Body.String())
	}

	return rec.Body.Bytes()
}

// writeInventory copies document to a fresh temporary directory as
// inventory.json and returns its path, so the CLI reports the name the API
// reports.
func writeInventory(t *testing.T, document []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), inventoryName)

	if err := os.WriteFile(path, document, 0o600); err != nil {
		t.Fatalf("writing the inventory %s: %v", path, err)
	}

	return path
}

// readFixture returns the bytes of a fixture file.
func readFixture(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the fixture %s: %v", path, err)
	}

	return data
}

// reportDocument is the part of api/openapi.json this file reads: the 200 answer
// of POST /v1/assessments and the schemas its JSON body is one of.
type reportDocument struct {
	Paths map[string]struct {
		Post struct {
			Responses map[string]struct {
				Content map[string]struct {
					Schema struct {
						OneOf []struct {
							Ref string `json:"$ref"`
						} `json:"oneOf"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		} `json:"post"`
	} `json:"paths"`
}

// reportSchemaKey identifies a report schema the API documents: the kind of
// document it describes and the schemaVersion it pins. The 200 answer may
// reference several versions of both kinds, because a client asks for the
// version it reads (docs/adr/0018, issue #307).
type reportSchemaKey struct {
	kind    string
	version int
}

// reportSchemas returns the schema of every report kind and schemaVersion the
// API documents for a 200 JSON answer, keyed by the kind and schemaVersion each
// schema describes. The document decides which schemas those are: it references
// them by path from the 200 response's oneOf, so a schema the document stops
// referencing stops being checked here.
func reportSchemas(t *testing.T) map[reportSchemaKey]*testutil.Schema {
	t.Helper()

	refs := reportSchemaRefs(t)

	schemas := make(map[reportSchemaKey]*testutil.Schema, len(refs))

	for _, ref := range refs {
		path := filepath.Join(filepath.Dir(openAPIPath), filepath.FromSlash(ref))
		data := readFixture(t, path)
		key := schemaIdentity(t, path, data)

		if _, ok := schemas[key]; ok {
			t.Errorf("the document references two report schemas of the kind %q and schemaVersion %d: %s",
				key.kind, key.version, path)
		}

		_, schema, err := testutil.ParseSchema(data)
		if err != nil {
			t.Fatalf("parsing the %s schemaVersion %d schema %s: %v", key.kind, key.version, path, err)
		}

		schemas[key] = schema
	}

	return schemas
}

// reportSchemaRefs returns the paths the document's 200 JSON answer references
// for its oneOf, as the document writes them: relative to its own directory.
func reportSchemaRefs(t *testing.T) []string {
	t.Helper()

	var document reportDocument
	if err := json.Unmarshal(readFixture(t, openAPIPath), &document); err != nil {
		t.Fatalf("decoding %s: %v", openAPIPath, err)
	}

	content := document.Paths[assessmentsPath].Post.Responses["200"].Content["application/json"]

	if len(content.Schema.OneOf) == 0 {
		t.Fatalf("%s declares no oneOf for the 200 JSON answer to POST %s", openAPIPath, assessmentsPath)
	}

	refs := make([]string, 0, len(content.Schema.OneOf))

	for _, alternative := range content.Schema.OneOf {
		if alternative.Ref == "" {
			t.Fatalf("%s declares an alternative of the 200 JSON answer with no $ref, want the report schemas by path",
				openAPIPath)
		}

		if strings.HasPrefix(alternative.Ref, "#") {
			t.Fatalf("%s references %q inside itself, want the report schema file", openAPIPath, alternative.Ref)
		}

		refs = append(refs, alternative.Ref)
	}

	if len(refs) < 2 {
		t.Fatalf("%s references %d report schemas for the 200 JSON answer, want the assessment and the comparison one",
			openAPIPath, len(refs))
	}

	return refs
}

// schemaIdentity returns what a report schema describes: its kind, the constant
// of its kind property or its title when the constant is missing, and the
// schemaVersion its schemaVersion property pins.
func schemaIdentity(t *testing.T, path string, data []byte) reportSchemaKey {
	t.Helper()

	var schema struct {
		Title      string `json:"title"`
		Properties struct {
			Kind struct {
				Const string `json:"const"`
			} `json:"kind"`
			SchemaVersion struct {
				Const int `json:"const"`
			} `json:"schemaVersion"`
		} `json:"properties"`
	}

	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("decoding the schema %s: %v", path, err)
	}

	key := reportSchemaKey{
		kind:    schema.Properties.Kind.Const,
		version: schema.Properties.SchemaVersion.Const,
	}

	if key.kind == "" {
		key.kind = schema.Title
	}

	if key.kind == "" {
		t.Fatalf("the schema %s names no kind constant and has no title, so a report cannot be matched to it", path)
	}

	if key.version == 0 {
		t.Fatalf("the schema %s pins no schemaVersion constant, so the report version it describes is unknown", path)
	}

	return key
}

// validateReport holds a JSON report to the schema of the kind and schemaVersion
// it declares.
func validateReport(t *testing.T, schemas map[reportSchemaKey]*testutil.Schema, report []byte) {
	t.Helper()

	var envelope struct {
		Kind          string `json:"kind"`
		SchemaVersion int    `json:"schemaVersion"`
	}

	if err := json.Unmarshal(report, &envelope); err != nil {
		t.Fatalf("decoding the report's kind and schemaVersion: %v; body: %s", err, report)
	}

	schema, ok := schemas[reportSchemaKey{kind: envelope.Kind, version: envelope.SchemaVersion}]
	if !ok {
		t.Fatalf("the document references no version %d schema of the kind %q for the 200 JSON answer to POST %s, "+
			"so a client that asks for that version holds a document the API document does not describe",
			envelope.SchemaVersion, envelope.Kind, assessmentsPath)
	}

	schema.ValidateJSON(t, report)
}

// cliDeps wires the assess path of the CLI as cmd/shift2eu does: the inventory
// file, the embedded OVHcloud, Scaleway and UpCloud catalogs and the two report
// formats, each rendering a single report and a comparison.
func cliDeps() cli.Deps {
	return cli.Deps{
		NewInventorySource: func(path string) analysis.InventorySource {
			return inventoryfile.New(path)
		},
		Targets: targetSources(),
		Formats: map[string]cli.Format{
			jsonFormat: {
				NewReporter:           func(schemaVersion int) analysis.Reporter { return jsonreport.New(schemaVersion) },
				NewComparisonReporter: func(schemaVersion int) analysis.ComparisonReporter { return jsonreport.New(schemaVersion) },
			},
			markdownFormat: {
				NewReporter:           func(int) analysis.Reporter { return markdown.New() },
				NewComparisonReporter: func(int) analysis.ComparisonReporter { return markdown.New() },
			},
		},
	}
}

// decodeRequest is what the server's composition root passes as
// httpapi.Deps.DecodeRequest: the strict request decoder, whose Request is the
// adapter's own type, carried over to the port's.
func decodeRequest(body []byte) (httpapi.Request, error) {
	req, err := assessmentrequest.Decode(body)
	if err != nil {
		return httpapi.Request{}, err
	}

	return httpapi.Request{
		Inventory:     req.Inventory,
		Targets:       req.Targets,
		Regions:       req.Regions,
		Estimate:      req.Estimate,
		SchemaVersion: req.SchemaVersion,
	}, nil
}

// apiHandler wires the HTTP API as its composition root does: the strict request
// decoder, the in-memory inventory source, the same targets and reporters as the
// CLI, and the body cap assessmentrequest declares.
func apiHandler(tb testing.TB) http.Handler {
	tb.Helper()

	handler, err := httpapi.New(httpapi.Deps{
		DecodeRequest: decodeRequest,
		NewInventorySource: func(document []byte) analysis.InventorySource {
			return inventoryfile.FromBytes(document)
		},
		Targets: targetSources(),
		JSON: httpapi.Format{
			NewReporter:           func(schemaVersion int) analysis.Reporter { return jsonreport.New(schemaVersion) },
			NewComparisonReporter: func(schemaVersion int) analysis.ComparisonReporter { return jsonreport.New(schemaVersion) },
		},
		Markdown: httpapi.Format{
			NewReporter:           func(int) analysis.Reporter { return markdown.New() },
			NewComparisonReporter: func(int) analysis.ComparisonReporter { return markdown.New() },
		},
	}, httpapi.WithMaxBodyBytes(assessmentrequest.MaxBodySize))
	if err != nil {
		tb.Fatalf("httpapi.New() returned unexpected error: %v", err)
	}

	return handler
}

// targetSources wires the embedded target catalogs, as both composition roots
// do.
func targetSources() map[string]func() analysis.CatalogSource {
	return map[string]func() analysis.CatalogSource{
		"ovhcloud": func() analysis.CatalogSource { return ovhcloud.New() },
		"scaleway": func() analysis.CatalogSource { return scaleway.New() },
		"upcloud":  func() analysis.CatalogSource { return upcloud.New() },
	}
}
