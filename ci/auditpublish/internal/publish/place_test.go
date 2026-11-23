package publish_test

import (
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// TestPlace pins the disclosure table of ADR 0040 row by row, first match
// first: a finding's place is derived from its check, severity, confirmation,
// category and the model's own flag, and nothing else. Each row also says what
// the table's earlier rows must win over.
func TestPlace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// row is the disclosure table's row this case pins.
		row   int
		place publish.Placement
		// flagPrivate sets the model's private flag, the only input row 5 reads.
		flagPrivate bool
		finding     publish.Finding
	}{
		{
			name:  "row 1: a history secret is always advisory",
			row:   1,
			place: publish.PlacementAdvisoryHistorySecret,
			finding: publish.Finding{
				Check: "history-secrets", Severity: sevLow, Category: catConfiguration, Confirmed: true,
			},
		},
		{
			name:  "row 1 wins over row 4: a history secret stays a history secret at critical",
			row:   1,
			place: publish.PlacementAdvisoryHistorySecret,
			finding: publish.Finding{
				Check: "history-secrets", Severity: sevCritical, Category: catInjection, Confirmed: false,
			},
		},
		{
			name:  "row 2: an unknown severity, unconfirmed",
			row:   2,
			place: publish.PlacementAdvisoryUnconfirmed,
			finding: publish.Finding{
				Check: "tree-review", Severity: "High", Category: catHardening, Confirmed: false,
			},
		},
		{
			name:  "row 2: an unknown severity, confirmed",
			row:   2,
			place: publish.PlacementAdvisory,
			finding: publish.Finding{
				Check: "tree-review", Severity: "High", Category: catHardening, Confirmed: true,
			},
		},
		{
			name:  "row 2 wins over row 6: an unknown category stays advisory at low",
			row:   2,
			place: publish.PlacementAdvisory,
			finding: publish.Finding{
				Check: "tree-review", Severity: sevLow, Category: "runtime sink", Confirmed: true,
			},
		},
		{
			name:  "row 2 wins over row 5: an unknown severity is advisory unconfirmed without the model's flag",
			row:   2,
			place: publish.PlacementAdvisoryUnconfirmed,
			finding: publish.Finding{
				Check: "tree-review", Severity: "", Category: catHardening, Confirmed: false,
			},
		},
		{
			name:  "row 3: injection is advisory whatever the severity",
			row:   3,
			place: publish.PlacementAdvisory,
			finding: publish.Finding{
				Check: "tree-review", Severity: sevLow, Category: catInjection, Confirmed: true,
			},
		},
		{
			name:  "row 3: authentication bypass, unconfirmed",
			row:   3,
			place: publish.PlacementAdvisoryUnconfirmed,
			finding: publish.Finding{
				Check: "tree-review", Severity: sevInformational, Category: "authentication bypass", Confirmed: false,
			},
		},
		{
			name:  "row 3 wins over row 6: credential exposure stays advisory at informational",
			row:   3,
			place: publish.PlacementAdvisory,
			finding: publish.Finding{
				Check: "tree-review", Severity: sevInformational, Category: "credential exposure", Confirmed: true,
			},
		},
		{
			name:  "row 3: remote code execution is advisory",
			row:   3,
			place: publish.PlacementAdvisory,
			finding: publish.Finding{
				Check: "tree-review", Severity: sevLow, Category: "remote code execution", Confirmed: true,
			},
		},
		{
			name:  "row 4: critical is advisory",
			row:   4,
			place: publish.PlacementAdvisory,
			finding: publish.Finding{
				Check: "tree-review", Severity: sevCritical, Category: catConfiguration, Confirmed: true,
			},
		},
		{
			name:  "row 4: high is advisory",
			row:   4,
			place: publish.PlacementAdvisory,
			finding: publish.Finding{
				Check: "supply-chain", Severity: sevHigh, Category: catSupplyChain, Confirmed: true,
			},
		},
		{
			name:  "row 4: medium, unconfirmed, is advisory unconfirmed",
			row:   4,
			place: publish.PlacementAdvisoryUnconfirmed,
			finding: publish.Finding{
				Check: "dependencies", Severity: sevMedium, Category: catDependency, Confirmed: false,
			},
		},
		{
			name:  "row 4 wins over row 6: a confirmed medium finding is not public",
			row:   4,
			place: publish.PlacementAdvisory,
			finding: publish.Finding{
				Check: "dependencies", Severity: sevMedium, Category: catDependency, Confirmed: true,
			},
		},
		{
			name:        "row 5: the model's flag makes a confirmed low finding private",
			row:         5,
			place:       publish.PlacementAdvisory,
			flagPrivate: true,
			finding: publish.Finding{
				Check: "tree-review", Severity: sevLow, Category: catHardening, Confirmed: true,
			},
		},
		{
			name:        "row 5: the flag on an unconfirmed finding",
			row:         5,
			place:       publish.PlacementAdvisoryUnconfirmed,
			flagPrivate: true,
			finding: publish.Finding{
				Check: "tree-review", Severity: sevInformational, Category: catHardening, Confirmed: false,
			},
		},
		{
			name:  "row 6: confirmed low is public",
			row:   6,
			place: publish.PlacementPublic,
			finding: publish.Finding{
				Check: "workflows", Severity: sevLow, Category: catHardening, Confirmed: true,
			},
		},
		{
			name:  "row 6: confirmed informational is public",
			row:   6,
			place: publish.PlacementPublic,
			finding: publish.Finding{
				Check: "agent-surface", Severity: sevInformational, Category: catConfiguration, Confirmed: true,
			},
		},
		{
			name:  "row 7: unconfirmed low needs investigation",
			row:   7,
			place: publish.PlacementInvestigation,
			finding: publish.Finding{
				Check: "tree-review", Severity: sevLow, Category: catDenialOfSvc, Confirmed: false,
			},
		},
		{
			name:  "row 7: unconfirmed informational needs investigation",
			row:   7,
			place: publish.PlacementInvestigation,
			finding: publish.Finding{
				Check: "tree-review", Severity: sevInformational, Category: catInfoDisclosure, Confirmed: false,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			finding := tc.finding
			finding.Private = tc.flagPrivate

			if got := publish.Place(finding); got != tc.place {
				t.Errorf("Place(%+v) = placement %d, want row %d's placement %d", finding, got, tc.row, tc.place)
			}
		})
	}
}

// TestPlacementPrivate pins that exactly the three advisory placements are
// private: public and needs-investigation findings reach the report.
func TestPlacementPrivate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		place publish.Placement
		want  bool
	}{
		{publish.PlacementPublic, false},
		{publish.PlacementInvestigation, false},
		{publish.PlacementAdvisory, true},
		{publish.PlacementAdvisoryUnconfirmed, true},
		{publish.PlacementAdvisoryHistorySecret, true},
	}

	for _, tc := range tests {
		if got := tc.place.Private(); got != tc.want {
			t.Errorf("Placement(%d).Private() = %t, want %t", tc.place, got, tc.want)
		}
	}

	// The five placements are distinct, so a table row that returns one never
	// stands for another.
	places := []publish.Placement{
		publish.PlacementPublic,
		publish.PlacementInvestigation,
		publish.PlacementAdvisory,
		publish.PlacementAdvisoryUnconfirmed,
		publish.PlacementAdvisoryHistorySecret,
	}

	for i, a := range places {
		for j, b := range places {
			if i != j && a == b {
				t.Errorf("Placement(%d) and Placement(%d) are the same value, want five distinct placements", i, j)
			}
		}
	}
}
