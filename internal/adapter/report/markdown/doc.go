// Package markdown renders an assessment as a human-readable Markdown report, and
// a comparison of several targets' assessments as one Markdown comparison
// report. It implements the analysis.Reporter and analysis.ComparisonReporter
// ports.
//
// # Layout
//
// A report is, in this order:
//
//   - the title, "# Migration assessment: " and the target;
//   - the header lines: the inventory file name (left out when the name is
//     empty), the source, the catalog snapshot's source URL and retrieval
//     date, as a UTC calendar date, and the region: "chosen with --region" when
//     the assessment's Region is set, else the estimate region as "the target's
//     default for estimates", and no line when neither is set;
//   - "## Summary": a table of the number of resources per fit, and in total;
//   - "## Resources": one table row per verdict, in the assessment's order, with
//     the ID, raw type, category, fit, best offering, then, with estimates on,
//     the plan (name and size, or "no plan large enough" and the largest plan)
//     and the monthly estimate (the amount, any fixed fee it includes, then
//     "(count assumed 1)" and "(730 hours assumed)" when the resource gives no
//     count or no hours, or "not estimated: " and the reason in plain words,
//     the region named for a missing price or storage price), then, when some
//     resource has a relation, its relations, and last the reasons. The best
//     offering is followed by "(tie: ...)" naming, in candidate order, the other
//     candidates with the best one's fit and as many missing capabilities;
//   - "## Required changes": one list item per partial verdict with required
//     changes, in the assessment's order, "ID: capabilities (not offered by
//     offering)", or "None." when there is none;
//   - "## Blockers": one list item per blocker, "ID: reason", then one item per
//     "affected by a blocker on ID" note (assessment.Verdict.AffectedBy), or
//     "None." when there is no blocker;
//   - "## Workloads", only when assessment.Assessment.ShowWorkloads: one "###"
//     subsection per workload group, "No workload" last, with its fit counts,
//     with estimates on its total and the source's cost per currency, and its
//     blockers;
//   - "## Cost estimate", only with estimates on: the total "for N of M
//     resources" ("resource" when M is 1), the basis (list prices on the retrieval date, excluding VAT
//     and discounts, a fixed fee as the offering's listed fee), the region the
//     prices are for, the hours basis, followed by one clause per distinct
//     billing cap of the estimated resources' plans, ascending (ADR 0029), and
//     the source's cost per currency, never converted.
//
// Estimates are on exactly when the assessment's Total is set
// (assessment.AssessWith). With estimates off and no relation and no workload,
// the report is byte for byte the report before estimates existed. Amounts are
// estimate.Money.String with a thousands separator and the currency's sign
// (EUR, USD, GBP; any other currency by its code alone).
//
// The report ends with a single newline. Its bytes depend only on the
// assessment and the report metadata: never on the clock, the environment or
// map iteration.
//
// # Comparison layout
//
// A comparison report is, in this order:
//
//   - the title, "# Migration assessment: " and every target's display name, in
//     the comparison's order (target key order);
//   - the header lines: the inventory file name (left out when the name is
//     empty), the source once, taken from the first target, and one catalog line
//     per target with its snapshot's source URL and retrieval date, then one
//     region line per target that has a region, named by display name;
//   - "## Comparison", holding "### Summary", a table of the counts per fit with
//     one column per target; "### Resources", one table row per resource in the
//     first target's verdict order, which is the inventory's, with the ID, the
//     category and each target's fit and best offering, without the tie note,
//     and, for a target with estimates, its monthly estimate; the summary gains
//     one estimate row per target with estimates, its total in its own column,
//     and the basis of the totals; and "### Blockers on every target", one list item per shared blocker with
//     a nested item per target naming it by display name, never by key, and
//     giving its reasons, or "None.";
//   - one "## " section per target, headed by its display name, holding that
//     target's single-target report from "Summary" to "Blockers", then its
//     Workloads and Cost estimate sections when it has them, every heading one
//     level lower.
//
// The comparison ranks no target and recommends none. Values are escaped as in
// the single-target report.
//
// A comparison must hold at least one target, every target must assess the same
// resources in the same order, the one inventory's, and every shared blocker
// must name only compared targets. A comparison that breaks any of these is an
// error, and nothing is written.
//
// # Values are code spans
//
// Every value taken from the assessment or the metadata is untrusted: the
// target, the source, the source URL, the inventory file name, IDs, raw types,
// categories, offering names and capabilities, in reasons too. Each is an inline
// code span, the one form every renderer shows literally:
//
//   - Backslash escapes are not enough. GitHub resolves them first and then runs
//     its math, mention, reference, emoji, task-list and bare-link filters over
//     the text; those filters skip code.
//   - An autolink is not enough either. Renderers decode the visible text of
//     <...>, so a stored %E2%80%AE or &#8238; would show as a right-to-left
//     override and %0A as a line break. The source URL is the stored string,
//     never decoded and never percent-encoded.
//
// The fence is one backtick longer than the longest backtick run in the value,
// the value is padded with a space when it starts or ends with a backtick or a
// space, a line ending becomes a space as CommonMark would make it, and inside a
// table cell a pipe is written as \|. An empty value renders as nothing, never
// as an empty code span.
//
// The report's own words are plain text: fits, counts, the date and the literal
// text of reasons. Literal text and fits still get a backslash before each of
// \ ` * _ [ ] ( ) < > | ! ~ # & and have line endings turned into character
// references, so a reason code the domain does not know cannot become syntax.
//
// Reasons are rendered from mapping.Reason.Parts, never from String, and joined
// with "; ". Domain values never reach fmt.
package markdown
