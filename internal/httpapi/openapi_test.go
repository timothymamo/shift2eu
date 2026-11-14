package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/internal/httpapi"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// This file holds the handler to the document it publishes: api/openapi.json
// (OpenAPI 3.1, ADR 0019). It pins the routes and methods, the statuses each
// operation declares, the JSON bodies against the document's schemas and the
// response headers, so the contract a client reads and the API that answers it
// cannot drift apart.

// openAPIPath is the API document, relative to this package.
const openAPIPath = "../../api/openapi.json"

// The paths the document declares, as the tests name them.
const (
	assessmentsPath = "/v1/assessments"
	targetsPath     = "/v1/targets"
)

// reportsOperation is the operation that answers with a report.
const reportsOperation = "POST " + assessmentsPath

// standardMethods are the methods every path answers, declared or not.
var standardMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
	http.MethodPatch, http.MethodDelete, http.MethodOptions,
}

// documentedMethods are the path-item keys that declare an operation.
var documentedMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// undocumentedPaths are paths the document declares no operation for, so the
// handler must answer them as an unknown route.
var undocumentedPaths = []string{
	"/",
	"/v1",
	"/v1/assessments/extra",
	"/v1/targets/ovhcloud",
	"/v2/assessments",
	"/healthz/ready",
	"/readyz/live",
}

// apiDocument is the part of the OpenAPI document these tests read.
type apiDocument struct {
	OpenAPI string `json:"openapi"`
	// Paths maps a path to its path item: the operation keys of the document.
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Responses map[string]apiResponse `json:"responses"`
		Headers   map[string]apiHeader   `json:"headers"`
	} `json:"components"`
}

// apiOperation is one operation of a path item.
type apiOperation struct {
	OperationID string                 `json:"operationId"`
	Responses   map[string]apiResponse `json:"responses"`
	RequestBody apiRequestBody         `json:"requestBody"`
}

// apiRequestBody is the body an operation declares.
type apiRequestBody struct {
	Required bool                  `json:"required"`
	Content  map[string]apiContent `json:"content"`
}

// apiResponse is one declared response of an operation, or one of
// components.responses its $ref names.
type apiResponse struct {
	Ref     string                `json:"$ref"`
	Headers map[string]apiHeader  `json:"headers"`
	Content map[string]apiContent `json:"content"`
}

// apiHeader is one declared response header, or one of components.headers.
type apiHeader struct {
	Ref         string `json:"$ref"`
	Description string `json:"description"`
	Schema      struct {
		Type  string   `json:"type"`
		Const *string  `json:"const"`
		Enum  []string `json:"enum"`
	} `json:"schema"`
}

// value returns what the declaration says the header holds: its constant, its
// listed values, or the description when it fixes none.
func (h apiHeader) value() string {
	switch {
	case h.Schema.Const != nil:
		return *h.Schema.Const
	case len(h.Schema.Enum) > 0:
		return strings.Join(h.Schema.Enum, ", ")
	default:
		return h.Description
	}
}

// apiContent is one declared media type of a response.
type apiContent struct {
	Schema json.RawMessage `json:"schema"`
}

// apiRoute is one operation the document declares.
type apiRoute struct {
	path   string
	method string
	op     *apiOperation
}

// name is the operation as a scenario names it, "METHOD /path".
func (r apiRoute) name() string {
	return r.method + " " + r.path
}

// loadOpenAPI reads and decodes the document, returning it with its bytes so a
// test can also parse its schemas.
func loadOpenAPI(t *testing.T) (*apiDocument, []byte) {
	t.Helper()

	data, err := os.ReadFile(openAPIPath)
	if err != nil {
		t.Fatalf("reading the OpenAPI document %s: %v", openAPIPath, err)
	}

	var doc apiDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decoding %s: %v", openAPIPath, err)
	}

	if doc.OpenAPI == "" {
		t.Fatalf("%s declares no OpenAPI version", openAPIPath)
	}

	if len(doc.Paths) == 0 {
		t.Fatalf("%s declares no path", openAPIPath)
	}

	return &doc, data
}

// routes returns every operation the document declares, in a fixed order.
func (d *apiDocument) routes(t *testing.T) []apiRoute {
	t.Helper()

	var routes []apiRoute

	for _, path := range slices.Sorted(maps.Keys(d.Paths)) {
		item := d.Paths[path]
		for _, key := range slices.Sorted(maps.Keys(item)) {
			if !slices.Contains(documentedMethods, key) {
				continue
			}

			var op apiOperation
			if err := json.Unmarshal(item[key], &op); err != nil {
				t.Fatalf("decoding the %s operation of %s: %v", key, path, err)
			}

			if len(op.Responses) == 0 {
				t.Errorf("the document declares the operation %s %s with no response", strings.ToUpper(key), path)
			}

			routes = append(routes, apiRoute{path: path, method: strings.ToUpper(key), op: &op})
		}
	}

	if len(routes) == 0 {
		t.Fatal("the document declares no operation")
	}

	return routes
}

