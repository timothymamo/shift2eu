package httpapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/report/jsonreport"
	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/adapter/request/assessmentrequest"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/httpapi"
)

// TestAssessOneTarget pins the single-target path of POST /v1/assessments: one
// target runs the single-target reporter, the report names the inventory
// inventory.json because a request body carries no file name, and the response
// is that reporter's output byte for byte as application/json.
func TestAssessOneTarget(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	handler := h.handler(t)

	rec := serve(handler, newAssessRequest(assessBody(t, alphaKey)))

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/assessments = %d, want %d; body: %q", rec.Code, http.StatusOK, rec.Body.String())
	}

	requireHardeningHeaders(t, rec)
	requireJSONContentType(t, rec)

	want := singleBody(jsonFormat, alphaTarget, inventoryName)
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want the JSON reporter's output %q", got, want)
	}

	if got := h.jsonReporter.callCount(); got != 1 {
		t.Errorf("the JSON reporter ran %d times, want 1", got)
	}

	for name, got := range map[string]int{
		"the JSON comparison reporter":     h.jsonComparison.callCount(),
		"the Markdown reporter":            h.markdownReporter.callCount(),
		"the Markdown comparison reporter": h.markdownComparison.callCount(),
	} {
		if got != 0 {
			t.Errorf("%s ran %d times for a one-target run, want 0", name, got)
		}
	}

	if got := h.jsonReporter.lastMeta().InventoryName; got != inventoryName {
		t.Errorf("ReportMeta.InventoryName = %q, want %q", got, inventoryName)
	}

	if diff := cmp.Diff([]string{alphaTarget}, h.jsonReporter.targetsSeen()); diff != "" {
		t.Errorf("the reporter was given the targets (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff([]string{alphaKey}, h.builtTargets()); diff != "" {
		t.Errorf("catalog sources built (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff([][]byte{[]byte(inventoryDocument)}, h.inventoryDocs()); diff != "" {
		t.Errorf("the inventory source was built over the docs (-want +got):\n%s", diff)
	}
}

// TestAssessSeveralTargets pins that a target list runs the comparison, in
// sorted key order, whatever order the client sent.
func TestAssessSeveralTargets(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	handler := h.handler(t)

	rec := serve(handler, newAssessRequest(assessBody(t, gammaKey, alphaKey)))

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/assessments = %d, want %d; body: %q", rec.Code, http.StatusOK, rec.Body.String())
	}

	requireJSONContentType(t, rec)

	wantKeys := []string{alphaKey, gammaKey}
	want := comparisonBody(jsonFormat, wantKeys, inventoryName)
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want the comparison reporter's output %q", got, want)
	}

	if got := h.jsonComparison.callCount(); got != 1 {
		t.Errorf("the JSON comparison reporter ran %d times, want 1", got)
	}

	if got := h.jsonReporter.callCount(); got != 0 {
		t.Errorf("the JSON single-target reporter ran %d times for a two-target run, want 0", got)
	}

	if diff := cmp.Diff(wantKeys, comparisonKeys(h.jsonComparison.lastComparison())); diff != "" {
		t.Errorf("the compared targets (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(wantKeys, h.builtTargets()); diff != "" {
		t.Errorf("catalog sources built (-want +got):\n%s", diff)
	}
}

// TestAssessAllTargets pins that "all" compares every supported target, in
// sorted key order.
func TestAssessAllTargets(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	handler := h.handler(t)

	rec := serve(handler, newAssessRequest(assessBody(t, allKey)))

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/assessments = %d, want %d; body: %q", rec.Code, http.StatusOK, rec.Body.String())
	}

	want := comparisonBody(jsonFormat, supportedKeys, inventoryName)
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want the comparison reporter's output %q", got, want)
	}

	if diff := cmp.Diff(supportedKeys, comparisonKeys(h.jsonComparison.lastComparison())); diff != "" {
		t.Errorf("the compared targets (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(supportedKeys, h.builtTargets()); diff != "" {
		t.Errorf("catalog sources built (-want +got):\n%s", diff)
	}
}

