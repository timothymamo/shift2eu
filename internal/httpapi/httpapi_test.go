// Package httpapi_test pins the HTTP driving adapter from the outside: its
// routes, content negotiation, target validation, limits and the headers every
// response carries. It builds the handler with httpapi.New over fakes for every
// port, so no adapter, no network and no cloud is involved (PRD #103).
package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/httpapi"
)

// The target keys the fake deps support: lower case and one word, as the
// composition root wires them.
const (
	alphaKey = "alpha"
	betaKey  = "beta"
	gammaKey = "gamma"
	// allKey is the target value that selects every supported target.
	allKey = "all"
)

// The display names of the fixture catalogs, as a report shows them.
const (
	alphaTarget = "Alpha Cloud"
	betaTarget  = "Beta Cloud"
	gammaTarget = "Gamma Cloud"
)

// supportedKeys is every target key the fake deps support, sorted.
var supportedKeys = []string{alphaKey, betaKey, gammaKey}

// The keys of the fakes that name a target or an inventory, used to check that
// a response never echoes them.
const (
	inventoryName = "inventory.json"
	// bearerToken is the token one test configures; no response may hold it.
	bearerToken = "s3cret-token"
)

// The media types the handler answers with.
const (
	jsonContentType     = "application/json"
	markdownContentType = "text/markdown; charset=utf-8"
)

// The two formats the fake deps support, as the fakes name them in the body
// they write.
const (
	jsonFormat     = "json"
	markdownFormat = "markdown"
)

// inventoryDocument is the inventory the fake decoder hands back: the handler
// passes it to the inventory source constructor unchanged.
const inventoryDocument = `{"source":"aws","resources":[]}`

// targetFixture is the catalog one supported target is faked with. The
// retrieval times differ per target, and gamma's is in a zone ahead of UTC, so a
// test can tell the targets' snapshots apart and see that the API dates them in
// UTC.
var targetFixtures = map[string]struct {
	name      string
	sourceURL string
	retrieved time.Time
}{
	alphaKey: {
		name:      alphaTarget,
		sourceURL: "https://alpha.example.com/pricing",
		retrieved: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
	},
	betaKey: {
		name:      betaTarget,
		sourceURL: "https://beta.example.com/pricing",
		retrieved: time.Date(2026, 10, 25, 6, 30, 0, 0, time.UTC),
	},
	gammaKey: {
		name:      gammaTarget,
		sourceURL: "https://gamma.example.com/pricing",
		// Just after midnight CET, so the UTC date is the day before.
		retrieved: time.Date(2027, 1, 1, 0, 30, 0, 0, time.FixedZone("CET", 60*60)),
	},
}

// fixtureCatalog returns the catalog the fake of key returns: one offering, the
// target's display name and its snapshot.
func fixtureCatalog(t *testing.T, key string) catalog.Catalog {
	t.Helper()

	fixture, ok := targetFixtures[key]
	if !ok {
		t.Fatalf("no fixture catalog for target key %q", key)
	}

	cat, err := catalog.New(fixture.name, catalog.Snapshot{
		SourceURL:     fixture.sourceURL,
		RetrievedOn:   fixture.retrieved,
		DefaultRegion: "r1",
	}, []catalog.Offering{{
		Name:       fixture.name + " Virtual Machine",
		Categories: []taxonomy.Category{taxonomy.ComputeVM},
		Regions:    []string{"r1"},
	}})
	if err != nil {
		t.Fatalf("catalog.New() returned unexpected error: %v", err)
	}

	return cat
}

// fixtureDate is the retrieval date the API shows for key, in UTC.
func fixtureDate(key string) string {
	return targetFixtures[key].retrieved.UTC().Format("2006-01-02")
}

// fakeInventorySource is an analysis.InventorySource returning a fixed
// inventory or a fixed error.
type fakeInventorySource struct {
	inv inventory.Inventory
	err error

	mu    sync.Mutex
	calls int
}

func (s *fakeInventorySource) Inventory(ctx context.Context) (inventory.Inventory, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return inventory.Inventory{}, err
	}

	if s.err != nil {
		return inventory.Inventory{}, s.err
	}

	return s.inv, nil
}