// operation returns the operation the document declares for "METHOD /path".
func (d *apiDocument) operation(t *testing.T, name string) *apiOperation {
	t.Helper()

	for _, route := range d.routes(t) {
		if route.name() == name {
			return route.op
		}
	}

	t.Fatalf("the document declares no operation %q", name)

	return nil
}

// methodNames returns the methods the document declares for path, sorted.
func (d *apiDocument) methodNames(t *testing.T, path string) []string {
	t.Helper()

	var methods []string

	for _, key := range slices.Sorted(maps.Keys(d.Paths[path])) {
		if slices.Contains(documentedMethods, key) {
			methods = append(methods, strings.ToUpper(key))
		}
	}

	return methods
}

// resolveResponse follows a $ref to components.responses.
func (d *apiDocument) resolveResponse(t *testing.T, resp apiResponse) *apiResponse {
	t.Helper()

	for resp.Ref != "" {
		const prefix = "#/components/responses/"

		if !strings.HasPrefix(resp.Ref, prefix) {
			t.Fatalf("the response reference %q is not one to components.responses", resp.Ref)
		}

		name := strings.TrimPrefix(resp.Ref, prefix)

		component, ok := d.Components.Responses[name]
		if !ok {
			t.Fatalf("the document references the response %q, which components.responses does not declare", name)
		}

		resp = component
	}

	return &resp
}

// resolveHeader follows a $ref to components.headers.
func (d *apiDocument) resolveHeader(t *testing.T, header apiHeader) apiHeader {
	t.Helper()

	if header.Ref == "" {
		return header
	}

	const prefix = "#/components/headers/"

	if !strings.HasPrefix(header.Ref, prefix) {
		t.Fatalf("the header reference %q is not one to components.headers", header.Ref)
	}

	name := strings.TrimPrefix(header.Ref, prefix)

	component, ok := d.Components.Headers[name]
	if !ok {
		t.Fatalf("the document references the header %q, which components.headers does not declare", name)
	}

	return component
}

// declaredResponse returns the response the document declares for status of op,
// or nil when it declares none. 404 and 405 are the two component responses the
// document applies to every path and method no operation declares.
func (d *apiDocument) declaredResponse(t *testing.T, op *apiOperation, status int) *apiResponse {
	t.Helper()

	if resp, ok := op.Responses[strconv.Itoa(status)]; ok {
		return d.resolveResponse(t, resp)
	}

	name, ok := map[int]string{
		http.StatusNotFound:         "NotFound",
		http.StatusMethodNotAllowed: "MethodNotAllowed",
	}[status]

	if !ok {
		return nil
	}

	resp, ok := d.Components.Responses[name]
	if !ok {
		return nil
	}

	return d.resolveResponse(t, resp)
}

// TestOpenAPIReferencesResolve pins that every reference a client has to follow
// leads somewhere: a "#..." pointer stays inside the document, and every other
// reference names a file beside the document, which must exist and be valid
// JSON. A reference to a missing or broken schema file leaves the document
// describing a body no client can check, even though the document itself parses.
func TestOpenAPIReferencesResolve(t *testing.T) {
	t.Parallel()

	_, data := loadOpenAPI(t)

	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decoding %s: %v", openAPIPath, err)
	}

	refs := map[string]bool{}
	collectRefs(t, document, refs)

	external := 0

	for _, ref := range slices.Sorted(maps.Keys(refs)) {
		if strings.HasPrefix(ref, "#") {
			continue
		}

		external++

		path := filepath.Join(filepath.Dir(openAPIPath), filepath.FromSlash(ref))

		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("the document references %q, which cannot be read: %v", ref, err)

			continue
		}

		if !json.Valid(content) {
			t.Errorf("the document references %q, which is not valid JSON", ref)
		}
	}

	if external == 0 {
		t.Errorf("the document references no file beside itself, so this test checks nothing: " +
			"the report schemas should be referenced by path")
	}
}

// collectRefs adds every $ref value the document holds to refs.
func collectRefs(t *testing.T, value any, refs map[string]bool) {
	t.Helper()

	switch node := value.(type) {
	case map[string]any:
		for _, key := range slices.Sorted(maps.Keys(node)) {
			if key != "$ref" {
				collectRefs(t, node[key], refs)

				continue
			}

			ref, ok := node[key].(string)
			if !ok {
				t.Fatalf("the document has a $ref that is not a string: %v", node[key])
			}

			refs[ref] = true
		}
	case []any:
		for _, item := range node {
			collectRefs(t, item, refs)
		}
	}
}

