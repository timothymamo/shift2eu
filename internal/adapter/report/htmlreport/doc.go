// Package htmlreport renders an assessment, and a comparison of several
// targets' assessments, as an HTML fragment: the report the site shows in its
// page (PRD #297). It implements the analysis.Reporter and
// analysis.ComparisonReporter ports, beside the markdown and jsonreport
// reporters, whose reports are the site's downloads.
//
// A fragment is semantic HTML (headings, tables with header cells, lists) to
// be inserted into the page, never a whole document: no doctype, html, head or
// body element, no script, style, image or form, and no link that leaves the
// page. It is rendered with html/template, whose contextual escaping makes
// every value taken from the assessment or the metadata (IDs, raw types,
// offering names, capabilities, the inventory name, the snapshot's source URL)
// text, however hostile.
//
// The fragment's bytes depend only on the assessment and the report metadata:
// never on the clock, the environment or map iteration.
//
// Status: a skeleton. Report and ReportComparison return an error and write
// nothing until PRD #297 milestone 1 implements them.
package htmlreport
