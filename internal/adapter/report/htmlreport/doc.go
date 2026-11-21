// Package htmlreport renders an assessment, and a comparison of several
// targets' assessments, as an HTML fragment: the report the site shows in its
// page (PRD #297, docs/adr/0035). It implements the analysis.Reporter and
// analysis.ComparisonReporter ports, beside the markdown and jsonreport
// reporters, whose reports are the site's downloads.
//
// # Layout
//
// A fragment holds the sections of the Markdown report, in the same order
// (the markdown package documents them), one heading level lower, as the page
// holds the fragment under its own h1, laid out as the site's mock-up has it
// (issue #373, docs/adr/0035, amendment of 2026-11-21):
//
//   - an h2 title, the words "Migration assessment" and the target, then one
//     header line, a list: "source <source> · <inventory file> · catalog
//     retrieved <date> · region <region>", the file left out when it has no
//     name, the region when there is none, and the date a link to the
//     catalog's own page;
//   - h3 sections: Summary, one box per fit (its badge, its count, the total)
//     and a monthly-estimate box; Resources, the Markdown table's columns
//     with a Region column it does not have; Required changes and Blockers,
//     both in one box; Workloads, one box per workload (its h4 name, a badge
//     per fit with its count, its estimate and the source's cost); and Cost
//     estimate, the total and one closing line;
//   - for a comparison, the title naming every target, the header line with
//     one catalog link per target, an h3 Comparison section holding the h4
//     Summary table, Resources (with a Region column) and Blockers on every
//     target, then one h3 section per target holding its single-target
//     sections, one level lower.
//
// A fragment is semantic HTML, one section element holding the title, its
// list and part sections (below) of headings, paragraphs, lists, tables with
// header cells, classless section boxes and the inline elements of a badge, a
// figure and the catalog's link, to be inserted into the page, never a whole
// document: no doctype, html, head or body element, no div, script, style,
// image or form. It ends with a single newline.
//
// # Classes
//
// So the site's stylesheet can draw the report, which cannot select by text,
// the fragment carries constant classes from closed sets (docs/adr/0035,
// amendment for issue #373): each h3 section (and each section of a target
// in a comparison) sits in a section element of class "part part-<kind>",
// the kind one of summary, resources, changes, blockers, workloads, cost,
// comparison and target; every Fit cell and fit badge has "fit fit-direct",
// "fit fit-partial" or "fit fit-none", chosen from the mapping's fit by an
// exhaustive switch ("fit" alone for a fit it does not know), and a badge
// outside a table shows that fit's glyph in a span hidden from screen
// readers; a comparison's Summary table's rows have "fit-direct",
// "fit-partial", "fit-none" and "fit-total"; the Cost estimate's total has
// "total"; and the box every table sits in, which scrolls sideways on its
// own, has "table-scroll". The classes are fixed tables keyed by unexported
// enums, never built from a value, and render refuses a key outside them.
//
// # List role
//
// The site's stylesheet strips the markers of two lists: the header line
// below the h2 (what was assessed against what, in a report and a comparison
// alike), and the Blockers list (at h3 and h4 level alike). Safari and
// VoiceOver drop the list role and the item count of a list drawn without
// markers, so those lists, and only they, carry the
// constant attribute role="list" (docs/adr/0035, amendment for
// issue #373), from a fixed table of list start tags keyed by an unexported
// enum, like the classes; no other element carries a role, and "list" is the
// only role value.
//
// # Values are text
//
// The report is built as blocks of segments, each the report's own words or a
// value from the assessment or the metadata (IDs, raw types, regions, offering
// names, capabilities, the inventory name), which a fragment shows inside a
// code element. Every element and attribute the fragment holds is a constant
// of this package; a value is only ever written as element content, never into
// a tag, an attribute, a script or a style, and it is escaped as html/template
// escapes text there: & < > " ' + become character references. The one
// exception is the catalog's link (issue #373): the snapshot's source URL is
// written into its href, only when it is an https URL (the words alone
// otherwise), and escaped by the same escaper, which turns both quotes into
// references, so it never ends the attribute. Before that, invalid UTF-8 and control
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
