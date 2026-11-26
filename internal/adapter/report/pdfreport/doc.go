// Package pdfreport renders an assessment, or a comparison of several targets'
// assessments, as a PDF for people to keep and pass on (PRD #584, ADR 0049). It
// implements the analysis.Reporter and analysis.ComparisonReporter ports, with
// a small deterministic PDF writer of its own built on the standard library.
//
// # The file
//
// The file is PDF 1.7 (ISO 32000-1), A4 portrait (595 × 842 points), and every
// byte of it is a line feed or printable ASCII. It holds a catalog, a flat page
// tree, three font objects, an /Info dictionary whose one entry is the constant
// /Title "shift2eu migration assessment", and one page object and one
// uncompressed content stream per page, then the cross-reference table, whose
// entries are exactly 20 bytes and end in a space and a line feed, and the
// trailer with /Size, /Root and /Info only. It holds no date, no /ID, no
// /Producer, no link, action, JavaScript, form, embedded file or embedded font,
// and every name in it is a constant of the writer with no '#'.
//
// Text uses three of the PDF's standard core fonts with /WinAnsiEncoding:
// Helvetica for the report's own words, Helvetica-Bold for headings and table
// header cells, and Courier for every value from the inventory, a catalog, the
// report metadata or the version line. Lines are wrapped with the fonts'
// standard character widths; all layout arithmetic is integer, in thousandths
// of a point, so the bytes are the same on every release target.
//
// A content stream holds only the hairlines under the page header, above the
// footer and under table header rows (re and S at line width 0, inside q and
// Q), and one text object of runs, each placed with Tm and drawn with Tj, the
// font selected with Tf. Every operand is an integer, a literal string or a
// font resource name /F1, /F2 or /F3.
//
// # Values
//
// Every value is escaped twice. escapeValue first turns it into the text the
// PDF shows: every character the core fonts show through WinAnsiEncoding is
// kept (printable ASCII, the 27 characters of 0x80 to 0x9F and the Latin-1
// Supplement from U+00A1, except U+00AD), a backslash is doubled, every other
// rune is written as \u and four lowercase hex digits or \U and eight, and
// every byte of invalid UTF-8 as \x and two; no rune is ever dropped, replaced
// or cut off. pdfString then writes that text only inside a literal string of a
// Tj, with \( \) \\ and a three-digit octal escape for every byte outside 0x20
// to 0x7E, so a value can never end its string, start an operator or add an
// object. A value is never written as a name, a number, a dictionary entry or
// a stream length.
//
// A value wraps at the boundaries of its escaped form, never inside an escape,
// and each of its lines is a Tj of its own, at its column's left edge or at the
// start of the line. A value that does not fit in what is left of a line
// starts the next one. Only an escape wider than its whole column would be
// split.
//
// # Layout
//
// Every page has a header, "shift2eu migration assessment: " and the target's
// display name or "comparison of N targets", and a footer with the version
// line New was given and "Page N of M". A table that continues on a new page
// repeats its header row; a row stays on one page unless it is taller than a
// page, and is then split between two lines. A heading never ends a page: it
// moves to the next page with the block that follows it when that block does
// not fit under it, and a heading taller than a page, which only a very long
// value makes it, is drawn at the body size as a paragraph.
//
// A single-target report is: the title "Migration assessment: " and the
// target, the header facts (inventory file, left out when the metadata names
// none; source; catalog snapshot URL and UTC retrieval date; region, "chosen
// with --region" or the target's default for estimates), then Summary,
// Resources (ID, category, fit, best offering with its tie note and, with
// estimates on, plan and monthly estimate), Required changes, Blockers,
// Workloads (only when the assessment shows workloads), Cost estimate (only
// with estimates on), Resource details (per resource: raw type, category,
// region, workload, fit, best offering, relations and "affected by a blocker
// on" notes, every candidate offering with its fit and missing capabilities,
// the reasons, the required changes, the blockers and, with estimates on, the
// estimate's breakdown), How this report is worked out, and About this report
// (the version line, every catalog snapshot with its source URL, retrieval
// date and default region for estimates, the regions, "Estimates: on" or
// "Estimates: off", the inventory file and the source). The words follow the
// Markdown report's, amounts included ("€2,500.00").
//
// A comparison report is: the title naming every target, the header facts with
// one catalog and one region line per target, then Comparison (a Summary table
// with one column per target, a Resources table whose cell per target stacks
// its fit, best offering and estimate, and Blockers on every target), one
// section per target headed by its display name holding its sections from
// Summary to Resource details one heading level lower, and How this report is
// worked out and About this report once. It ranks no target. A comparison with
// no target, or whose targets assess different resources, is an error, and
// nothing is written.
//
// The bytes depend only on the assessment, the report metadata and the version
// line: never on the clock, the environment or map iteration. A report is
// built whole before it is written to the io.Writer in one call.
package pdfreport