// TestOpenAPIExposesTheErrorHeaders pins that the document declares, on every
// response where it declares CORS headers, that a browser may read Retry-After
// and WWW-Authenticate. A browser at an allowed origin cannot read either header
// without Access-Control-Expose-Headers, so a document that promises CORS and
// omits it would hide the API's own 503 busy and 401 answers from the page.
func TestOpenAPIExposesTheErrorHeaders(t *testing.T) {
	t.Parallel()

	doc, _ := loadOpenAPI(t)

	cors, exposing := 0, 0

	for _, route := range doc.routes(t) {
		for _, key := range slices.Sorted(maps.Keys(route.op.Responses)) {
			resp := doc.resolveResponse(t, route.op.Responses[key])

			if _, ok := resp.Headers["Access-Control-Allow-Origin"]; !ok {
				continue
			}

			cors++

			header, ok := resp.Headers["Access-Control-Expose-Headers"]
			if !ok {
				t.Errorf("%s declares CORS headers on its %s response but not Access-Control-Expose-Headers, "+
					"so a browser at an allowed origin cannot read Retry-After or WWW-Authenticate",
					route.name(), key)

				continue
			}

			exposing++

			value := doc.resolveHeader(t, header).value()

			for _, name := range []string{"Retry-After", "WWW-Authenticate"} {
				if !headerHasToken(value, name) {
					t.Errorf("%s declares Access-Control-Expose-Headers %q on its %s response, want it to name %s",
						route.name(), value, key, name)
				}
			}
		}
	}

	if cors == 0 {
		t.Fatal("the document declares CORS headers on no response, so this test checks nothing")
	}

	if exposing != cors {
		t.Errorf("%d of the %d responses that declare CORS headers declare Access-Control-Expose-Headers, want all",
			exposing, cors)
	}
}

// TestOpenAPIDocumentedOperationsAnswer pins that the handler serves every
// operation the document declares, with a status the operation declares: a
// documented route the handler does not serve would be a contract lie whichever
// way it is wrong.
func TestOpenAPIDocumentedOperationsAnswer(t *testing.T) {
	t.Parallel()

	doc, _ := loadOpenAPI(t)

	for _, route := range doc.routes(t) {
		t.Run(route.name(), func(t *testing.T) {
			t.Parallel()

			handler := mustNew(t, newHarness(t).deps(), httpapi.WithAllowedOrigins(allowedOrigin))

			rec := serve(handler, documentedRequest(t, route))

			if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
				t.Fatalf("%s = %d, want the status the document declares; body: %q",
					route.name(), rec.Code, rec.Body.String())
			}

			requireHardeningHeaders(t, rec)

			if doc.declaredResponse(t, route.op, rec.Code) == nil {
				t.Errorf("%s answers %d, which the document does not declare for it", route.name(), rec.Code)
			}
		})
	}
}

// TestOpenAPIUndeclaredMethodsAre405 pins the document's silence on a method:
// every path answers each standard method it does not declare with 405
// method_not_allowed, an Allow header naming the methods it does declare, and
// never with a 404, which would say the path does not exist.
func TestOpenAPIUndeclaredMethodsAre405(t *testing.T) {
	t.Parallel()

	doc, _ := loadOpenAPI(t)

	for _, path := range slices.Sorted(maps.Keys(doc.Paths)) {
		declared := doc.methodNames(t, path)

		for _, method := range standardMethods {
			if slices.Contains(declared, method) {
				continue
			}

			t.Run(method+" "+path, func(t *testing.T) {
				t.Parallel()

				handler := mustNew(t, newHarness(t).deps())

				rec := serve(handler, newRequest(method, path))

				if rec.Code != http.StatusMethodNotAllowed {
					t.Fatalf("%s %s = %d, want %d; body: %q",
						method, path, rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
				}

				requireHardeningHeaders(t, rec)
				decodeError(t, rec, "method_not_allowed")

				allow := rec.Header().Get("Allow")
				if strings.TrimSpace(allow) == "" {
					t.Fatalf("%s %s answered 405 without an Allow header, which the document declares", method, path)
				}

				// OPTIONS is answered only for a preflight from an allowed origin,
				// so the Allow header need not name it; every other declared
				// method is one the path answers as it stands.
				for _, allowed := range declared {
					if allowed == http.MethodOptions {
						continue
					}

					if !headerHasToken(allow, allowed) {
						t.Errorf("Allow = %q, want it to name the documented method %s", allow, allowed)
					}
				}
			})
		}
	}
}

// TestOpenAPIUndocumentedPathsAre404 pins that a path the document declares no
// operation for is 404 not_found, never a route the document hides from a
// client.
func TestOpenAPIUndocumentedPathsAre404(t *testing.T) {
	t.Parallel()

	doc, _ := loadOpenAPI(t)

	if _, ok := doc.Components.Responses["NotFound"]; !ok {
		t.Fatal("the document applies 404 to undeclared paths but declares no components.responses.NotFound")
	}

	if _, ok := doc.Components.Responses["MethodNotAllowed"]; !ok {
		t.Fatal("the document applies 405 to undeclared methods but declares no components.responses.MethodNotAllowed")
	}

	for _, path := range undocumentedPaths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			if _, ok := doc.Paths[path]; ok {
				t.Fatalf("the test calls %q an undocumented path, but the document declares it", path)
			}

			handler := mustNew(t, newHarness(t).deps())

			rec := serve(handler, newRequest(http.MethodGet, path))

			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET %s = %d, want %d; body: %q", path, rec.Code, http.StatusNotFound, rec.Body.String())
			}

			requireHardeningHeaders(t, rec)
			decodeError(t, rec, "not_found")
		})
	}
}

