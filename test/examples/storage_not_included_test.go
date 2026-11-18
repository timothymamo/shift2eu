package examples_test

// This file pins the acceptance of issue #226 through the real OVHcloud
// snapshot (ADR 0032): an OVHcloud Gen 3 instance plan includes no storage, so a
// VM that gives no disk size is estimated with the note that its boot volume is
// left out, and a VM that gives one has that storage priced and carries no note.
// The samples in examples/ hold no Gen 3 VM with a disk size, so the fixture in
// testdata/boot-volume/inventory.json supplies a pair that differs only in that.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/jsonreport"
	"github.com/timothymamo/shift2eu/internal/adapter/target/ovhcloud"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
)

// reportedPlan is the plan the JSON report names for a resource, decoded from the
// document's own keys.
type reportedPlan struct {
	Name               string `json:"name"`
	IncludedStorageGiB *int64 `json:"includedStorageGiB"`
}

// reportedEstimate is the estimate the JSON report carries for a resource.
type reportedEstimate struct {
	Estimated          *bool  `json:"estimated"`
	StorageNotIncluded string `json:"storageNotIncluded"`
	Breakdown          *struct {
		Plan    string `json:"plan"`
		Storage string `json:"storage"`
		Fee     string `json:"fixedFee"`
	} `json:"breakdown"`
}

// reportedResource is one entry of the JSON report's "resources".
type reportedResource struct {
	ID       string            `json:"id"`
	Plan     *reportedPlan     `json:"plan"`
	Estimate *reportedEstimate `json:"estimate"`
}

// TestStorageNotIncludedThroughTheOvhcloudSnapshot pins the note an estimate by
// plan carries through the real OVHcloud catalog, not a test catalog: both VMs of
// the fixture are 2 vCPU, 8 GiB, so both land on an OVHcloud Gen 3 plan whose
// included storage is 0, and only the VM that gives no disk size leaves its boot
// volume out of the estimate. It reads the JSON report, the document the CLI and
// the HTTP API serve, so the assertion is on what a consumer of the report sees.
func TestStorageNotIncludedThroughTheOvhcloudSnapshot(t *testing.T) {
	t.Parallel()

	inventoryPath := filepath.Join("testdata", "boot-volume", "inventory.json")

	rendered := runSingleWith(t, inventoryPath, ovhcloud.New(), jsonreport.New(),
		analysis.Options{Estimate: true}, analysis.ReportMeta{InventoryName: filepath.Base(inventoryPath)})

	var doc struct {
		Resources []reportedResource `json:"resources"`
	}
	if err := json.Unmarshal(rendered, &doc); err != nil {
		t.Fatalf("decode the JSON report of %s: %v", inventoryPath, err)
	}

	tests := []struct {
		name string
		// id is the fixture resource the case is about.
		id string
		// want is the storageNotIncluded the report must carry, "" for none.
		want string
		// wantStoragePriced is whether the estimate must price storage the
		// resource gives, above the none the plan includes.
		wantStoragePriced bool
	}{
		{
			name: "a VM with no disk size is estimated with the boot volume note",
			id:   "i-0123456789abcdef0",
			want: "boot-volume",
		},
		{
			name:              "a VM with a disk size is estimated without the note",
			id:                "i-0123456789abcdef1",
			wantStoragePriced: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resource := reportedResourceOf(t, doc.Resources, tc.id)

			// The note is right only on a plan that includes no storage, so pin
			// that the OVHcloud plan each VM got is such a one.
			if resource.Plan == nil {
				t.Fatalf("%s: the report names no plan, want the Gen 3 plan that includes no storage", tc.id)
			}

			if !strings.HasPrefix(resource.Plan.Name, "b3-") {
				t.Errorf("%s: plan = %q, want a Gen 3 plan (b3-...)", tc.id, resource.Plan.Name)
			}

			if resource.Plan.IncludedStorageGiB == nil || *resource.Plan.IncludedStorageGiB != 0 {
				t.Errorf("%s: plan %q includedStorageGiB = %v, want 0",
					tc.id, resource.Plan.Name, resource.Plan.IncludedStorageGiB)
			}

			if resource.Estimate == nil || resource.Estimate.Estimated == nil || !*resource.Estimate.Estimated {
				t.Fatalf("%s: the report does not estimate the resource, want an estimate", tc.id)
			}

			if got := resource.Estimate.StorageNotIncluded; got != tc.want {
				t.Errorf("%s: storageNotIncluded = %q, want %q", tc.id, got, tc.want)
			}

			if resource.Estimate.Breakdown == nil {
				t.Fatalf("%s: the estimate has no breakdown, want the storage it prices", tc.id)
			}

			if got := resource.Estimate.Breakdown.Storage != "0.00"; got != tc.wantStoragePriced {
				t.Errorf("%s: breakdown.storage = %q, want the resource's storage priced: %t",
					tc.id, resource.Estimate.Breakdown.Storage, tc.wantStoragePriced)
			}
		})
	}
}

// reportedResourceOf returns the report's entry for the resource with id, and
// fails the test when the report has none: a lookup that finds nothing must not
// let the case pass.
func reportedResourceOf(t *testing.T, resources []reportedResource, id string) reportedResource {
	t.Helper()

	for _, resource := range resources {
		if resource.ID == id {
			return resource
		}
	}

	t.Fatalf("the report has no resource %q: want the fixture's resource", id)

	return reportedResource{}
}
