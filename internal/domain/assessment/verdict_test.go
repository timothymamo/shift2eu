package assessment_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
)

// candidate is a candidate offering named name with fit and missing
// capabilities.
func candidate(name string, fit mapping.Fit, missing ...taxonomy.Capability) mapping.Candidate {
	return mapping.Candidate{Offering: catalog.Offering{Name: name}, Fit: fit, Missing: missing}
}

// globalCandidate is candidate, with the offering recorded as global
// (catalog.Offering.Global, PRD #581, ADR 0048).
func globalCandidate(name string, fit mapping.Fit, missing ...taxonomy.Capability) mapping.Candidate {
	c := candidate(name, fit, missing...)
	c.Offering.Global = true

	return c
}

// isSubsequence reports whether want appears in got in the same order, not
// necessarily next to one another.
func isSubsequence(want, got []string) bool {
	i := 0

	for _, name := range got {
		if i < len(want) && want[i] == name {
			i++
		}
	}

	return i == len(want)
}

// TestVerdictTies pins which candidates every report shows as ties of the best
// one: the same fit and as many missing capabilities, in candidate order.
func TestVerdictTies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		candidates []mapping.Candidate
		want       []string
	}{
		{name: "no candidate"},
		{name: "one candidate", candidates: []mapping.Candidate{candidate("A", mapping.Direct)}},
		{
			name: "direct ties in candidate order",
			candidates: []mapping.Candidate{
				candidate("A", mapping.Direct),
				candidate("C", mapping.Direct),
				candidate("B", mapping.Direct),
			},
			want: []string{"C", "B"},
		},
		{
			name: "a partial candidate does not tie a direct one",
			candidates: []mapping.Candidate{
				candidate("A", mapping.Direct),
				candidate("B", mapping.Partial, taxonomy.IPv6),
			},
		},
		{
			name: "partial candidates tie only with as many missing capabilities",
			candidates: []mapping.Candidate{
				candidate("A", mapping.Partial, taxonomy.IPv6),
				candidate("B", mapping.Partial, taxonomy.Snapshots),
				candidate("C", mapping.Partial, taxonomy.IPv6, taxonomy.Snapshots),
			},
			want: []string{"B"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := assessment.Verdict{Candidates: tc.candidates}.Ties()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Ties() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestVerdictGlobalTies pins the invariant every report relies on to mark the
// global offerings it names and to write the global note once (PRD #581, ADR
// 0048): GlobalTies is exactly the subsequence of Ties whose offering is global,
// in tie order, it is empty when there is no best offering, and ShowsGlobal is
// true exactly when the report names a global offering, the best one or a tie.
func TestVerdictGlobalTies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		fit          mapping.Fit
		bestOffering string
		candidates   []mapping.Candidate
		wantTies     []string
		wantGlobal   []string
		wantShows    bool
	}{
		{name: "no candidate"},
		{
			name:         "a global best offering without a tie",
			bestOffering: "A",
			candidates: []mapping.Candidate{
				globalCandidate("A", mapping.Direct),
				candidate("B", mapping.Partial, taxonomy.IPv6),
			},
			wantShows: true,
		},
		{
			name:         "a regional tie of a global best offering is not marked",
			bestOffering: "A",
			candidates: []mapping.Candidate{
				globalCandidate("A", mapping.Direct),
				candidate("B", mapping.Direct),
				candidate("C", mapping.Direct),
			},
			wantTies:  []string{"B", "C"},
			wantShows: true,
		},
		{
			name:         "the global ties of a regional best offering are marked, in tie order",
			bestOffering: "A",
			candidates: []mapping.Candidate{
				candidate("A", mapping.Direct),
				globalCandidate("B", mapping.Direct),
				candidate("C", mapping.Direct),
				globalCandidate("D", mapping.Direct),
			},
			wantTies:   []string{"B", "C", "D"},
			wantGlobal: []string{"B", "D"},
			wantShows:  true,
		},
		{
			name:         "only a global candidate that ties is marked",
			fit:          mapping.Partial,
			bestOffering: "A",
			candidates: []mapping.Candidate{
				candidate("A", mapping.Partial, taxonomy.IPv6),
				globalCandidate("B", mapping.Partial, taxonomy.Snapshots),
				candidate("C", mapping.Partial, taxonomy.IPv6),
				globalCandidate("D", mapping.Partial, taxonomy.IPv6, taxonomy.Snapshots),
			},
			wantTies:   []string{"B", "C"},
			wantGlobal: []string{"B"},
			wantShows:  true,
		},
		{
			name: "no best offering, no global candidate",
			candidates: []mapping.Candidate{
				candidate("A", mapping.Direct),
				candidate("B", mapping.Direct),
			},
			wantTies: []string{"B"},
		},
		{
			name: "no best offering, global ties",
			candidates: []mapping.Candidate{
				globalCandidate("A", mapping.Direct),
				globalCandidate("B", mapping.Direct),
			},
			wantTies: []string{"B"},
		},
		{
			name: "no best offering, an unclassified verdict whose global candidates tie",
			fit:  mapping.None,
			candidates: []mapping.Candidate{
				globalCandidate("A", mapping.Partial, taxonomy.IPv6),
				globalCandidate("B", mapping.Partial, taxonomy.Snapshots),
			},
			wantTies: []string{"B"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			v := assessment.Verdict{
				Fit:          tc.fit,
				BestOffering: tc.bestOffering,
				Candidates:   tc.candidates,
			}

			ties := v.Ties()
			if diff := cmp.Diff(tc.wantTies, ties); diff != "" {
				t.Fatalf("Ties() mismatch (-want +got):\n%s", diff)
			}

			got := v.GlobalTies()
			if diff := cmp.Diff(tc.wantGlobal, got); diff != "" {
				t.Errorf("GlobalTies() mismatch (-want +got):\n%s", diff)
			}

			if !isSubsequence(got, ties) {
				t.Errorf("GlobalTies() = %v is not a subsequence of Ties() = %v", got, ties)
			}

			if tc.bestOffering == "" && len(got) > 0 {
				t.Errorf("GlobalTies() = %v with no best offering, want none", got)
			}

			if shown, want := v.ShowsGlobal(), tc.wantShows; shown != want {
				t.Errorf("ShowsGlobal() = %t, want %t", shown, want)
			}
		})
	}
}

// TestVerdictChangesToList pins that a report lists required changes for a
// partial fit only.
func TestVerdictChangesToList(t *testing.T) {
	t.Parallel()

	changes := []taxonomy.Capability{taxonomy.IPv6}

	tests := []struct {
		fit  mapping.Fit
		want []taxonomy.Capability
	}{
		{fit: mapping.Direct},
		{fit: mapping.Partial, want: changes},
		{fit: mapping.None},
	}

	for _, tc := range tests {
		t.Run(string(tc.fit), func(t *testing.T) {
			t.Parallel()

			got := assessment.Verdict{Fit: tc.fit, RequiredChanges: changes}.ChangesToList()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ChangesToList() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