// apiScenario is one request the handler answers, with the status it must
// answer: the statuses every operation declares are driven through these.
type apiScenario struct {
	name string
	// operation is the documented operation the scenario targets.
	operation string
	// configure changes the fakes before the handler is built.
	configure func(*harness)
	// options are the handler's options.
	options func() []httpapi.Option
	// request builds the request a fresh handler is sent.
	request func(t *testing.T) *http.Request
	// run, when set, serves the request itself, for a scenario that needs more
	// than one request or a reporter held open.
	run func(t *testing.T) *httptest.ResponseRecorder
	// marker is a value no response may echo, if any.
	marker     string
	wantStatus int
}

// apiScenarios returns one request per status every operation declares, plus
// every error code the bodies carry. A status that no scenario produces fails
// TestOpenAPIResponsesMatchTheDocument unless it is in unreachableStatuses.
func apiScenarios() []apiScenario {
	return []apiScenario{
		{
			name:       "a JSON report",
			operation:  reportsOperation,
			request:    func(t *testing.T) *http.Request { t.Helper(); return newAssessRequest(assessBody(t, alphaKey)) },
			wantStatus: http.StatusOK,
		},
		{
			name:      "a Markdown report",
			operation: reportsOperation,
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return withAccept(newAssessRequest(assessBody(t, alphaKey)), "text/markdown")
			},
			wantStatus: http.StatusOK,
		},
		{
			name:      "a body the decoder refuses",
			operation: reportsOperation,
			configure: func(h *harness) {
				h.decoder.err = errors.New(`decode request: missing field "targets"`)
			},
			request:    func(t *testing.T) *http.Request { t.Helper(); return newAssessRequest(`{}`) },
			wantStatus: http.StatusBadRequest,
		},
		{
			name:      "an inventory the reader refuses",
			operation: reportsOperation,
			configure: func(h *harness) { h.inventory.err = errors.New("inventory: bad") },
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return newAssessRequest(assessBody(t, alphaKey))
			},
			// The 400 invalid_inventory message is the reader's own message, so it
			// may quote the inventory; TestInvalidInventory pins that it is one
			// escaped line. No other code echoes anything of the request.
			wantStatus: http.StatusBadRequest,
		},
		{
			name:      "a target list the CLI's rules refuse",
			operation: reportsOperation,
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return newAssessRequest(assessBody(t, "MARKER-T-77"))
			},
			marker:     "MARKER-T-77",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:      "a request without the bearer token",
			operation: reportsOperation,
			options:   func() []httpapi.Option { return []httpapi.Option{httpapi.WithBearerToken(bearerToken)} },
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return newAssessRequest(assessBody(t, alphaKey))
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:      "an Accept header nothing satisfies",
			operation: reportsOperation,
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return withAccept(newAssessRequest(assessBody(t, alphaKey)), "text/html")
			},
			wantStatus: http.StatusNotAcceptable,
		},
		{
			name:       "a body over the configured cap",
			operation:  reportsOperation,
			options:    func() []httpapi.Option { return []httpapi.Option{httpapi.WithMaxBodyBytes(64)} },
			request:    func(t *testing.T) *http.Request { t.Helper(); return newAssessRequest(assessBody(t, alphaKey)) },
			wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name:      "a body of another media type",
			operation: reportsOperation,
			request: func(t *testing.T) *http.Request {
				t.Helper()

				req := newAssessRequest(assessBody(t, alphaKey))
				req.Header.Set("Content-Type", "text/plain")

				return req
			},
			wantStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:       "a catalog that cannot be loaded",
			operation:  reportsOperation,
			configure:  func(h *harness) { h.catalogs[alphaKey].err = errors.New("catalog: MARKER-CAT-1") },
			request:    func(t *testing.T) *http.Request { t.Helper(); return newAssessRequest(assessBody(t, alphaKey)) },
			marker:     "MARKER-CAT-1",
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "a second assessment while the concurrency cap is reached",
			operation:  reportsOperation,
			run:        busyScenario,
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:      "a preflight from an allowed origin",
			operation: "OPTIONS /v1/assessments",
			options:   func() []httpapi.Option { return []httpapi.Option{httpapi.WithAllowedOrigins(allowedOrigin)} },
			request: func(t *testing.T) *http.Request {
				t.Helper()
				return preflightRequest(t, "/v1/assessments", http.MethodPost)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "a preflight without an allowed origin",
			operation:  "OPTIONS /v1/assessments",
			request:    func(*testing.T) *http.Request { return newRequest(http.MethodOptions, "/v1/assessments") },
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			// The host check comes before CORS, so a preflight for another host is
			// 421 misdirected_request and never the 204 an allowed origin alone
			// would earn: the document declares it too.
			name:      "a preflight for another host",
			operation: "OPTIONS /v1/assessments",
			options: func() []httpapi.Option {
				return []httpapi.Option{httpapi.WithAllowedHosts(allowedHost), httpapi.WithAllowedOrigins(allowedOrigin)}
			},
			request: func(t *testing.T) *http.Request {
				t.Helper()

				req := preflightRequest(t, "/v1/assessments", http.MethodPost)
				req.Host = "other.example.com"

				return req
			},
			wantStatus: http.StatusMisdirectedRequest,
		},
		{
			name:       "the target list",
			operation:  "GET " + targetsPath,
			request:    func(*testing.T) *http.Request { return newRequest(http.MethodGet, targetsPath) },
			wantStatus: http.StatusOK,
		},
		{
			name:       "the target list without the bearer token",
			operation:  "GET " + targetsPath,
			options:    func() []httpapi.Option { return []httpapi.Option{httpapi.WithBearerToken(bearerToken)} },
			request:    func(*testing.T) *http.Request { return newRequest(http.MethodGet, targetsPath) },
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "the target list when a catalog cannot be loaded",
			operation:  "GET " + targetsPath,
			configure:  func(h *harness) { h.catalogs[betaKey].err = errors.New("catalog: MARKER-CAT-2") },
			request:    func(*testing.T) *http.Request { return newRequest(http.MethodGet, targetsPath) },
			marker:     "MARKER-CAT-2",
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:      "a targets preflight from an allowed origin",
			operation: "OPTIONS /v1/targets",
			options:   func() []httpapi.Option { return []httpapi.Option{httpapi.WithAllowedOrigins(allowedOrigin)} },
			request: func(t *testing.T) *http.Request {
				t.Helper()
				return preflightRequest(t, targetsPath, http.MethodGet)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "a targets preflight without an allowed origin",
			operation:  "OPTIONS /v1/targets",
			request:    func(*testing.T) *http.Request { return newRequest(http.MethodOptions, targetsPath) },
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:      "a targets preflight for another host",
			operation: "OPTIONS /v1/targets",
			options: func() []httpapi.Option {
				return []httpapi.Option{httpapi.WithAllowedHosts(allowedHost), httpapi.WithAllowedOrigins(allowedOrigin)}
			},
			request: func(t *testing.T) *http.Request {
				t.Helper()

				req := preflightRequest(t, targetsPath, http.MethodGet)
				req.Host = "other.example.com"

				return req
			},
			wantStatus: http.StatusMisdirectedRequest,
		},
		{
			name:       "liveness",
			operation:  "GET /healthz",
			request:    func(*testing.T) *http.Request { return newRequest(http.MethodGet, "/healthz") },
			wantStatus: http.StatusOK,
		},
		{
			name:       "readiness",
			operation:  "GET /readyz",
			request:    func(*testing.T) *http.Request { return newRequest(http.MethodGet, "/readyz") },
			wantStatus: http.StatusOK,
		},
		{
			name:      "readiness when the check fails",
			operation: "GET /readyz",
			options: func() []httpapi.Option {
				return []httpapi.Option{httpapi.WithReadiness(func(context.Context) error {
					return errors.New("readiness: MARKER-READY-1")
				})}
			},
			request:    func(*testing.T) *http.Request { return newRequest(http.MethodGet, "/readyz") },
			marker:     "MARKER-READY-1",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:      "readiness when the check panics",
			operation: "GET /readyz",
			options: func() []httpapi.Option {
				return []httpapi.Option{httpapi.WithReadiness(func(context.Context) error {
					panic("MARKER-PANIC-READY")
				})}
			},
			request:    func(*testing.T) *http.Request { return newRequest(http.MethodGet, "/readyz") },
			marker:     "MARKER-PANIC-READY",
			wantStatus: http.StatusInternalServerError,
		},
		{
			// A request the API does not serve, by Host, is 421 misdirected_request
			// before the body is decoded: the document declares it too.
			name:      "a report for another host",
			operation: reportsOperation,
			options:   func() []httpapi.Option { return []httpapi.Option{httpapi.WithAllowedHosts(allowedHost)} },
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return assessRequestForHost(t, "other.example.com")
			},
			wantStatus: http.StatusMisdirectedRequest,
		},
		{
			name:      "the target list for another host",
			operation: "GET " + targetsPath,
			options:   func() []httpapi.Option { return []httpapi.Option{httpapi.WithAllowedHosts(allowedHost)} },
			request: func(*testing.T) *http.Request {
				req := newRequest(http.MethodGet, targetsPath)
				req.Host = "other.example.com"

				return req
			},
			wantStatus: http.StatusMisdirectedRequest,
		},
	}
}

