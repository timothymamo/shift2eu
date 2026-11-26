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
//   - h3 sections: Summary, one box per fit (its badge, its count, and "of N
//     resources", "needs changes" or "blocked") and a monthly-estimate box;
//     Resources, the Markdown table's columns, its Plan cell the plan's name
//     alone and its Monthly estimate cell the amount or "not estimated" alone
//     (issue #373, round 2, item 04: the detail stays in the downloads);
//     Required changes and Blockers, both in one box; Workloads, one box per
//     workload (its h4 name, a bubble per fit showing its count, whose fit a
//     screen reader reads, and its estimate in euros); and Cost estimate, the
//     total and one closing line. A global offering is marked "(global)" in
//     the Resources table's best-offering cell (the best one and each tie)
//     and in a required change's "(not offered by X)", as in the Markdown
//     report, and the first Resources table that shows one is followed by
//     the one paragraph that says what the mark means (ADR 0048: the
//     Markdown report's note, served from locations that may be outside
//     the EU), also in a
//     comparison, under the first target section that shows one. The
//     fragment names the source's cost
//     nowhere (round 3, item 04): the Markdown and JSON reports keep it;
//   - for a comparison, the title naming every target, the header line with
//     one catalog link per target, then one h3 section per target holding its
//     single-target sections, one level lower, its Summary boxes included
//     (round 2, item 10), and nothing else: no combined section, no table
//     with a column per target and no list of blockers on every target
//     (round 3, item 05), which the Markdown report keeps. No table names a
//     region.
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
// the kind one of summary, resources, changes, blockers, workloads, cost and
// target; every Fit cell and fit badge has "fit fit-direct",
// "fit fit-partial" or "fit fit-none", chosen from the mapping's fit by an
// exhaustive switch ("fit" alone for a fit it does not know), and a badge
// outside a table shows that fit's glyph in a span hidden from screen
// readers; a workload bubble's fit word sits in a span of "visually-hidden",
// the site stylesheet's class for words only a screen reader reads; the Cost
// estimate's total has "total"; and the box every table sits in, which
// scrolls sideways on its own, has "table-scroll". The classes are fixed
// tables keyed by unexported enums, never built from a value, and render
// refuses a key outside them.
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
// enum, like the classes. The one other role is the table box's: each box of
// class "table-scroll" scrolls sideways, so a keyboard visitor must reach it,
// and it carries the constant attributes tabindex="0", role="region" and an
// aria-label from a fixed table of names keyed by an unexported enum
// ("Resources table"), never from a value (issue #373,
// review 6). No other element carries a role.
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
// references, so it never ends the attribute. Before that, invalid UTF-8
// and control characters become U+FFFD, and tabs and line endings spaces,
// so a value the domain would refuse still reaches the page as valid text.
//
// The package does not use html/template: its reflection keeps every
// exported method of the program in the site's module, which grows by a third
// and goes over the module's size budget (docs/adr/0035). Its goldens were
// first written by an html/template version and are reproduced byte for byte.
//
// The fragment's bytes depend only on the assessment and the report metadata:
// never on the clock, the environment or map iteration.
//
// # Tests beyond this package
//
// The package's tests also hold the whole assess path against the scheduled
// security audit's hostile-inventory corpus (docs/adr/0040, issue #661): the
// inventory decoder's fuzz target FuzzInventoryDecoding, which fuzzes the
// inventory file's name too (issue #671), the corpus's decisions and Markdown,
// JSON and HTML goldens under testdata/hostile-corpus/, and the hostile file
// names' goldens under testdata/hostile-inventory-names/.
// They live here because depguard lets no other package's tests import this
// reporter outside the web root.
package htmlreport
