package markdown_test

import (
	"slices"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// phraseRegion is the region the reasons that name one are rendered with. It
// holds a pipe, the one character a code span writes differently in a table cell
// than in running text (codeSpan), so the two places a report renders a reason
// in tell apart, and each phrase is pinned in both.
const phraseRegion = "fr|par"

// phraseRegionInTable is phraseRegion as a table cell's code span writes it: the
// pipe escaped (codeSpan).
const phraseRegionInTable = `fr\|par`

// notEstimatedPlaces are the two places a report renders a reason in, and what
// renderNotEstimated is told there: the Monthly estimate cell, and a total in a
// table cell, are table cells (true); a total in a list item is running text
// (false). It is the list's place that escapes a region the reasons name, and
// not the reason itself, so a phrase is pinned in both.
var notEstimatedPlaces = []struct {
	name    string
	inTable bool
}{
	{name: "table cell", inTable: true},
	{name: "list item", inTable: false},
}

// reasonPhrases is the phrase a report gives each reason of the estimate domain,
// as the Monthly estimate cell and a total show it, with the estimate whose
// region the phrase names. It is the report's own wording, so it stays in this
// adapter (CLAUDE.md, ubiquitous language: the domain gives reasons, not
// sentences); a reason the domain declares without an entry here is named by
// TestEveryReasonHasAPhraseAndAPlaceInTheOrder. wantInTable is the phrase as a
// table cell shows it, which differs from want only in the code span's escaping
// of phraseRegion; empty means the same as want.
var reasonPhrases = map[estimate.Reason]struct {
	estimate    estimate.Estimate
	want        string
	wantInTable string
}{
	estimate.NoFit:             {want: "no fit"},
	estimate.UsagePriced:       {want: "usage-priced"},
	estimate.NeverPriced:       {want: "not priced"},
	estimate.NoSize:            {want: "size unknown"},
	estimate.NoPlan:            {want: "no plan"},
	estimate.NoPlanLargeEnough: {want: "no plan large enough"},
	estimate.NotOffered: {
		estimate:    estimate.Estimate{Region: phraseRegion},
		want:        "not offered in `" + phraseRegion + "`",
		wantInTable: "not offered in `" + phraseRegionInTable + "`",
	},
	estimate.NoPrice: {
		estimate:    estimate.Estimate{Region: phraseRegion},
		want:        "no price in `" + phraseRegion + "`",
		wantInTable: "no price in `" + phraseRegionInTable + "`",
	},
	estimate.NoStoragePrice: {
		estimate:    estimate.Estimate{Region: phraseRegion},
		want:        "no storage price in `" + phraseRegion + "`",
		wantInTable: "no storage price in `" + phraseRegionInTable + "`",
	},
}

// reasonsPhrasedAsTheirCode are the reasons whose phrase is their own code, so
// the text renderNotEstimated's default gives an unknown reason is the same as
// the one its case gives them. A reason in this list is the only one whose
// phrase may be that default, which keeps the phrase check from passing by
// accident.
var reasonsPhrasedAsTheirCode = []estimate.Reason{
	estimate.UsagePriced,
}

// TestEveryReasonHasAPhraseAndAPlaceInTheOrder pins that the report says
// something about every reason the estimate domain can give a resource, and
// names it in reasonOrder: a reason added to the domain without a phrase here, or
// without a place in the order, would be silently dropped from "none of N
// resources could be estimated (<reasons>)" (issue #308). The phrasing stays in
// this adapter; the domain gives reasons, not sentences.
//
// A phrase is pinned in both places a report renders a reason in
// (notEstimatedPlaces), because the cell and the list item write a code span
// differently, and the phrase check compares against what the default branch
// really returns in each (TestFallbackPhraseIsTheEscapedCode).
//
// The reasons come from the domain's own source (testutil.EstimateReasons), so
// this test sees a reason it was not written for.
func TestEveryReasonHasAPhraseAndAPlaceInTheOrder(t *testing.T) {
	t.Parallel()

	for _, place := range notEstimatedPlaces {
		t.Run(place.name, func(t *testing.T) {
			t.Parallel()

			for _, reason := range testutil.EstimateReasons(t) {
				t.Run(string(reason), func(t *testing.T) {
					t.Parallel()

					if !slices.Contains(markdown.ReasonOrder, reason) {
						t.Errorf("reasonOrder has no place for estimate.Reason %q, want it in %v so a total names it",
							string(reason), markdown.ReasonOrder)
					}

					phrase, ok := reasonPhrases[reason]
					if !ok {
						t.Errorf("estimate.Reason %q has no Markdown phrase, want one in this test's reasonPhrases "+
							"and a case in the report's renderNotEstimated", string(reason))

						return
					}

					want := phrase.want
					if place.inTable && phrase.wantInTable != "" {
						want = phrase.wantInTable
					}

					e := phrase.estimate
					e.Reason = reason

					got := markdown.RenderNotEstimated(e, place.inTable)

					if got != want {
						t.Errorf("renderNotEstimated(%q, inTable %t) in a %s = %q, want %q",
							string(reason), place.inTable, place.name, got, want)
					}

					if got == markdown.EscapeText(string(reason)) && !slices.Contains(reasonsPhrasedAsTheirCode, reason) {
						t.Errorf("renderNotEstimated(%q, inTable %t) in a %s = %q, which is what its default gives a "+
							"reason it has no case for (the escaped code), so the phrase could be that fallback rather "+
							"than the report's own words; want a case in renderNotEstimated, or the reason in "+
							"reasonsPhrasedAsTheirCode", string(reason), place.inTable, place.name, got)
					}
				})
			}
		})
	}
}

// TestFallbackPhraseIsTheEscapedCode pins the premise the phrase check above
// stands on: renderNotEstimated's default branch shows a reason it has no case
// for as its code escaped (escapeText), in both places it is rendered in, so a
// phrase is compared with that escape and not with the code itself. A code
// holding a character the escaper replaces makes the two tell apart; without it,
// the guard would take such a fallback for the report's own words.
func TestFallbackPhraseIsTheEscapedCode(t *testing.T) {
	t.Parallel()

	unknown := estimate.Reason("no-fit|unknown|reason")

	want := markdown.EscapeText(string(unknown))
	if want == string(unknown) {
		t.Fatalf("escapeText(%q) = %q, want the code escaped, so a comparison tells the fallback from a phrase",
			string(unknown), want)
	}

	for _, place := range notEstimatedPlaces {
		t.Run(place.name, func(t *testing.T) {
			t.Parallel()

			got := markdown.RenderNotEstimated(estimate.Estimate{Reason: unknown}, place.inTable)

			if got != want {
				t.Fatalf("renderNotEstimated(%q, inTable %t) in a %s = %q, want its default branch's %q",
					string(unknown), place.inTable, place.name, got, want)
			}
		})
	}
}

// TestReasonOrderHoldsOnlyDomainReasons pins the other direction: reasonOrder
// names no reason the domain no longer declares, so the report's order cannot
// quietly hold a stale entry.
func TestReasonOrderHoldsOnlyDomainReasons(t *testing.T) {
	t.Parallel()

	reasons := testutil.EstimateReasons(t)

	for _, reason := range markdown.ReasonOrder {
		if !slices.Contains(reasons, reason) {
			t.Errorf("reasonOrder lists estimate.Reason %q, which the domain does not declare; want one of %v",
				string(reason), reasons)
		}
	}
}