// busyScenario reaches the concurrency cap: one assessment holds the only slot
// while a second one is answered, and the first is then let go.
func busyScenario(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()

	h := newHarness(t)

	gate := make(chan struct{})
	started := make(chan struct{})
	h.jsonReporter.gate = gate
	h.jsonReporter.started = started

	handler := mustNew(t, h.deps(), httpapi.WithMaxConcurrent(1))

	firstReq := newAssessRequest(assessBody(t, alphaKey))
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstDone <- serve(handler, firstReq) }()

	select {
	case <-started:
	case <-time.After(settle):
		t.Fatal("the first assessment never reached the reporter")
	}

	rec := serveWithin(t, handler, newAssessRequest(assessBody(t, betaKey)), settle)

	close(gate)

	select {
	case first := <-firstDone:
		if first.Code != http.StatusOK {
			t.Errorf("the first assessment = %d, want %d; body: %q",
				first.Code, http.StatusOK, first.Body.String())
		}
	case <-time.After(settle):
		t.Fatal("the first assessment did not finish after the gate opened")
	}

	return rec
}

// preflightRequest returns a CORS preflight from the allowed origin for method.
func preflightRequest(t *testing.T, path, method string) *http.Request {
	t.Helper()

	req := newRequest(http.MethodOptions, path)
	req.Header.Set("Origin", allowedOrigin)
	req.Header.Set("Access-Control-Request-Method", method)

	return req
}