// pinAccept pins the content negotiation of POST /v1/assessments: the Accept
// header picks the representation, q=0 excludes one, and nothing acceptable is
// 406.
func TestAcceptNegotiation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// accept is the Accept header; an empty one sets no header.
		accept          string
		wantStatus      int
		wantCode        string
		wantContentType string
		wantBody        string
	}{
		{
			name:            "no Accept header",
			wantStatus:      http.StatusOK,
			wantContentType: jsonContentType,
			wantBody:        singleBody(jsonFormat, alphaTarget, inventoryName),
		},
		{
			name:            "any type",
			accept:          "*/*",
			wantStatus:      http.StatusOK,
			wantContentType: jsonContentType,
			wantBody:        singleBody(jsonFormat, alphaTarget, inventoryName),
		},
		{
			name:            "application/json",
			accept:          "application/json",
			wantStatus:      http.StatusOK,
			wantContentType: jsonContentType,
			wantBody:        singleBody(jsonFormat, alphaTarget, inventoryName),
		},
		{
			name:            "any application type",
			accept:          "application/*",
			wantStatus:      http.StatusOK,
			wantContentType: jsonContentType,
			wantBody:        singleBody(jsonFormat, alphaTarget, inventoryName),
		},
		{
			name:            "text/markdown",
			accept:          "text/markdown",
			wantStatus:      http.StatusOK,
			wantContentType: markdownContentType,
			wantBody:        singleBody(markdownFormat, alphaTarget, inventoryName),
		},
		{
			name:            "any text type",
			accept:          "text/*",
			wantStatus:      http.StatusOK,
			wantContentType: markdownContentType,
			wantBody:        singleBody(markdownFormat, alphaTarget, inventoryName),
		},
		{
			name:            "markdown excluded by q=0",
			accept:          "text/markdown;q=0, application/json",
			wantStatus:      http.StatusOK,
			wantContentType: jsonContentType,
			wantBody:        singleBody(jsonFormat, alphaTarget, inventoryName),
		},
		{
			name:       "nothing acceptable",
			accept:     "text/html",
			wantStatus: http.StatusNotAcceptable,
			wantCode:   "not_acceptable",
		},
		// An entry whose q is not a number in [0, 1] is invalid on its own and
		// skipped: it neither excludes the entry nor spoils the rest of the header.
		// NaN is the trap, because strconv.ParseFloat parses it without an error.
		{
			name:            "a NaN q beside Markdown",
			accept:          "application/json;q=NaN, text/markdown",
			wantStatus:      http.StatusOK,
			wantContentType: markdownContentType,
			wantBody:        singleBody(markdownFormat, alphaTarget, inventoryName),
		},
		{
			name:            "a NaN q on Markdown beside a lower JSON q",
			accept:          "text/markdown;q=NaN, application/json;q=0.5",
			wantStatus:      http.StatusOK,
			wantContentType: jsonContentType,
			wantBody:        singleBody(jsonFormat, alphaTarget, inventoryName),
		},
		{
			name:       "a q above 1",
			accept:     "text/markdown;q=2",
			wantStatus: http.StatusNotAcceptable,
			wantCode:   "not_acceptable",
		},
		{
			name:            "a q above 1 beside JSON",
			accept:          "text/markdown;q=2, application/json;q=0.5",
			wantStatus:      http.StatusOK,
			wantContentType: jsonContentType,
			wantBody:        singleBody(jsonFormat, alphaTarget, inventoryName),
		},
		{
			name:            "a negative q beside Markdown",
			accept:          "application/json;q=-1, text/markdown;q=0.5",
			wantStatus:      http.StatusOK,
			wantContentType: markdownContentType,
			wantBody:        singleBody(markdownFormat, alphaTarget, inventoryName),
		},
		{
			name:            "a q that is no number beside Markdown",
			accept:          "application/json;q=abc, text/markdown",
			wantStatus:      http.StatusOK,
			wantContentType: markdownContentType,
			wantBody:        singleBody(markdownFormat, alphaTarget, inventoryName),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			handler := h.handler(t)

			req := newAssessRequest(assessBody(t, alphaKey))
			if tc.accept != "" {
				withAccept(req, tc.accept)
			}

			rec := serve(handler, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("%s: status = %d, want %d; body: %q", tc.name, rec.Code, tc.wantStatus, rec.Body.String())
			}

			requireHardeningHeaders(t, rec)

			if tc.wantCode != "" {
				decodeError(t, rec, tc.wantCode)
				requireMediaType(t, rec, jsonContentType)

				return
			}

			requireMediaType(t, rec, tc.wantContentType)

			if got := rec.Body.String(); got != tc.wantBody {
				t.Errorf("body = %q, want %q", got, tc.wantBody)
			}
		})
	}
}