func (s *fakeInventorySource) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.calls
}

// fakeCatalogSource is an analysis.CatalogSource returning a fixed catalog or a
// fixed error. errs are returned by the first calls, in order, before err, so a
// test can make a source fail once and then succeed.
type fakeCatalogSource struct {
	cat  catalog.Catalog
	err  error
	errs []error

	mu    sync.Mutex
	calls int
}

func (s *fakeCatalogSource) Catalog(ctx context.Context) (catalog.Catalog, error) {
	s.mu.Lock()
	s.calls++

	err := s.err
	if len(s.errs) > 0 {
		err, s.errs = s.errs[0], s.errs[1:]
	}
	s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return catalog.Catalog{}, err
	}

	if err != nil {
		return catalog.Catalog{}, err
	}

	return s.cat, nil
}

func (s *fakeCatalogSource) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.calls
}

// singleBody is what a reporter of format writes for one target, so a test can
// tell the reporter's output from anything the handler writes itself and can
// see which target and inventory name the reporter was given.
func singleBody(format, target, name string) string {
	return "# " + format + " single target=" + target + " inventory=" + name + "\n"
}

// comparisonBody is what a comparison reporter of format writes for targets, in
// order, so a test can see which targets a comparison covered, in which order,
// and with which inventory name.
func comparisonBody(format string, keys []string, name string) string {
	return "# " + format + " comparison targets=" + strings.Join(keys, ",") + " inventory=" + name + "\n"
}

// comparisonKeys returns the target keys of c, in order.
func comparisonKeys(c assessment.Comparison) []string {
	keys := make([]string, 0, len(c.Targets))
	for _, target := range c.Targets {
		keys = append(keys, target.Key)
	}

	return keys
}

// fakeReporter is an analysis.Reporter that writes a fixed body and then returns
// a fixed error. gate, when set, holds Report until it is closed, and started,
// when set, is closed as Report begins, so a test can hold one report open and
// watch the handler answer another request meanwhile.
type fakeReporter struct {
	format  string
	err     error
	panics  bool
	gate    chan struct{}
	started chan struct{}

	once sync.Once

	mu          sync.Mutex
	calls       int
	metas       []analysis.ReportMeta
	targets     []string
	assessments []assessment.Assessment
}

func (r *fakeReporter) Report(_ context.Context, w io.Writer, a assessment.Assessment,
	meta analysis.ReportMeta,
) error {
	r.mu.Lock()
	r.calls++
	r.metas = append(r.metas, meta)
	r.targets = append(r.targets, a.Target)
	r.assessments = append(r.assessments, a)
	r.mu.Unlock()

	if r.started != nil {
		r.once.Do(func() { close(r.started) })
	}

	if r.gate != nil {
		<-r.gate
	}

	if r.panics {
		panic("MARKER-PANIC-1")
	}

	// The body is written before a failure, as a reporter may write part of its
	// report and then fail: the handler must not let that reach the client.
	if _, err := io.WriteString(w, singleBody(r.format, a.Target, meta.InventoryName)); err != nil {
		return err
	}

	return r.err
}

func (r *fakeReporter) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.calls
}

func (r *fakeReporter) lastMeta() analysis.ReportMeta {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.metas) == 0 {
		return analysis.ReportMeta{}
	}

	return r.metas[len(r.metas)-1]
}

// lastAssessment returns the assessment the last report was rendered for, so a
// test can see the options the handler assessed with.
func (r *fakeReporter) lastAssessment() assessment.Assessment {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.assessments) == 0 {
		return assessment.Assessment{}
	}

	return r.assessments[len(r.assessments)-1]
}

func (r *fakeReporter) targetsSeen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.targets)
}

// fakeComparisonReporter is an analysis.ComparisonReporter that writes one line
// naming the targets it was given, in order, and then returns a fixed error.
type fakeComparisonReporter struct {
	format string
	err    error

	mu         sync.Mutex
	calls      int
	metas      []analysis.ReportMeta
	comparison assessment.Comparison
}

