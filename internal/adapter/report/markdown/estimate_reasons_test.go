package markdown_test

import (
	"slices"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// phraseRegion is the region the reasons that name one are rendered with.
const phraseRegion = "fr-par"

// reasonPhrases is the phrase a report gives each reason of the estimate domain,
// as the Monthly estimate cell and a total show it, with the estimate whose
// region the phrase names. It is the report's own wording, so it stays in this
// adapter (CLAUDE.md, ubiquitous language: the domain gives reasons, not
// sentences); a reason the domain declares without an entry here is named by
// TestEveryReasonHasAPhraseAndAPlaceInTheOrder.
var reasonPhrases = map[estimate.Reason]struct {
	estimate estimate.Estimate
	want     string
}{
	estimate.NoFit:             {want: "no fit"},
	estimate.UsagePriced:       {want: "usage-priced"},
	estimate.NoSize:            {want: "size unknown"},
	estimate.NoPlan:            {want: "no plan"},
	estimate.NoPlanLargeEnough: {want: "no plan large enough"},
	estimate.NotOffered:        {estimate: estimate.Estimate{Region: phraseRegion}, want: "not offered in `" + phraseRegion + "`"},
	estimate.NoPrice:           {estimate: estimate.Estimate{Region: phraseRegion}, want: "no price in `" + phraseRegion + "`"},
	estimate.NoStoragePrice:    {estimate: estimate.Estimate{Region: phraseRegion}, want: "no storage price in `" + phraseRegion + "`"},
}

// reasonsPhrasedAsTheirCode are the reasons whose phrase is their own code, so
// the text renderNotEstimated's default gives an unknown reason is the same as
// the one its case gives them. A reason in this list is the only one whose
// phrase may equal its code, which keeps that default from satisfying the
// phrase check by accident.
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
// The reasons come from the domain's own source (testutil.EstimateReasons), so
// this test sees a reason it was not written for.
func TestEveryReasonHasAPhraseAndAPlaceInTheOrder(t *testing.T) {
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

			e := phrase.estimate
			e.Reason = reason

			got := markdown.RenderNotEstimated(e, false)

			if got != phrase.want {
				t.Errorf("renderNotEstimated(%q) = %q, want %q", string(reason), got, phrase.want)
			}

			if got == string(reason) && !slices.Contains(reasonsPhrasedAsTheirCode, reason) {
				t.Errorf("renderNotEstimated(%q) = %q, which is what its default gives a reason it has no case for, "+
					"so the phrase could be that fallback rather than the report's own words; want a case in "+
					"renderNotEstimated, or the reason in reasonsPhrasedAsTheirCode", string(reason), got)
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
