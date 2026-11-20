package markdown

// The report's own words about a reason and the order it names them in are
// unexported: only renderNotEstimated and notEstimatedReasons use them, and the
// wording stays in this adapter (CLAUDE.md, ubiquitous language: the domain
// gives reasons, not sentences). The black-box test reaches them through these
// aliases, which exist in test builds alone (issue #308).

// ReasonOrder is reasonOrder, for the tests: the order a total names the
// reasons its resources are not estimated in.
var ReasonOrder = reasonOrder

// RenderNotEstimated is renderNotEstimated, for the tests: the phrase a report
// gives an estimate that is not estimated.
var RenderNotEstimated = renderNotEstimated