func (r *fakeComparisonReporter) ReportComparison(_ context.Context, w io.Writer, c assessment.Comparison,
	meta analysis.ReportMeta,
) error {
	r.mu.Lock()
	r.calls++
	r.metas = append(r.metas, meta)
	r.comparison = c
	r.mu.Unlock()

	if _, err := io.WriteString(w, comparisonBody(r.format, comparisonKeys(c), meta.InventoryName)); err != nil {
		return err
	}

	return r.err
}

func (r *fakeComparisonReporter) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.calls
}

func (r *fakeComparisonReporter) lastComparison() assessment.Comparison {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.comparison
}

// fakeDecoder stands in for assessmentrequest.Decode: it decodes the tiny
// envelope the API documents with encoding/json and records every body it was
// given and how often it was called. err, when set, is returned instead.
type fakeDecoder struct {
	err error

	mu     sync.Mutex
	calls  int
	bodies [][]byte
}

func (d *fakeDecoder) decode(body []byte) (httpapi.Request, error) {
	d.mu.Lock()
	d.calls++
	d.bodies = append(d.bodies, slices.Clone(body))
	d.mu.Unlock()

	if d.err != nil {
		return httpapi.Request{}, d.err
	}

	// estimate defaults to true, as the real decoder's does (PRD #190).
	envelope := struct {
		Inventory json.RawMessage   `json:"inventory"`
		Targets   []string          `json:"targets"`
		Regions   map[string]string `json:"regions"`
		Estimate  *bool             `json:"estimate"`
	}{Estimate: &[]bool{true}[0]}

	if err := json.Unmarshal(body, &envelope); err != nil {
		return httpapi.Request{}, fmt.Errorf("decode request: %w", err)
	}

	if len(envelope.Inventory) == 0 {
		return httpapi.Request{}, errors.New(`decode request: missing field "inventory"`)
	}

	return httpapi.Request{
		Inventory: envelope.Inventory,
		Targets:   envelope.Targets,
		Regions:   envelope.Regions,
		Estimate:  *envelope.Estimate,
	}, nil
}

func (d *fakeDecoder) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.calls
}

// seenBodies returns every body the decoder was given, in order.
func (d *fakeDecoder) seenBodies() [][]byte {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.bodies)
}

// harness holds the fakes the handler is built over and what they were asked
// for, so each test says what it expects without wiring deps itself.
type harness struct {
	decoder   *fakeDecoder
	inventory *fakeInventorySource
	catalogs  map[string]*fakeCatalogSource

	jsonReporter       *fakeReporter
	jsonComparison     *fakeComparisonReporter
	markdownReporter   *fakeReporter
	markdownComparison *fakeComparisonReporter

	mu   sync.Mutex
	docs [][]byte
	// built is every target key the handler built a catalog source for, in
	// order.
	built []string
}

// newHarness returns a harness whose fakes succeed.
func newHarness(t *testing.T) *harness {
	t.Helper()

	inv, err := inventory.New("aws", []inventory.Resource{{
		ID:       "i-0123456789abcdef0",
		RawType:  "AWS::EC2::Instance",
		Category: taxonomy.ComputeVM,
		Region:   "eu-west-1",
	}})
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	catalogs := make(map[string]*fakeCatalogSource, len(supportedKeys))
	for _, key := range supportedKeys {
		catalogs[key] = &fakeCatalogSource{cat: fixtureCatalog(t, key)}
	}

	return &harness{
		decoder:            &fakeDecoder{},
		inventory:          &fakeInventorySource{inv: inv},
		catalogs:           catalogs,
		jsonReporter:       &fakeReporter{format: jsonFormat},
		jsonComparison:     &fakeComparisonReporter{format: jsonFormat},
		markdownReporter:   &fakeReporter{format: markdownFormat},
		markdownComparison: &fakeComparisonReporter{format: markdownFormat},
	}
}

