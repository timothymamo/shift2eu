// Package jsonreport renders an assessment, or a comparison of several targets'
// assessments, as a stable, versioned JSON document for other tools to consume:
// the JSON report format, schemaVersion 2 or 3 (docs/adr/0018, docs/adr/0030).
// It implements the analysis.Reporter and analysis.ComparisonReporter ports.
// The CLI writes it with --format json (version 3 unless --schema-version 2),
// and the site's page offers the same document as a download.
//
// # Versions
//
// New(version) returns a Reporter that writes Version2 or Version3; any other
// version is refused when it reports, with an error naming the versions it
// writes, and nothing is written. The two versions differ in one rule (issue
// #307): in version 3 a total that estimated nothing (estimated is 0) has no
// amount key, where version 2 writes "0.00". That holds for the estimate
// basis's total, each workload's estimate (the "No workload" group's included)
// and each comparison target's total; their counts stay, and a total that
// estimated something is written as in version 2. A version 2 document is byte
// for byte what earlier releases wrote.
//
// # Documents
//
// Every document is one JSON object with its "schemaVersion" and a "kind":
//
//   - "assessment", from Report: the inventory name, the source, the target and
//     its catalog snapshot, the summary, one resource per verdict, the
//     blockers and the workloads, and, with estimates on, the estimate basis
//     and total and the source cost. Schema: schema/assessment-v3.schema.json
//     (schema/assessment-v2.schema.json for version 2).
//   - "comparison", from ReportComparison: the inventory name, the source, one
//     entry per target in the comparison's order (its key and the single
//     document's target, summary, resources, blockers, workloads, estimate and
//     source cost), and the blockers every target shares. Schema:
//     schema/comparison-v3.schema.json (schema/comparison-v2.schema.json for
//     version 2).
//
// The version 1 schema files stay beside them for documents an earlier release
// wrote. The version 1 and 2 files are frozen: a test pins their SHA-256
// digests (issue #293).
//
// A resource carries its ID, raw type, category, region, fit, best offering,
// the tied offerings and the required changes, the same ones the Markdown
// report shows (assessment.Verdict.Ties and ChangesToList), and its reasons. In
// version 3 only, a resource whose best offering is global (ADR 0048: not
// bound to a region the user chooses, served from the provider's own
// locations, which may be outside the EU) carries "bestOfferingGlobal": true right
// after "bestOffering", and one with global ties carries "globalTies", their
// names in the order of "ties", right after "ties"; each key is absent when it
// would be false or empty (assessment.Verdict.BestOfferingGlobal, GlobalTies),
// and version 2 writes neither. A
// reason carries its code, its plain sentence (mapping.Reason.String, no
// Markdown) and the target, category, offering and capability it mentions,
// each only when set. A resource always carries its relations and its
// "affected by" notes ([] when none); with estimates on it carries its estimate
// (estimated with amount, breakdown and the countAssumed and hoursAssumed
// flags, both always written, and billingCapHours when the plan's offering caps
// its billed hours (ADR 0029), and storageNotIncluded when the estimate leaves
// out storage its plan does not include (ADR 0032), or the reason code when not)
// and the plan the estimate names, if any. A total carries storageNotIncluded,
// the count of such estimates, only when it is above 0.
//
// # Estimates
//
// Estimates are on exactly when the assessment's Total is set
// (assessment.AssessWith). With them off, no estimate, plan, amount or source
// cost key is written anywhere: an absent key cannot be read as zero. The basis
// ("estimate") names the region the prices are for, whether it was chosen, the
// price date, the currency EUR, what is excluded (VAT, discounts), the hours
// basis 730 and the total. Every amount is a decimal string rounded to the
// cent, half away from zero (estimate.Money.String), never a JSON number. A
// total is rounded from the exact sum, not summed from the rounded amounts, so
// it can differ from their sum by a cent or so. The source cost is per currency
// and never converted.
//
// A region chosen for the target (--region, the API's regions) is the
// document's top-level "region", and each comparison target's, with or without
// estimates; with no region chosen the key is absent.
//
// # Encoding
//
// Documents are built from private structs and encoded with encoding/json,
// never by concatenating strings: two-space indent, one trailing newline. Every
// value is a JSON string or number, so a value holding quotes, backslashes,
// control characters or an HTML end tag stays inside its string. encoding/json
// escapes <, >, &, U+2028 and U+2029 and writes every invalid UTF-8 byte as
// U+FFFD, so a document is always valid JSON and valid UTF-8.
//
// Keys are camelCase and every key the schemas require is always present: a
// string that holds nothing is "", an array that holds nothing is [], never
// null. Order is the domain's: resources and blockers in the assessment's
// order, targets in the comparison's, never map order. A document's bytes
// depend only on the version, the assessment, or the comparison, and the
// report metadata.
//
// The whole document is encoded before it is written to the io.Writer in one
// call; a write error is returned, wrapped. A comparison of no target is an
// error, and nothing is written.
package jsonreport