// documentedRequest returns the request that reaches the documented operation
// with an answer the document declares.
func documentedRequest(t *testing.T, route apiRoute) *http.Request {
	t.Helper()

	switch {
	case route.method == http.MethodPost && route.path == assessmentsPath:
		return newAssessRequest(assessBody(t, alphaKey))
	case route.method == http.MethodOptions:
		return preflightRequest(t, route.path, http.MethodPost)
	case route.method == http.MethodGet:
		return newRequest(route.method, route.path)
	default:
		t.Fatalf("the test has no request for the documented operation %s; add one", route.name())

		return nil
	}
}

// unreachableStatuses are the declared statuses no scenario produces, with the
// reason each is out of reach. A status listed here with an empty reason still
// fails the coverage check.
var unreachableStatuses = map[string]map[int]string{
	"GET /healthz": {
		http.StatusInternalServerError: "liveness has no dependency that can fail and no panic of its own; only " +
			"the handler's recovery wrapper could answer it, and no fake reaches that",
	},
}

// TestOpenAPIResponsesMatchTheDocument pins the handler to the document's
// operations: every scenario answers the status it expects, that status is
// declared for the operation, every header the response declares is present
// with its documented value, and every JSON body matches the declared schema.
func TestOpenAPIResponsesMatchTheDocument(t *testing.T) {
	t.Parallel()

	doc, data := loadOpenAPI(t)

	schemas, err := testutil.ParseOpenAPISchemas(data)
	if err != nil {
		t.Fatalf("parsing the document's schemas: %v", err)
	}

	var mu sync.Mutex

	seen := map[string]map[int]bool{}

	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()

		requireEveryDeclaredStatusIsDriven(t, doc, seen)
	})

	for _, tc := range apiScenarios() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := runScenario(t, tc)

			if rec.Code != tc.wantStatus {
				t.Fatalf("%s = %d, want %d; body: %q", tc.operation, rec.Code, tc.wantStatus, rec.Body.String())
			}

			requireHardeningHeaders(t, rec)

			resp := doc.declaredResponse(t, doc.operation(t, tc.operation), rec.Code)
			if resp == nil {
				t.Fatalf("%s answers %d, which the document does not declare for it", tc.operation, rec.Code)
			}

			requireDeclaredHeaders(t, doc, resp, rec)
			requireDeclaredBody(t, schemas, tc, resp, rec)

			if tc.marker != "" {
				requireNoEcho(t, rec, tc.marker)
			}

			mu.Lock()
			if seen[tc.operation] == nil {
				seen[tc.operation] = map[int]bool{}
			}

			seen[tc.operation][rec.Code] = true
			mu.Unlock()
		})
	}
}

