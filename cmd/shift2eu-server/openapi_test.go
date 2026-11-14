// The API document and the targets this server serves: the document closes the
// regions object of POST /v1/assessments (additionalProperties false) and lists
// one property per target key, so a client that validates its request against the
// document would refuse a region for a target the document does not list. These
// tests read the document from the repository, where the composition root can
// compare its property names with the target keys this binary wires (PRD #190
// milestone 8 review finding 4).
package main

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// repoRoot is the module root, as the package directory go test runs in is
// cmd/shift2eu-server.
const repoRoot = "../.."

// openAPIPath is the API document (OpenAPI 3.1, ADR 0019), relative to the root:
// the document test/openapibundle bundles into api/openapi.bundled.json.
const openAPIPath = "api/openapi.json"

// requestSchemaName is the name components.schemas gives the body of
// POST /v1/assessments.
const requestSchemaName = "AssessmentRequest"

// TestOpenAPIRegionKeysAreTheServerTargets pins that the regions object of the
// request schema lists exactly the target keys this server wires: the object is
// closed, so a fourth target, or a target it names that the server does not
// serve, would make the document refuse a valid request or accept an impossible
// one, and no other test would notice.
func TestOpenAPIRegionKeysAreTheServerTargets(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repoRoot, openAPIPath))
	if err != nil {
		t.Fatalf("reading %s: %v", openAPIPath, err)
	}

	var document struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}

	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decoding %s: %v", openAPIPath, err)
	}

	request, ok := document.Components.Schemas[requestSchemaName]
	if !ok {
		t.Fatalf("%s declares no components.schemas.%s", openAPIPath, requestSchemaName)
	}

	regions, ok := request.Properties["regions"]
	if !ok {
		t.Fatalf("components.schemas.%s describes no property \"regions\"", requestSchemaName)
	}

	got := slices.Sorted(maps.Keys(regions.Properties))

	want := slices.Sorted(maps.Keys(deps().Targets))
	if len(want) == 0 {
		t.Fatal("deps().Targets wires no target, so nothing was compared")
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the keys of components.schemas.%s.properties.regions.properties are not the target keys "+
			"this server serves, so the document and the binary have drifted apart (-want +got):\n%s",
			requestSchemaName, diff)
	}
}