// TestContentType pins which request media types a POST body is read for.
func TestContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// contentType is the request Content-Type; an empty one sets no header.
		contentType string
		wantStatus  int
		wantCode    string
	}{
		{
			name:        "application/json with a charset",
			contentType: "application/json; charset=utf-8",
			wantStatus:  http.StatusOK,
		},
		{
			name:        "application/json with an upper-case charset",
			contentType: "application/json; charset=UTF-8",
			wantStatus:  http.StatusOK,
		},
		{
			name:        "application/json with a quoted charset",
			contentType: `application/json; charset="utf-8"`,
			wantStatus:  http.StatusOK,
		},
		// Only UTF-8 is JSON text the decoder may read: another charset is a body
		// this API cannot decode, however valid the media type is.
		{
			name:        "application/json in latin-1",
			contentType: "application/json; charset=iso-8859-1",
			wantStatus:  http.StatusUnsupportedMediaType,
			wantCode:    "unsupported_media_type",
		},
		{
			name:        "application/json in utf-16",
			contentType: "application/json; charset=utf-16",
			wantStatus:  http.StatusUnsupportedMediaType,
			wantCode:    "unsupported_media_type",
		},
		{
			name:        "application/json",
			contentType: "application/json",
			wantStatus:  http.StatusOK,
		},
		{
			name:       "no Content-Type",
			wantStatus: http.StatusUnsupportedMediaType,
			wantCode:   "unsupported_media_type",
		},
		{
			name:        "text/plain",
			contentType: "text/plain",
			wantStatus:  http.StatusUnsupportedMediaType,
			wantCode:    "unsupported_media_type",
		},
		{
			name:        "a form post",
			contentType: "application/x-www-form-urlencoded",
			wantStatus:  http.StatusUnsupportedMediaType,
			wantCode:    "unsupported_media_type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			handler := h.handler(t)

			req := newAssessRequest(assessBody(t, alphaKey))
			req.Header.Del("Content-Type")
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}

			rec := serve(handler, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("POST /v1/assessments = %d, want %d; body: %q", rec.Code, tc.wantStatus, rec.Body.String())
			}

			requireHardeningHeaders(t, rec)

			if tc.wantCode != "" {
				decodeError(t, rec, tc.wantCode)

				if got := h.decoder.callCount(); got != 0 {
					t.Errorf("the request decoder ran %d times for a body that was never read, want 0", got)
				}

				return
			}

			if got := h.decoder.callCount(); got != 1 {
				t.Errorf("the request decoder ran %d times, want 1", got)
			}
		})
	}
}

// TestBodyTooLarge pins the body cap: a body over the limit is refused with 413
// before the decoder ever sees it, however the request states its length. The
// check against ContentLength is only a shortcut that saves reading; the cap
// that holds is http.MaxBytesReader's, which counts the bytes actually read, so
// a chunked body (no length at all) and a body that declares a small length are
// both refused too, and the decoder is never handed a byte past the cap.
func TestBodyTooLarge(t *testing.T) {
	t.Parallel()

	const maxBody = 64

	body := assessBody(t, alphaKey)
	if len(body) <= maxBody {
		t.Fatalf("the test body is %d bytes, not over the %d-byte cap", len(body), maxBody)
	}

	// small is a valid request under the cap, so only a declared length can
	// take the shortcut to 413 for it.
	small := `{"inventory":{},"targets":["` + alphaKey + `"]}`

	if len(small) > maxBody {
		t.Fatalf("the small test body is %d bytes, not under the %d-byte cap", len(small), maxBody)
	}

	tests := []struct {
		name string
		// request builds the request a fresh handler is sent.
		request func(t *testing.T) *http.Request
	}{
		{
			name:    "an accurate ContentLength",
			request: func(t *testing.T) *http.Request { t.Helper(); return newAssessRequest(body) },
		},
		{
			name: "a chunked body",
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return assessRequestWithLength(t, body, -1)
			},
		},
		{
			name: "a ContentLength below the body's length",
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return assessRequestWithLength(t, body, 10)
			},
		},
		{
			name: "a ContentLength over the cap for a small body",
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return assessRequestWithLength(t, small, 1<<20)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			handler := h.handler(t, httpapi.WithMaxBodyBytes(maxBody))

			rec := serve(handler, tc.request(t))

			// Every case is 413: http.MaxBytesReader reports the cap it hit, and
			// the handler reads through it whatever ContentLength claims.
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("POST /v1/assessments = %d, want %d; body: %q",
					rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
			}

			requireHardeningHeaders(t, rec)
			decodeError(t, rec, "payload_too_large")

			if got := h.decoder.callCount(); got != 0 {
				t.Errorf("the request decoder ran %d times for an oversized body, want 0", got)
			}

			for _, seen := range h.decoder.seenBodies() {
				if len(seen) > maxBody {
					t.Errorf("the decoder was given a %d-byte body, over the %d-byte cap", len(seen), maxBody)
				}
			}
		})
	}
}

