package httpapi_test

// This file pins the two optional request fields PRD #190 milestone 8 adds to
// POST /v1/assessments, regions and estimate: estimates are on unless the body
// turns them off, a region chosen per target reaches that target's assessment,
// and a region the target does not list, or one named for a target the request
// does not select, is 400 with the error envelope and assesses nothing.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/domain/assessment"
)

// fixtureRegion is the one region every fixture catalog lists and defaults to.
const fixtureRegion = "r1"

// assessOptions are the options of a request body, as the tests below send them.
type assessOptions struct {
	// Regions maps a target key to the region to assess that target in.
	Regions map[string]string `json:"regions,omitempty"`
	// Estimate is the estimate switch; nil leaves it out of the body.
	Estimate *bool `json:"estimate,omitempty"`
}

// bodyWithOptions returns the body of a POST /v1/assessments for targets and
// opts: the documented envelope around inventoryDocument, with the options
// PRD #190 milestone 8 adds.
func bodyWithOptions(t *testing.T, opts assessOptions, targets ...string) string {
	t.Helper()

	body, err := json.Marshal(struct {
		Inventory json.RawMessage `json:"inventory"`
		Targets   []string        `json:"targets"`
		assessOptions
	}{Inventory: json.RawMessage(inventoryDocument), Targets: targets, assessOptions: opts})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	return string(body)
}

// estimateSwitch returns a pointer to v, for a body that states the switch.
func estimateSwitch(v bool) *bool { return &v }

// TestAssessEstimatesByDefault pins the default this milestone makes: a request
// that says nothing about estimates is assessed with them on, in the target's
// default region.
func TestAssessEstimatesByDefault(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	rec := serve(h.handler(t), newAssessRequest(assessBody(t, alphaKey)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/assessments = %d, want %d; body: %q", rec.Code, http.StatusOK, rec.Body.String())
	}

	got := h.jsonReporter.lastAssessment()

	if got.Total == nil {
		t.Error("the assessment holds no estimate total, want estimates on by default (PRD #190)")
	}

	if got.EstimateRegion != fixtureRegion {
		t.Errorf("the estimate region = %q, want the target's default region %q", got.EstimateRegion, fixtureRegion)
	}

	if got.Region != "" {
		t.Errorf("the assessment region = %q, want none: the request chose none", got.Region)
	}
}

// TestAssessEstimateFalse pins "estimate": false: the assessment is made without
// estimates, and no verdict holds one.
func TestAssessEstimateFalse(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	rec := serve(h.handler(t), newAssessRequest(bodyWithOptions(t,
		assessOptions{Estimate: estimateSwitch(false)}, alphaKey)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/assessments = %d, want %d; body: %q", rec.Code, http.StatusOK, rec.Body.String())
	}

	got := h.jsonReporter.lastAssessment()

	if got.Total != nil {
		t.Errorf("the assessment holds the estimate total %v, want none with \"estimate\": false", *got.Total)
	}

	for _, verdict := range got.Verdicts {
		if verdict.Estimate != nil {
			t.Errorf("the verdict on %q holds an estimate, want none with \"estimate\": false", verdict.Resource.ID)
		}
	}
}

// TestAssessRegions pins "regions": the region named for a target is the region
// that target is assessed and estimated in, and a target the body names no
// region for keeps its catalog's default.
func TestAssessRegions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// opts are the request's options.
		opts assessOptions
		// targets are the target keys the request selects.
		targets []string
		// wantRegions is the region each compared target's key must be assessed
		// in, "" for the catalog's default.
		wantRegions map[string]string
		// estimatesOff is whether the request turns estimates off, which leaves
		// the estimate region unset by design: only an estimated assessment has
		// one.
		estimatesOff bool
	}{
		{
			name:        "one target's region",
			opts:        assessOptions{Regions: map[string]string{alphaKey: fixtureRegion}},
			targets:     []string{alphaKey},
			wantRegions: map[string]string{alphaKey: fixtureRegion},
		},
		{
			name:        "one of two targets names a region",
			opts:        assessOptions{Regions: map[string]string{betaKey: fixtureRegion}},
			targets:     []string{alphaKey, betaKey},
			wantRegions: map[string]string{alphaKey: "", betaKey: fixtureRegion},
		},
		{
			name: "both targets name a region, with estimates off",
			opts: assessOptions{
				Regions:  map[string]string{alphaKey: fixtureRegion, betaKey: fixtureRegion},
				Estimate: estimateSwitch(false),
			},
			targets:      []string{alphaKey, betaKey},
			wantRegions:  map[string]string{alphaKey: fixtureRegion, betaKey: fixtureRegion},
			estimatesOff: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)

			rec := serve(h.handler(t), newAssessRequest(bodyWithOptions(t, tc.opts, tc.targets...)))
			if rec.Code != http.StatusOK {
				t.Fatalf("POST /v1/assessments = %d, want %d; body: %q",
					rec.Code, http.StatusOK, rec.Body.String())
			}

			assessed := map[string]assessment.Assessment{
				alphaKey: h.jsonReporter.lastAssessment(),
			}

			if comparison := h.jsonComparison.lastComparison(); len(comparison.Targets) > 0 {
				assessed = make(map[string]assessment.Assessment, len(comparison.Targets))

				for _, target := range comparison.Targets {
					assessed[target.Key] = target.Assessment
				}
			}

			for key, want := range tc.wantRegions {
				got, ok := assessed[key]
				if !ok {
					t.Fatalf("no assessment of the %s target was rendered", key)
				}

				if got.Region != want {
					t.Errorf("the %s assessment region = %q, want %q", key, got.Region, want)
				}

				// The domain sets the estimate region only when estimates are on, so
				// a region chosen with them off shows in the assessment region alone.
				if !tc.estimatesOff && want != "" && got.EstimateRegion != want {
					t.Errorf("the %s estimate region = %q, want %q", key, got.EstimateRegion, want)
				}
			}
		})
	}
}

