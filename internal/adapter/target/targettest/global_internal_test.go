package targettest

// This file is an internal test because the target contract's own pieces cannot
// be reached from a black-box test, and the contract cannot observe them end to
// end: equalOffering and describe are unexported, and a catalog.Catalog is built
// only by catalog.New, which refuses a global offering of a category the taxonomy
// does not mark global. So the check that catches such an offering, and the
// equality and dump that carry the global flag, are pinned over the values they
// take. Everything a fake target can return is pinned in targettest_test.go.

import (
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
)

// offeringWithGlobal is one offering with the global flag set and every other
// field the same, so two of them differ in that flag alone.
func offeringWithGlobal(global bool) catalog.Offering {
	return catalog.Offering{
		Name:       "DNS Zone",
		Categories: []taxonomy.Category{taxonomy.NetworkDNS},
		Global:     global,
	}
}

// TestEqualOfferingCarriesGlobal pins that the global flag is part of an
// offering: the repeat check compares what an adapter answers between calls
// field by field, so an adapter that rewrites the flag alone is not stable (PRD
// #581, The region model).
func TestEqualOfferingCarriesGlobal(t *testing.T) {
	t.Parallel()

	global, regional := offeringWithGlobal(true), offeringWithGlobal(false)

	if !equalOffering(global, global) || !equalOffering(regional, regional) {
		t.Error("equalOffering() reports an offering unequal to itself, want every field of an offering compared")
	}

	if equalOffering(global, regional) {
		t.Error("equalOffering() reports offerings that differ only in Global as equal, want them unequal")
	}

	if equalOffering(regional, global) {
		t.Error("equalOffering() reports offerings that differ only in Global as equal, want them unequal")
	}
}

// TestDescribeCarriesGlobal pins that a violation shows the global flag, so an
// adapter author reading the repeat violation sees what differs, and the
// parsed-catalog goldens that hold a describe output show it too.
func TestDescribeCarriesGlobal(t *testing.T) {
	t.Parallel()

	global, regional := describe(offeringWithGlobal(true)), describe(offeringWithGlobal(false))

	if global == regional {
		t.Fatalf("describe() of a global and a regional offering is the same %s, want the Global field shown", global)
	}

	if !strings.Contains(global, "Global: true") {
		t.Errorf("describe() of a global offering = %q, want it to contain %q", global, "Global: true")
	}

	if !strings.Contains(regional, "Global: false") {
		t.Errorf("describe() of a regional offering = %q, want it to contain %q", regional, "Global: false")
	}
}

// TestCheckGlobalCategories pins the contract check that a global offering
// serves only categories the taxonomy marks global (PRD #581, The region model):
// catalog.New refuses such an offering already, so the check is defence in depth
// for a catalog built another way, and it must still say which offering and
// category are at fault. A regional offering of a global category is conforming:
// a category marked global may have regional offerings.
func TestCheckGlobalCategories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		offerings []catalog.Offering
		// wantViolations is how many violations the offerings must give.
		wantViolations int
		// wantMsg lists the substrings the joined violations must hold.
		wantMsg []string
	}{
		{
			name:      "a global offering of a global category",
			offerings: []catalog.Offering{offeringWithGlobal(true)},
		},
		{
			name: "a global offering of a category that is not global",
			offerings: []catalog.Offering{{
				Name:       "Instances",
				Categories: []taxonomy.Category{taxonomy.ComputeVM},
				Global:     true,
			}},
			wantViolations: 1,
			wantMsg:        []string{"global-category", "Instances", "compute.vm"},
		},
		{
			name:      "a regional offering of a global category",
			offerings: []catalog.Offering{offeringWithGlobal(false)},
		},
		{
			name:      "a regional offering of a category that is not global",
			offerings: []catalog.Offering{{Name: "Instances", Categories: []taxonomy.Category{taxonomy.ComputeVM}}},
		},
		{
			// One offering may serve several categories, and it is enough that
			// one of them is not global.
			name: "a global offering serving a global and a regional category",
			offerings: []catalog.Offering{{
				Name:       "DNS Zone and Instances",
				Categories: []taxonomy.Category{taxonomy.NetworkDNS, taxonomy.ComputeVM},
				Global:     true,
			}},
			wantViolations: 1,
			wantMsg:        []string{"global-category", "DNS Zone and Instances", "compute.vm"},
		},
		{
			// Every offering at fault is named, not only the first: a target
			// author must be able to fix them all from one report.
			name: "two global offerings of categories that are not global",
			offerings: []catalog.Offering{
				{Name: "Alpha Instances", Categories: []taxonomy.Category{taxonomy.ComputeVM}, Global: true},
				{Name: "Beta Buckets", Categories: []taxonomy.Category{taxonomy.StorageObject}, Global: true},
			},
			wantViolations: 2,
			wantMsg:        []string{"Alpha Instances", "compute.vm", "Beta Buckets", "storage.object"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			errs := checkGlobalCategories(tc.offerings)

			if len(errs) != tc.wantViolations {
				t.Fatalf("checkGlobalCategories() returned %d violations, want %d: %v",
					len(errs), tc.wantViolations, errs)
			}

			if tc.wantViolations == 0 {
				return
			}

			for i, err := range errs {
				if !strings.HasPrefix(err.Error(), "global-category:") {
					t.Errorf("violation %d = %q, want it to start with the check it breaks, %q",
						i+1, err, "global-category:")
				}
			}

			msg := violationMessages(errs)

			for _, want := range tc.wantMsg {
				if !strings.Contains(msg, want) {
					t.Errorf("checkGlobalCategories() violations = %q, want them to mention %q", msg, want)
				}
			}
		})
	}
}

// violationMessages joins the messages of errs, one per line.
func violationMessages(errs []error) string {
	messages := make([]string, len(errs))
	for i, err := range errs {
		messages[i] = err.Error()
	}

	return strings.Join(messages, "\n")
}