// assessRequestWithLength returns a POST /v1/assessments whose body is body and
// whose declared ContentLength is contentLength, -1 being a chunked body that
// declares none.
func assessRequestWithLength(t *testing.T, body string, contentLength int64) *http.Request {
	t.Helper()

	req := newAssessRequest(body)
	req.ContentLength = contentLength

	return req
}

// jsonDocumentDeps returns the harness's deps with the real request decoder and
// the real JSON reporters, so a test sees the document the API ships rather than
// the line a fake writes: only the catalogs and the inventory source stay fakes.
func jsonDocumentDeps(t *testing.T, h *harness) httpapi.Deps {
	t.Helper()

	deps := h.deps()
	deps.DecodeRequest = decodeRequest
	deps.JSON = httpapi.Format{
		NewReporter:           func(schemaVersion int) analysis.Reporter { return jsonreport.New(schemaVersion) },
		NewComparisonReporter: func(schemaVersion int) analysis.ComparisonReporter { return jsonreport.New(schemaVersion) },
	}

	return deps
}

// markdownDocumentDeps returns the harness's deps with the real request decoder
// and the real Markdown reporter, so a test compares the bytes text/markdown
// answers with.
func markdownDocumentDeps(t *testing.T, h *harness) httpapi.Deps {
	t.Helper()

	deps := h.deps()
	deps.DecodeRequest = decodeRequest
	deps.Markdown = httpapi.Format{
		NewReporter:           func(int) analysis.Reporter { return markdown.New() },
		NewComparisonReporter: func(int) analysis.ComparisonReporter { return markdown.New() },
	}

	return deps
}

// jsonResponse returns the body of the application/json answer to body, through
// the real request decoder and the real JSON reporters, and fails the test when
// the answer is not 200.
func jsonResponse(t *testing.T, body string) string {
	t.Helper()

	h := newHarness(t)
	rec := serve(mustNew(t, jsonDocumentDeps(t, h)), newAssessRequest(body))

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/assessments = %d, want %d; body: %q", rec.Code, http.StatusOK, rec.Body.String())
	}

	requireJSONContentType(t, rec)

	return rec.Body.String()
}

// markdownResponse returns the body of the text/markdown answer to body, through
// the real request decoder and the real Markdown reporter, and fails the test
// when the answer is not 200.
func markdownResponse(t *testing.T, body string) string {
	t.Helper()

	h := newHarness(t)
	rec := serve(mustNew(t, markdownDocumentDeps(t, h)), withAccept(newAssessRequest(body), "text/markdown"))

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/assessments = %d, want %d; body: %q", rec.Code, http.StatusOK, rec.Body.String())
	}

	requireMediaType(t, rec, markdownContentType)

	return rec.Body.String()
}

// decodeRequest decodes body with the real decoder and converts the result, as
// cmd/shift2eu-server's DecodeRequest does.
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

// assessBodySchemaVersion returns a POST /v1/assessments body for targets whose
// schemaVersion member holds value, or no member at all when value is nil and
// omit is true.
func assessBodySchemaVersion(t *testing.T, value any, targets ...string) string {
	t.Helper()

	body, err := json.Marshal(struct {
		Inventory     json.RawMessage `json:"inventory"`
		Targets       []string        `json:"targets"`
		SchemaVersion any             `json:"schemaVersion"`
	}{Inventory: json.RawMessage(inventoryDocument), Targets: targets, SchemaVersion: value})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	return string(body)
}