// runScenario builds a fresh harness, configures its fakes and the handler's
// options, and serves the scenario's request.
func runScenario(t *testing.T, tc apiScenario) *httptest.ResponseRecorder {
	t.Helper()

	if tc.run != nil {
		return tc.run(t)
	}

	h := newHarness(t)
	if tc.configure != nil {
		tc.configure(h)
	}

	var opts []httpapi.Option
	if tc.options != nil {
		opts = tc.options()
	}

	return serve(h.handler(t, opts...), tc.request(t))
}

// requireDeclaredHeaders pins the headers the response declares: each is
// present, and a header the document fixes to one value carries it.
func requireDeclaredHeaders(t *testing.T, doc *apiDocument, resp *apiResponse, rec *httptest.ResponseRecorder) {
	t.Helper()

	for _, name := range slices.Sorted(maps.Keys(resp.Headers)) {
		header := doc.resolveHeader(t, resp.Headers[name])

		value := rec.Header().Get(name)
		if strings.TrimSpace(value) == "" {
			t.Errorf("the response has no %s header, which the document declares for it", name)

			continue
		}

		if header.Schema.Const != nil && value != *header.Schema.Const {
			t.Errorf("%s = %q, want the documented %q", name, value, *header.Schema.Const)
		}

		if name == "Access-Control-Allow-Origin" && value != allowedOrigin {
			t.Errorf("Access-Control-Allow-Origin = %q, want the request's origin %q", value, allowedOrigin)
		}
	}
}

// requireDeclaredBody pins the response body to the document: an empty body
// where the response declares no content, the Markdown media type for a
// Markdown report, and every JSON body against the schema the response declares
// for it.
func requireDeclaredBody(t *testing.T, schemas *testutil.SchemaDoc, tc apiScenario,
	resp *apiResponse, rec *httptest.ResponseRecorder,
) {
	t.Helper()

	if rec.Code == http.StatusNoContent {
		if rec.Body.Len() != 0 {
			t.Errorf("the %d response has the body %q, want none", rec.Code, rec.Body.String())
		}

		return
	}

	if _, ok := resp.Content["text/markdown"]; ok && strings.HasPrefix(rec.Header().Get("Content-Type"), "text/markdown") {
		requireMediaType(t, rec, markdownContentType)

		if rec.Body.Len() == 0 {
			t.Error("the Markdown response has an empty body")
		}

		return
	}

	content, ok := resp.Content["application/json"]
	if !ok {
		t.Fatalf("the document declares no JSON content for the %d response of %s", rec.Code, tc.operation)
	}

	requireJSONContentType(t, rec)

	if tc.operation == reportsOperation && rec.Code == http.StatusOK {
		// The fakes write fake report bodies; test/apiparity checks the real
		// report against jsonreport's schema, where the real reporters run.
		if rec.Body.Len() == 0 {
			t.Error("the report response has an empty body")
		}

		return
	}

	schemas.ParseSchemaObject(t, content.Schema).ValidateJSON(t, rec.Body.Bytes())
}

// requireEveryDeclaredStatusIsDriven fails when an operation declares a status
// no scenario produces and unreachableStatuses does not explain why.
func requireEveryDeclaredStatusIsDriven(t *testing.T, doc *apiDocument, seen map[string]map[int]bool) {
	t.Helper()

	for _, route := range doc.routes(t) {
		for _, key := range slices.Sorted(maps.Keys(route.op.Responses)) {
			status, err := strconv.Atoi(key)
			if err != nil {
				t.Fatalf("%s declares the response %q, which is not a status code", route.name(), key)
			}

			if seen[route.name()][status] {
				continue
			}

			if strings.TrimSpace(unreachableStatuses[route.name()][status]) != "" {
				continue
			}

			t.Errorf("%s declares %d, which no scenario in apiScenarios produces: add one, "+
				"or say in unreachableStatuses why it cannot be reached", route.name(), status)
		}
	}
}

