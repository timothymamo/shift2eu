package publish

// Placement is where a finding goes: the public report, its needs-investigation
// list, or a draft advisory.
type Placement int

// The five placements. The zero value is none of them, and Private reads it as
// private, so a placement nobody set fails closed.
const (
	// PlacementPublic is a confirmed low or informational finding: the
	// findings table, and after the merge one security issue.
	PlacementPublic Placement = iota + 1
	// PlacementInvestigation is an unconfirmed low or informational finding:
	// the needs-investigation list, no issue.
	PlacementInvestigation
	// PlacementAdvisory is a confirmed private finding: a draft advisory.
	PlacementAdvisory
	// PlacementAdvisoryUnconfirmed is an unconfirmed private finding: a draft
	// advisory marked unconfirmed.
	PlacementAdvisoryUnconfirmed
	// PlacementAdvisoryHistorySecret is a secret in the history: a draft
	// advisory naming the commit and the rule only, "unconfirmed: maintainer
	// to check".
	PlacementAdvisoryHistorySecret
)

// Private reports whether a finding of this placement stays out of every
// public text: true for the three advisory placements, and for any value that
// is not one of the five.
func (p Placement) Private() bool {
	return p != PlacementPublic && p != PlacementInvestigation
}

// Place derives a finding's placement from its check, severity, confirmation,
// category and the model's flag alone, by ADR 0040's disclosure table: the
// first matching row decides, and the model's flag can only make a finding
// private.
func Place(f Finding) Placement {
	if f.Check == historySecretsCheck {
		return PlacementAdvisoryHistorySecret
	}

	advisory := PlacementAdvisory
	if !f.Confirmed {
		advisory = PlacementAdvisoryUnconfirmed
	}

	_, knownSeverity := severityRank[f.Severity]
	privateCategory, knownCategory := categories[f.Category]

	switch {
	case !knownSeverity || !knownCategory:
		return advisory
	case privateCategory:
		return advisory
	case privateSeverities[f.Severity]:
		return advisory
	case f.Private:
		return advisory
	case f.Confirmed:
		return PlacementPublic
	default:
		return PlacementInvestigation
	}
}