// deps returns the Deps the handler is built over: one catalog source per
// supported target, both formats with their own reporters, and the decoder and
// inventory source constructor a composition root would pass.
func (h *harness) deps() httpapi.Deps {
	targets := make(map[string]func() analysis.CatalogSource, len(h.catalogs))

	for key, source := range h.catalogs {
		targets[key] = func() analysis.CatalogSource {
			h.mu.Lock()
			h.built = append(h.built, key)
			h.mu.Unlock()

			return source
		}
	}

	return httpapi.Deps{
		DecodeRequest: h.decoder.decode,
		NewInventorySource: func(doc []byte) analysis.InventorySource {
			h.mu.Lock()
			h.docs = append(h.docs, slices.Clone(doc))
			h.mu.Unlock()

			return h.inventory
		},
		Targets: targets,
		JSON: httpapi.Format{
			NewReporter:           func() analysis.Reporter { return h.jsonReporter },
			NewComparisonReporter: func() analysis.ComparisonReporter { return h.jsonComparison },
		},
		Markdown: httpapi.Format{
			NewReporter:           func() analysis.Reporter { return h.markdownReporter },
			NewComparisonReporter: func() analysis.ComparisonReporter { return h.markdownComparison },
		},
	}
}

// builtTargets returns the target keys a catalog source was built for, sorted,
// so a test does not depend on the order the handler builds them in.
func (h *harness) builtTargets() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	built := slices.Clone(h.built)
	slices.Sort(built)

	return built
}

// inventoryDocs returns every inventory document the handler passed to the
// inventory source constructor.
func (h *harness) inventoryDocs() [][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.docs)
}

// mustNew builds the handler and fails the test when New refuses the deps.
func mustNew(t *testing.T, deps httpapi.Deps, opts ...httpapi.Option) http.Handler {
	t.Helper()

	handler, err := httpapi.New(deps, opts...)
	if err != nil {
		t.Fatalf("httpapi.New() returned unexpected error: %v", err)
	}

	if handler == nil {
		t.Fatal("httpapi.New() returned a nil handler")
	}

	return handler
}

// serve runs one request through the handler and returns the recorded response.
func serve(handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

// serveWithin serves req and fails the test when the handler does not answer
// within d, so a request that waits instead of being refused fails the test
// instead of hanging it.
func serveWithin(t *testing.T, handler http.Handler, req *http.Request, d time.Duration) *httptest.ResponseRecorder {
	t.Helper()

	done := make(chan *httptest.ResponseRecorder, 1)

	go func() { done <- serve(handler, req) }()

	select {
	case rec := <-done:
		return rec
	case <-time.After(d):
		t.Fatalf("%s %s did not answer within %s", req.Method, req.URL.Path, d)

		return nil
	}
}

// assessBody returns the body of a POST /v1/assessments for targets, in order:
// the documented envelope around inventoryDocument.
func assessBody(t *testing.T, targets ...string) string {
	t.Helper()

	body, err := json.Marshal(struct {
		Inventory json.RawMessage `json:"inventory"`
		Targets   []string        `json:"targets"`
	}{Inventory: json.RawMessage(inventoryDocument), Targets: targets})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	return string(body)
}

// newRequest returns a request for path with method and no body.
func newRequest(method, path string) *http.Request {
	return httptest.NewRequest(method, path, nil)
}

// newAssessRequest returns a POST /v1/assessments with body and the documented
// Content-Type.
func newAssessRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/assessments", strings.NewReader(body))
	req.Header.Set("Content-Type", jsonContentType)

	return req
}

// withAccept returns a copy of req whose Accept header is value.
func withAccept(req *http.Request, value string) *http.Request {
	req.Header.Set("Accept", value)

	return req
}

// requireHardeningHeaders pins the two headers every response carries, errors
// included.
func requireHardeningHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
}

// requireJSONContentType pins the media type of a JSON response.
func requireJSONContentType(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	requireMediaType(t, rec, jsonContentType)
}

// requireMediaType pins the media type of a response, parameters included.
func requireMediaType(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()

	raw := rec.Header().Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(raw)
	if err != nil {
		t.Fatalf("Content-Type %q is not a media type: %v", raw, err)
	}

	wantType, wantParams, err := mime.ParseMediaType(want)
	if err != nil {
		t.Fatalf("the test's media type %q is not a media type: %v", want, err)
	}

	if mediaType != wantType {
		t.Errorf("Content-Type = %q, want %q", raw, want)

		return
	}

	if diff := cmp.Diff(wantParams, params); diff != "" {
		t.Errorf("Content-Type %q parameters mismatch (-want +got):\n%s", raw, diff)
	}
}