// TestOpenAPIDocumentsTheEstimateOptions pins that the document describes the two
// request fields PRD #190 milestone 8 adds, regions and estimate, as optional,
// and that the body schema it declares accepts a request that sends them: the
// document is the contract a client reads, so a field the API honours and the
// document does not describe is a contract that fails closed.
func TestOpenAPIDocumentsTheEstimateOptions(t *testing.T) {
	t.Parallel()

	doc, data := loadOpenAPI(t)
	op := doc.operation(t, reportsOperation)

	content, ok := op.RequestBody.Content["application/json"]
	if !ok {
		t.Fatalf("the document declares no JSON request body for %s", reportsOperation)
	}

	// The operation's body schema is a reference to the one the document keeps
	// in components.schemas, as every other body schema of the document is.
	var ref struct {
		Ref string `json:"$ref"`
	}

	if err := json.Unmarshal(content.Schema, &ref); err != nil {
		t.Fatalf("decoding the request body schema %s: %v", content.Schema, err)
	}

	const schemasPrefix = "#/components/schemas/"

	if !strings.HasPrefix(ref.Ref, schemasPrefix) {
		t.Fatalf("the request body schema is %q, want a reference to %s<name>", ref.Ref, schemasPrefix)
	}

	var document struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}

	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decoding the document: %v", err)
	}

	name := strings.TrimPrefix(ref.Ref, schemasPrefix)

	raw, ok := document.Components.Schemas[name]
	if !ok {
		t.Fatalf("the request body schema refers to %q, which components.schemas does not declare", name)
	}

	var body struct {
		Properties map[string]struct {
			Type string `json:"type"`
			// Properties describes the properties of a nested object, such as the
			// regions object's per-target region strings.
			Properties map[string]struct {
				MinLength *int `json:"minLength"`
			} `json:"properties"`
		} `json:"properties"`
		Required []string `json:"required"`
	}

	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decoding components.schemas.%s: %v", name, err)
	}

	if len(body.Properties) == 0 {
		t.Fatalf("components.schemas.%s describes no property", name)
	}

	for _, name := range []string{"regions", "estimate"} {
		property, ok := body.Properties[name]
		if !ok {
			t.Errorf("the request body schema describes no %q, which the API honours", name)

			continue
		}

		if slices.Contains(body.Required, name) {
			t.Errorf("the request body schema requires %q, which a request may leave out", name)
		}

		want := map[string]string{"regions": "object", "estimate": "boolean"}[name]
		if property.Type != want {
			t.Errorf("%q is declared as %q, want %q", name, property.Type, want)
		}
	}

	// A region is at least one character, as assessmentrequest.Decode requires: an
	// empty one would otherwise mean "every region", so a client that thought it
	// had pinned a region would be assessed in all of them (PRD #190 milestone 8,
	// security-audit follow-up L2). Every target's property says so.
	regions := body.Properties["regions"].Properties
	if len(regions) == 0 {
		t.Error("the request body schema describes no property of the regions object")
	}

	for _, name := range slices.Sorted(maps.Keys(regions)) {
		if property := regions[name]; property.MinLength == nil || *property.MinLength != 1 {
			t.Errorf("regions.%s declares minLength %v, want 1: an empty region must not mean every region",
				name, property.MinLength)
		}
	}

	schemas, err := testutil.ParseOpenAPISchemas(data)
	if err != nil {
		t.Fatalf("parsing the document's schemas: %v", err)
	}

	schema := schemas.ParseSchemaObject(t, content.Schema)

	requests := []struct {
		name string
		body string
		// wantMismatches is whether the schema must refuse the body.
		wantMismatches bool
	}{
		{
			name: "both options",
			body: `{"inventory":{"source":"aws","resources":[]},"targets":["scaleway"],` +
				`"regions":{"scaleway":"fr-par"},"estimate":false}`,
		},
		{
			name: "no options",
			body: `{"inventory":{"source":"aws","resources":[]},"targets":["scaleway"]}`,
		},
		{
			name: "an empty regions object",
			body: `{"inventory":{"source":"aws","resources":[]},"targets":["scaleway"],"regions":{}}`,
		},
		{
			// The body is closed: a key the document does not describe is
			// refused, so the two above were accepted because it describes them.
			name:           "a key the document does not describe",
			body:           `{"inventory":{"source":"aws","resources":[]},"targets":["scaleway"],"region":"fr-par"}`,
			wantMismatches: true,
		},
		{
			name:           "a region that is not a string",
			body:           `{"inventory":{"source":"aws","resources":[]},"targets":["scaleway"],"regions":{"scaleway":1}}`,
			wantMismatches: true,
		},
		{
			// The decoder refuses an empty region, so the document does too: a client
			// that reads it never sends one.
			name:           "an empty region",
			body:           `{"inventory":{"source":"aws","resources":[]},"targets":["scaleway"],"regions":{"scaleway":""}}`,
			wantMismatches: true,
		},
		{
			// A region of one character is the shortest one any target could list,
			// and the schema accepts it: the minimum is a length, not a shape.
			name: "a one-character region",
			body: `{"inventory":{"source":"aws","resources":[]},"targets":["scaleway"],` +
				`"regions":{"scaleway":"r"}}`,
		},
	}

	for _, req := range requests {
		t.Run(req.name, func(t *testing.T) {
			t.Parallel()

			mismatches, err := schema.Mismatches([]byte(req.body))
			if err != nil {
				t.Fatalf("validating the request %s: %v", req.body, err)
			}

			if req.wantMismatches != (len(mismatches) > 0) {
				t.Errorf("the request %s has %d mismatches %q, want mismatches = %t",
					req.body, len(mismatches), mismatches, req.wantMismatches)
			}
		})
	}
}