// TestAssessBadRegion pins the validation the CLI shares: a region the target
// does not list, or a region named for a target the request does not select, is
// 400 with the error envelope, the message names the region and the target's
// regions, and nothing is assessed. The region is echoed only when it is
// region-shaped (ADR 0030): every other value comes back as "the region", and a
// regions key the API does not support is never echoed at all.
func TestAssessBadRegion(t *testing.T) {
	t.Parallel()

	// longRegion is a region-shaped value at the cap, and tooLongRegion one byte
	// past it: the message names the first and not the second.
	longRegion := strings.Repeat("a", 64)
	tests := []struct {
		name string
		// opts are the request's options.
		opts assessOptions
		// targets are the target keys the request selects.
		targets []string
		// want is what the error message must name.
		want []string
		// never are values the error message must not hold: a value the request
		// sent that is not region-shaped, in either its raw or its escaped form.
		never []string
		// rawNever are strings the raw response body must not hold, for a value
		// encoding/json would escape there.
		rawNever []string
	}{
		{
			name:    "a region the target does not list",
			opts:    assessOptions{Regions: map[string]string{alphaKey: "r9"}},
			targets: []string{alphaKey},
			want:    []string{"r9", fixtureRegion},
		},
		{
			name:    "a region for a target the request does not select",
			opts:    assessOptions{Regions: map[string]string{betaKey: fixtureRegion}},
			targets: []string{alphaKey},
			want:    []string{betaKey},
		},
		{
			name:    "a region with markup",
			opts:    assessOptions{Regions: map[string]string{alphaKey: "<b>x</b>"}},
			targets: []string{alphaKey},
			want:    []string{"the region", fixtureRegion},
			never:   []string{"<b>x</b>"},
		},
		{
			// The escaped form too: a message that quoted the value with %q would
			// hold the backslash and the letter, not the line ending.
			name:    "a region with a line ending",
			opts:    assessOptions{Regions: map[string]string{alphaKey: "a\nb"}},
			targets: []string{alphaKey},
			want:    []string{"the region", fixtureRegion},
			never:   []string{"\n", `\n`},
		},
		{
			name:    "a region longer than the cap",
			opts:    assessOptions{Regions: map[string]string{alphaKey: longRegion + "a"}},
			targets: []string{alphaKey},
			want:    []string{"the region", fixtureRegion},
			never:   []string{longRegion + "a"},
		},
		{
			name:    "a region that is not ASCII",
			opts:    assessOptions{Regions: map[string]string{alphaKey: "é"}},
			targets: []string{alphaKey},
			want:    []string{"the region", fixtureRegion},
			never:   []string{"é"},
		},
		{
			name:    "a region-shaped region at the cap is named",
			opts:    assessOptions{Regions: map[string]string{alphaKey: longRegion}},
			targets: []string{alphaKey},
			want:    []string{longRegion, fixtureRegion},
		},
		{
			// The key is client data too, and the API names a regions key only when
			// it is one of its own target keys.
			name:     "a regions key that is not a target",
			opts:     assessOptions{Regions: map[string]string{"<x>": fixtureRegion}},
			targets:  []string{alphaKey},
			want:     []string{alphaKey, "a target that is not supported"},
			never:    []string{"<x>"},
			rawNever: []string{`\u003cx`},
		},
		{
			// A supported-looking key that is no target of the API at all: the
			// message says so in the API's own words and never repeats the key.
			name:    "a regions key that is no supported target",
			opts:    assessOptions{Regions: map[string]string{"aws": fixtureRegion}},
			targets: []string{alphaKey},
			want:    []string{"a target that is not supported", alphaKey},
			never:   []string{"aws"},
		},
		{
			// The comparison path: the region error names the target whose catalog
			// does not list the region, never another selected target.
			name:    "a bad region on one target of a comparison",
			opts:    assessOptions{Regions: map[string]string{betaKey: "r9"}},
			targets: []string{alphaKey, betaKey},
			want:    []string{`the region "r9" for the target "` + betaKey + `"`, fixtureRegion},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)

			rec := serve(h.handler(t), newAssessRequest(bodyWithOptions(t, tc.opts, tc.targets...)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("POST /v1/assessments = %d, want %d; body: %q",
					rec.Code, http.StatusBadRequest, rec.Body.String())
			}

			requireHardeningHeaders(t, rec)
			requireJSONContentType(t, rec)

			var doc struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}

			if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
				t.Fatalf("decoding the error body %q: %v", rec.Body.String(), err)
			}

			if doc.Error.Code != "invalid_region" {
				t.Errorf("the error code = %q, want %q; body: %q", doc.Error.Code, "invalid_region",
					rec.Body.String())
			}

			for _, want := range tc.want {
				if !strings.Contains(doc.Error.Message, want) {
					t.Errorf("the error message = %q, want it to name %q", doc.Error.Message, want)
				}
			}

			for _, never := range tc.never {
				if strings.Contains(doc.Error.Message, never) {
					t.Errorf("the error message = %q, want it not to hold the client's value %q",
						doc.Error.Message, never)
				}
			}

			for _, never := range tc.rawNever {
				if strings.Contains(rec.Body.String(), never) {
					t.Errorf("the response body = %q, want it not to hold %q either", rec.Body.String(), never)
				}
			}

			if strings.ContainsAny(doc.Error.Message, "\r\n") {
				t.Errorf("the error message = %q, want a single line", doc.Error.Message)
			}

			// The request is refused before it is assessed: the inventory is
			// never read and no report is rendered.
			if got := h.inventory.callCount(); got != 0 {
				t.Errorf("the inventory was read %d times, want 0: the region is a request error", got)
			}

			if got := h.jsonReporter.callCount() + h.jsonComparison.callCount(); got != 0 {
				t.Errorf("a report was rendered %d times for a bad region, want 0", got)
			}
		})
	}
}