// TestAssessSchemaVersion pins the opt-in version of a response (docs/adr/0018,
// docs/adr/0030, issue #307): a body that names no schemaVersion answers version
// 2, which is what every client sent before the key existed, and a body that
// names 2 or 3 answers that version. The fixture estimates nothing, so a version
// 3 document writes no amount for its total while version 2 writes "0.00".
func TestAssessSchemaVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want int
		// wantAmount is whether the estimate's total writes an amount.
		wantAmount bool
	}{
		{
			name:       "no schemaVersion is version 2",
			body:       assessBody(t, alphaKey),
			want:       2,
			wantAmount: true,
		},
		{
			name:       "version 2 named",
			body:       assessBodySchemaVersion(t, 2, alphaKey),
			want:       2,
			wantAmount: true,
		},
		{
			name: "version 3 named",
			body: assessBodySchemaVersion(t, 3, alphaKey),
			want: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			handler := mustNew(t, jsonDocumentDeps(t, h))

			rec := serve(handler, newAssessRequest(tc.body))

			if rec.Code != http.StatusOK {
				t.Fatalf("POST /v1/assessments = %d, want %d; body: %q", rec.Code, http.StatusOK, rec.Body.String())
			}

			requireJSONContentType(t, rec)

			var document map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &document); err != nil {
				t.Fatalf("the response is not a JSON document: %v", err)
			}

			if got := document["schemaVersion"]; got != float64(tc.want) {
				t.Errorf("schemaVersion = %v, want %d", got, tc.want)
			}

			estimate, ok := document["estimate"].(map[string]any)
			if !ok {
				t.Fatalf("the document holds no estimate object: %v", document["estimate"])
			}

			total, ok := estimate["total"].(map[string]any)
			if !ok {
				t.Fatalf("the estimate holds no total object: %v", estimate["total"])
			}

			_, hasAmount := total["amount"]
			if hasAmount != tc.wantAmount {
				t.Errorf("estimate.total holds an amount = %t, want %t: version 3 writes no amount for a total "+
					"that estimated nothing (issue #307)", hasAmount, tc.wantAmount)
			}

			if !tc.wantAmount {
				if got := total["estimated"]; got != float64(0) {
					t.Errorf("estimate.total.estimated = %v, want 0 without an amount", got)
				}

				if _, ok := total["resources"]; !ok {
					t.Errorf("estimate.total = %v, want its counts kept beside the absent amount", total)
				}
			}
		})
	}
}

// TestAssessRefusesASchemaVersionItCannotWrite pins the refusal of a schemaVersion
// the format does not write (docs/adr/0030, issue #307): version 1 is frozen and
// version 4 does not exist, and a value of any other JSON type is refused too. The
// answer is 400 invalid_request, and the message never repeats a value the client
// sent.
func TestAssessRefusesASchemaVersionItCannotWrite(t *testing.T) {
	t.Parallel()

	const marker = "MARKER-VERSION-7"

	tests := []struct {
		name  string
		value any
		// echoes is the value the message must not hold, if any.
		echoes string
	}{
		{name: "version 1", value: 1},
		{name: "version 4", value: 4},
		{name: "a fraction", value: 2.5},
		{name: "a string", value: marker, echoes: marker},
		{name: "null", value: nil},
		{name: "a boolean", value: true},
		{name: "an object", value: map[string]int{"version": 3}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			handler := mustNew(t, jsonDocumentDeps(t, h))

			rec := serve(handler, newAssessRequest(assessBodySchemaVersion(t, tc.value, alphaKey)))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("POST /v1/assessments = %d, want %d; body: %q",
					rec.Code, http.StatusBadRequest, rec.Body.String())
			}

			requireHardeningHeaders(t, rec)

			message := decodeError(t, rec, "invalid_request")

			if tc.echoes != "" && strings.Contains(message, tc.echoes) {
				t.Errorf("the error message %q repeats the client's value %q", message, tc.echoes)
			}
		})
	}
}