// errorDocument is the documented shape of every error body.
type errorDocument struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// decodeError decodes an error body strictly: the documented shape, exactly the
// documented keys, nothing after the object and the documented JSON media type.
// It returns the message.
func decodeError(t *testing.T, rec *httptest.ResponseRecorder, wantCode string) string {
	t.Helper()

	requireJSONContentType(t, rec)

	body := rec.Body.Bytes()

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()

	var doc errorDocument
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("error body is not the documented JSON shape: %v\nbody: %q", err, body)
	}

	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		t.Fatalf("error body has trailing data after the object (%v): %q", err, body)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatalf("error body is not a JSON object: %v\nbody: %q", err, body)
	}

	if diff := cmp.Diff([]string{"error"}, sortedKeys(top)); diff != "" {
		t.Errorf("error body keys mismatch (-want +got):\n%s", diff)
	}

	var inner map[string]json.RawMessage
	if err := json.Unmarshal(top["error"], &inner); err != nil {
		t.Fatalf(`error body's "error" is not a JSON object: %v\nbody: %q`, err, body)
	}

	if diff := cmp.Diff([]string{"code", "message"}, sortedKeys(inner)); diff != "" {
		t.Errorf("error object keys mismatch (-want +got):\n%s", diff)
	}

	if doc.Error.Code != wantCode {
		t.Errorf("error code = %q, want %q; message: %q", doc.Error.Code, wantCode, doc.Error.Message)
	}

	if strings.TrimSpace(doc.Error.Message) == "" {
		t.Errorf("error message is empty for code %q", doc.Error.Code)
	}

	return doc.Error.Message
}

// strictDecode decodes the body of rec into want, refusing an unknown field and
// data after the JSON value, so a test pins a response's exact keys.
func strictDecode(t *testing.T, rec *httptest.ResponseRecorder, want any) {
	t.Helper()

	dec := json.NewDecoder(rec.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(want); err != nil {
		t.Fatalf("the response body is not the documented JSON: %v\nbody: %q", err, rec.Body.String())
	}

	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		t.Fatalf("the response body has data after the JSON value (%v): %q", err, rec.Body.String())
	}
}

// sortedKeys returns the keys of m, sorted.
func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys
}

// requireNoEcho fails when the response body or any response header value holds
// marker: nothing a client sent may come back.
func requireNoEcho(t *testing.T, rec *httptest.ResponseRecorder, marker string) {
	t.Helper()

	if strings.Contains(rec.Body.String(), marker) {
		t.Errorf("the response body echoes %q: %q", marker, rec.Body.String())
	}

	response := rec.Result()
	defer func() { _ = response.Body.Close() }()

	for name, values := range response.Header {
		for _, value := range values {
			if strings.Contains(value, marker) {
				t.Errorf("the response header %s echoes %q: %q", name, value, marker)
			}
		}
	}
}

// requireNoControlChars fails when msg holds a raw control character, the
// character that would start a forged line or a terminal sequence.
func requireNoControlChars(t *testing.T, msg string) {
	t.Helper()

	for _, r := range msg {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("the message holds the raw control character %U: %q", r, msg)

			return
		}
	}
}

// requireMentionsSupportedTargets fails unless msg names every supported target
// key, in sorted order.
func requireMentionsSupportedTargets(t *testing.T, msg string) {
	t.Helper()

	previous := -1

	for _, key := range supportedKeys {
		i := strings.Index(msg, key)
		if i < 0 {
			t.Errorf("the message %q does not list the supported target %q", msg, key)

			continue
		}

		if i <= previous {
			t.Errorf("the message %q does not list the supported targets in sorted order", msg)
		}

		previous = i
	}
}

// handler builds the handler this harness's deps and opts describe.
func (h *harness) handler(t *testing.T, opts ...httpapi.Option) http.Handler {
	t.Helper()

	return mustNew(t, h.deps(), opts...)
}
