package htmlreport_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/htmlreport"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// phraseRegion is the region the reasons that name one are rendered with.
const phraseRegion = "fr-par"

// reasonPhrases is the phrase the HTML report gives each reason of the estimate
// domain, as the Monthly estimate cell and a total show it, with the estimate
// whose region the phrase names. The report keeps its own wording rather than
// the Markdown report's, so each adapter states it here: the two are siblings,
// not one, and the domain gives reasons, not sentences (CLAUDE.md, ubiquitous
// language). A value the report shows as code is in backticks
// (htmlreport.NotEstimatedPhrase).
var reasonPhrases = map[estimate.Reason]struct {
	estimate estimate.Estimate
	want     string
}{
	estimate.NoFit:             {want: "no fit"},
	estimate.UsagePriced:       {want: "usage-priced"},
	estimate.NeverPriced:       {want: "not priced"},
	estimate.NoSize:            {want: "size unknown"},
	estimate.NoPlan:            {want: "no plan"},
	estimate.NoPlanLargeEnough: {want: "no plan large enough"},
	estimate.NotOffered:        {estimate: estimate.Estimate{Region: phraseRegion}, want: "not offered in `" + phraseRegion + "`"},
	estimate.NoPrice:           {estimate: estimate.Estimate{Region: phraseRegion}, want: "no price in `" + phraseRegion + "`"},
	estimate.NoStoragePrice:    {estimate: estimate.Estimate{Region: phraseRegion}, want: "no storage price in `" + phraseRegion + "`"},
}

// reasonsPhrasedAsTheirCode are the reasons whose phrase is their own code, so
// renderNotEstimated's default gives an unknown reason the same phrase as the
// one its case gives them. A reason in this list is the only one whose phrase
// may be that default, which keeps the phrase check from passing by accident.
var reasonsPhrasedAsTheirCode = []estimate.Reason{
	estimate.UsagePriced,
}

// TestEveryReasonHasAPhraseAndAPlaceInTheOrder pins that the HTML report says
// something about every reason the estimate domain can give a resource, and
// names it in reasonOrder: a reason added to the domain without a phrase here,
// or without a place in the order, would be silently dropped from "none of N
// resources could be estimated (<reasons>)" (issue #308). The Markdown report
// has the same guard, which is why the domain reader is shared
// (testutil.EstimateReasons); the phrasing and the order are this adapter's own.
func TestEveryReasonHasAPhraseAndAPlaceInTheOrder(t *testing.T) {
	t.Parallel()

	for _, reason := range testutil.EstimateReasons(t) {
		t.Run(string(reason), func(t *testing.T) {
			t.Parallel()

			if !slices.Contains(htmlreport.ReasonOrder, reason) {
				t.Errorf("reasonOrder has no place for estimate.Reason %q, want it in %v so a total names it",
					string(reason), htmlreport.ReasonOrder)
			}

			phrase, ok := reasonPhrases[reason]
			if !ok {
				t.Errorf("estimate.Reason %q has no HTML phrase, want one in this test's reasonPhrases "+
					"and a case in the report's renderNotEstimated", string(reason))

				return
			}

			e := phrase.estimate
			e.Reason = reason

			got := htmlreport.NotEstimatedPhrase(e)

			if got != phrase.want {
				t.Errorf("renderNotEstimated(%q) = %q, want %q", string(reason), got, phrase.want)
			}

			// The default branch writes the code itself, and the fragment escapes it
			// only when it writes the cell, so the fallback a test reads here is
			// the code as it is (TestFallbackPhraseIsTheCode).
			if got == string(reason) && !slices.Contains(reasonsPhrasedAsTheirCode, reason) {
				t.Errorf("renderNotEstimated(%q) = %q, which is what its default gives a reason it has no case "+
					"for, so the phrase could be that fallback rather than the report's own words; want a case in "+
					"renderNotEstimated, or the reason in reasonsPhrasedAsTheirCode", string(reason), got)
			}
		})
	}
}

// TestFallbackPhraseIsTheCode pins the premise the phrase check above stands on:
// renderNotEstimated's default branch shows a reason it has no case for as its
// code, as it is, and the fragment escapes it only when it writes the cell
// (fragment.go, textEscaper), so the phrase a test reads here is compared with
// the code itself and not with an escaped form. A code holding a character the
// text escaper replaces makes the two tell apart; without it, a default branch
// that escaped the code would pass for one that did not.
func TestFallbackPhraseIsTheCode(t *testing.T) {
	t.Parallel()

	unknown := estimate.Reason("no-fit<unknown>&reason")

	if !strings.Contains(string(unknown), "<") || !strings.Contains(string(unknown), "&") {
		t.Fatalf("estimate.Reason %q holds no markup character, want one the text escaper replaces, "+
			"so the comparison below tells an escaped code from a code written as it is", string(unknown))
	}

	got := htmlreport.NotEstimatedPhrase(estimate.Estimate{Reason: unknown})

	if got != string(unknown) {
		t.Fatalf("NotEstimatedPhrase(%q) = %q, want the code as it is: the default branch writes it unescaped",
			string(unknown), got)
	}
}

// TestReasonOrderHoldsOnlyDomainReasons pins the other direction: reasonOrder
// names no reason the domain no longer declares, so the report's order cannot
// quietly hold a stale entry.
func TestReasonOrderHoldsOnlyDomainReasons(t *testing.T) {
	t.Parallel()

	reasons := testutil.EstimateReasons(t)

	for _, reason := range htmlreport.ReasonOrder {
		if !slices.Contains(reasons, reason) {
			t.Errorf("reasonOrder lists estimate.Reason %q, which the domain does not declare; want one of %v",
				string(reason), reasons)
		}
	}
}