// TestAssessForwardsTheRequestedSchemaVersion pins that the handler builds the
// reporter of the version the body asks for (issue #307): version 2 when it names
// none, and the named version for both the single-target and the comparison path.
func TestAssessForwardsTheRequestedSchemaVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		body   string
		want   int
		single bool
	}{
		{
			name:   "no version names version 2",
			body:   assessBody(t, alphaKey),
			want:   2,
			single: true,
		},
		{
			name: "version 3 for one target",
			body: assessBodySchemaVersion(t, 3, alphaKey),
			want: 3,

			single: true,
		},
		{
			name: "version 3 for several targets",
			body: assessBodySchemaVersion(t, 3, alphaKey, betaKey),
			want: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			handler := h.handler(t)

			rec := serve(handler, newAssessRequest(tc.body))

			if rec.Code != http.StatusOK {
				t.Fatalf("POST /v1/assessments = %d, want %d; body: %q", rec.Code, http.StatusOK, rec.Body.String())
			}

			if tc.single {
				if got := h.jsonReporter.builtSchemaVersion(); got != tc.want {
					t.Errorf("the JSON reporter was built for schemaVersion %d, want %d", got, tc.want)
				}

				return
			}

			if got := h.jsonComparison.builtSchemaVersion(); got != tc.want {
				t.Errorf("the JSON comparison reporter was built for schemaVersion %d, want %d", got, tc.want)
			}
		})
	}
}

// TestAssessMarkdownIgnoresSchemaVersion pins that text/markdown answers the same
// bytes whatever schemaVersion the body names (docs/adr/0018, docs/adr/0030,
// issue #307): the Markdown report has no schemaVersion, so the handler passes
// the version only to the JSON reporter, and a body naming 3 answers what a body
// naming none answers.
func TestAssessMarkdownIgnoresSchemaVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		targets []string
	}{
		{name: "one target", targets: []string{alphaKey}},
		{name: "several targets", targets: []string{alphaKey, betaKey}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			without := markdownResponse(t, assessBody(t, tc.targets...))
			withVersion := markdownResponse(t, assessBodySchemaVersion(t, 3, tc.targets...))

			if diff := cmp.Diff(without, withVersion); diff != "" {
				t.Errorf("the Markdown answer to a body naming schemaVersion 3 differs from the body naming none "+
					"(-without +with):\n%s", diff)
			}
		})
	}
}

// TestAssessTreatsAZeroSchemaVersionAsVersion2 pins the default for a Request
// whose SchemaVersion is 0 (docs/adr/0030, issue #307): the decoder never
// produces one, but a Deps wrapping it can, and the handler must answer the
// version 2 document a body naming none gets, not a 500 from a reporter built for
// a version the format does not write.
func TestAssessTreatsAZeroSchemaVersionAsVersion2(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		targets []string
	}{
		{name: "one target", targets: []string{alphaKey}},
		{name: "several targets", targets: []string{alphaKey, betaKey}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)

			deps := jsonDocumentDeps(t, h)
			deps.DecodeRequest = func(body []byte) (httpapi.Request, error) {
				req, err := decodeRequest(body)
				if err != nil {
					return httpapi.Request{}, err
				}

				req.SchemaVersion = 0

				return req, nil
			}

			body := assessBody(t, tc.targets...)
			rec := serve(mustNew(t, deps), newAssessRequest(body))

			if rec.Code != http.StatusOK {
				t.Fatalf("POST /v1/assessments for a Request with SchemaVersion 0 = %d, want %d; body: %q",
					rec.Code, http.StatusOK, rec.Body.String())
			}

			requireJSONContentType(t, rec)

			want := jsonResponse(t, body)

			if diff := cmp.Diff(want, rec.Body.String()); diff != "" {
				t.Errorf("the document for a Request with SchemaVersion 0 differs from the version 2 document "+
					"(-want +got):\n%s", diff)
			}
		})
	}
}

