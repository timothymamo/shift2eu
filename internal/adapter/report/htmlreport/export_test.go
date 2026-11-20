package htmlreport

import (
	"strings"

	"github.com/timothymamo/shift2eu/internal/domain/estimate"
)

// The report's own words about a reason and the order it names them in are
// unexported: only renderNotEstimated and notEstimatedReasons use them, and the
// wording stays in this adapter (CLAUDE.md, ubiquitous language: the domain
// gives reasons, not sentences). The black-box test reaches them through these
// aliases, which exist in test builds alone (issue #308).

// ReasonOrder is reasonOrder, for the tests: the order a total names the
// reasons its resources are not estimated in.
var ReasonOrder = reasonOrder

// NotEstimatedPhrase is renderNotEstimated, for the tests: the phrase a report
// gives an estimate that is not estimated, as one plain string, a value the
// report shows as code in backticks, so a test can state the phrase in one line
// the way the Markdown report's own phrase is stated.
func NotEstimatedPhrase(e estimate.Estimate) string {
	var phrase strings.Builder

	for _, segment := range renderNotEstimated(e) {
		if segment.Code {
			phrase.WriteString("`" + segment.Text + "`")

			continue
		}

		phrase.WriteString(segment.Text)
	}

	return phrase.String()
}
