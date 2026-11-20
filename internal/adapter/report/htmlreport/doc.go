// Package htmlreport renders an assessment, and a comparison of several
// targets' assessments, as an HTML fragment: the report the site shows in its
// page (PRD #297, docs/adr/0035). It implements the analysis.Reporter and
// analysis.ComparisonReporter ports, beside the markdown and jsonreport
// reporters, whose reports are the site's downloads.
//
// # Layout
//
// A fragment holds what the Markdown report holds, in the same order and with
// the same words (the markdown package documents the layout), one heading
// level lower, as the page holds the fragment under its own h1:
//
//   - an h2 title, "Migration assessment: " and the target, then a list of
//     the inventory file name (left out when empty), the source, the catalog
//     snapshot's source URL and retrieval date and the region;
//   - h3 sections: Summary, Resources (with a Region column the Markdown table
//     does not have), Required changes, Blockers, Workloads (each workload an
//     h4) and Cost estimate, each as the Markdown report shows it;
//   - for a comparison, the title naming every target, an h3 Comparison
//     section holding the h4 Summary, Resources (with a Region column) and
//     Blockers on every target, then one h3 section per target holding its
//     single-target sections, one level lower.
//
// A fragment is semantic HTML, one section element holding the title, its
// list and part sections (below) of headings, paragraphs, lists and tables
// with header cells, to be inserted into the
// page, never a whole document: no doctype, html, head or body element, no
// script, style, image, form or link. It ends with a single newline.
//
// # Classes
//
// So the site's stylesheet can draw the report, which cannot select by text,
// the fragment carries constant classes from closed sets (docs/adr/0035,
// amendment for issue #373): each h3 section (and each section of a target
// in a comparison) sits in a section element of class "part part-<kind>",
// the kind one of summary, resources, changes, blockers, workloads, cost,
// comparison and target; every Fit cell has "fit fit-direct", "fit
// fit-partial" or "fit fit-none", chosen from the mapping's fit by an
// exhaustive switch ("fit" alone for a fit it does not know); a Summary
// table's rows have "fit-direct", "fit-partial", "fit-none" and "fit-total";
// the Cost estimate's total item has "total". The classes are fixed tables
// keyed by unexported enums, never built from a value, and render refuses a
// key outside them.
//
// # Values are text
//
// The report is built as blocks of segments, each the report's own words or a
// value from the assessment or the metadata (IDs, raw types, regions, offering
// names, capabilities, the inventory name, the snapshot's source URL), which a
// fragment shows inside a code element. Every element and attribute the
// fragment holds is a constant of this package; a value is only ever written as
// element content, never into a tag, an attribute, a URL, a script or a
// style, and it is escaped as html/template escapes text there: & < > " ' +
// become character references. Before that, invalid UTF-8 and control
// characters become U+FFFD, and tabs and line endings spaces, so a value the
// domain would refuse still reaches the page as valid text.
//
// The package does not use html/template: its reflection keeps every
// exported method of the program in the site's module, which grows by a third
// and goes over the module's size budget (docs/adr/0035). Its goldens were
// first written by an html/template version and are reproduced byte for byte.
//
// The fragment's bytes depend only on the assessment and the report metadata:
// never on the clock, the environment or map iteration.
package htmlreport