// TestDecodeError pins that a body the decoder refuses is 400 invalid_request.
func TestDecodeError(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.decoder.err = errors.New(`decode request: missing field "targets"`)
	handler := h.handler(t)

	rec := serve(handler, newAssessRequest(`{"inventory":{}}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /v1/assessments = %d, want %d; body: %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	requireHardeningHeaders(t, rec)
	decodeError(t, rec, "invalid_request")

	if got := h.inventory.callCount(); got != 0 {
		t.Errorf("the inventory source ran %d times after a decode error, want 0", got)
	}
}

// TestInvalidTargets pins the CLI's target rules on the API: an unknown, repeated
// or empty key, or "all" beside another key, is 400 invalid_targets, and the
// message lists the supported targets without echoing what the client sent.
func TestInvalidTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		targets []string
		// marker is a value the message must not echo, if any.
		marker string
	}{
		{name: "unknown target", targets: []string{"MARKER-T-77"}, marker: "MARKER-T-77"},
		{name: "repeated target", targets: []string{alphaKey, alphaKey}},
		{name: "empty target", targets: []string{""}},
		{name: "empty target beside a known one", targets: []string{alphaKey, ""}},
		{name: "all beside another target", targets: []string{allKey, alphaKey}},
		{name: "a target beside all", targets: []string{betaKey, allKey}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			handler := h.handler(t)

			rec := serve(handler, newAssessRequest(assessBody(t, tc.targets...)))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("POST /v1/assessments = %d, want %d; body: %q",
					rec.Code, http.StatusBadRequest, rec.Body.String())
			}

			requireHardeningHeaders(t, rec)

			message := decodeError(t, rec, "invalid_targets")
			requireMentionsSupportedTargets(t, message)

			if tc.marker != "" {
				requireNoEcho(t, rec, tc.marker)
			}

			if got := h.jsonReporter.callCount() + h.jsonComparison.callCount(); got != 0 {
				t.Errorf("a reporter ran %d times for an invalid target list, want 0", got)
			}
		})
	}
}

// TestInvalidInventory pins that an inventory the reader refuses is 400
// invalid_inventory, and that its message reaches the client as one escaped
// line: a control character in it can neither start a forged line nor a
// terminal sequence.
func TestInvalidInventory(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.inventory.err = errors.New("inventory: cannot read MARKER-I-5\nsecond line\x1b[31m in red")
	handler := h.handler(t)

	rec := serve(handler, newAssessRequest(assessBody(t, alphaKey)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /v1/assessments = %d, want %d; body: %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	requireHardeningHeaders(t, rec)

	message := decodeError(t, rec, "invalid_inventory")
	requireNoControlChars(t, message)

	if !strings.Contains(message, `\n`) {
		t.Errorf("the message %q does not write the line feed as an escape", message)
	}

	if !strings.Contains(message, `\x1b`) {
		t.Errorf("the message %q does not write the escape character as an escape", message)
	}

	if got := h.jsonReporter.callCount(); got != 0 {
		t.Errorf("the reporter ran %d times for an invalid inventory, want 0", got)
	}
}

// TestInternalErrors pins that anything that goes wrong beyond the client's
// control is 500 internal, with the fixed message and nothing internal in it,
// and that a partial report never reaches the client.
func TestInternalErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// configure changes the fakes of the fresh harness.
		configure func(*harness)
		// marker is the failure's own text, which no response may hold.
		marker string
		// forbidden is text that shows part of a report leaked into the body.
		forbidden string
	}{
		{
			name: "a failing catalog",
			configure: func(h *harness) {
				h.catalogs[alphaKey].err = errors.New("catalog: MARKER-C-3")
			},
			marker: "MARKER-C-3",
		},
		{
			name: "a failing reporter",
			configure: func(h *harness) {
				h.jsonReporter.err = errors.New("render: MARKER-R-4")
			},
			marker:    "MARKER-R-4",
			forbidden: "# json single",
		},
		{
			name: "a panicking reporter",
			configure: func(h *harness) {
				h.jsonReporter.panics = true
			},
			marker: "MARKER-PANIC-1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			tc.configure(h)
			handler := h.handler(t)

			rec := serve(handler, newAssessRequest(assessBody(t, alphaKey)))

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("POST /v1/assessments = %d, want %d; body: %q",
					rec.Code, http.StatusInternalServerError, rec.Body.String())
			}

			requireHardeningHeaders(t, rec)

			message := decodeError(t, rec, "internal")
			if message != "internal error" {
				t.Errorf("the 500 message = %q, want %q", message, "internal error")
			}

			requireNoEcho(t, rec, tc.marker)

			if tc.forbidden != "" && strings.Contains(rec.Body.String(), tc.forbidden) {
				t.Errorf("the 500 body holds part of a report: %q", rec.Body.String())
			}
		})
	}
}
