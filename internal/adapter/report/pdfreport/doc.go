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
// Helvetica for the report's own words, Helvetica-Bold for headings, table
// header cells, badge words, tile figures and the banner's eyebrow, heading
// and address, and Courier for every value from the inventory, a catalog, the
// report metadata or the version line. Lines are wrapped with the fonts'
// standard character widths; all layout arithmetic is integer, in thousandths
// of a point, so the bytes are the same on every release target.
//
// A content stream holds its shapes inside q and Q, then one text object of
// runs, and uses exactly ADR 0049's sixteen operators: q Q h f S BT ET, w, rg
// RG, re, m l, Tf, Tm and Tj. A shape is pathed with re (four integers, whole
// points) or m and l (decimals) and closed with h, then filled with f or
// stroked with S; a run is placed with Tm (six integers) and drawn with Tj, its
// font selected with Tf (/F1, /F2 or /F3 and an integer size). A colour (rg,
// RG) or a line width (w) is written only when it changes, as a decimal: ASCII
// digits, a dot and exactly three digits, never a sign or an exponent. Every
// colour is one of the site's light-theme tokens in site/style.css (colour.go),
// each component written as (byte × 1000 + 127) / 255 thousandths. Each page's
// /Resources holds only /Font.
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
// not fit under it, and a heading too tall to fit a page with the room kept
// under it and the headings kept above it, which only a very long value makes
// it, is drawn at the body size as a paragraph.
//
// # Look
//
// The report has the site's look (PRD #584 milestone 1b, The PDF, Look), drawn
// with rectangles and straight-line paths only. The title sits on a --mast band
// with a --gold rule at its left; the header facts, the page header and the
// footer are --muted, the furniture set off by --ring rules. A section heading
// is --accent under a --ring rule; a comparison's per-target heading is --ink
// with a --gold rule at its left; a Resource details block's ID lies on a
// --surface-2 band. A table's header row lies on --surface-2, every even row on
// --bg, and a --ring rule runs under every row.
//
// Every fit shown in a table cell, a stacked comparison cell, a summary tile
// or a workload card is a badge: a fill in the fit's -bg colour holding its
// icon and its word, "Direct", "Partial" or "None" (after a count on a
// workload card), both in the fit's -ink colour. The icons are vector paths:
// a check mark (a stroked polyline of three points), a half-filled disc (a
// stroked 16-sided polygon, its vertices a constant integer table, with its
// left half filled) and a cross (two stroked lines). The word always stands
// beside colour and icon, so a fit reads the same in greyscale.
//
// The single-target summary is a row of tiles, one per fit, the total and,
// with estimates on, the monthly estimate, each with a 0.5 pt --ring outline,
// a 3 pt rule along its top and a big Helvetica-Bold figure in its colour.
// Required changes stand in an --info-bg box with an --info-rule left rule;
// blockers, and a comparison's blockers on every target, in a --warn-bg box
// with a --warn-rule left rule and a filled warning triangle, holding a white
// exclamation mark, before its --warn-ink heading. Each workload is a --bg card
// with a --ring outline.
//
// The contact banner, the site's "Planning a migration?" band, stands once per
// document: in a single-target report after the last of its sections from
// Summary to Cost estimate and before Resource details, in a comparison after
// the Comparison section and before the first per-target section. It is a
// --mast fill across the text width with a 3 pt --gold rule along its top,
// holding the eyebrow, the heading and the text in the site's words and the
// address info@shift2eu.com in a --gold box: plain text on filled boxes, with
// no link, annotation or action.
//
// A box (the required-changes and blockers boxes, a workload card, the title
// band) whose heading and first line do not both fit in the space left starts
// on the next page. A box that continues on a new page is drawn again there,
// its fill, left rule and outline covering its part on each page, and no part
// is shorter than 26 pt, so no box is ever as short as a badge. The banner, the tile row and a workload
// card move whole to the next page when they do not fit; a card taller than a
// page continues as a box does. A table's header row and its first row move to
// the next page together.
//
// A single-target report is: the title "Migration assessment: " and the
// target, the header facts (inventory file, left out when the metadata names
// none; source; catalog snapshot URL and UTC retrieval date; region, "chosen
// with --region" or the target's default for estimates), then Summary,
// Resources (ID, category, fit, best offering with its tie note and, with
// estimates on, plan and monthly estimate), Required changes, Blockers,
// Workloads (only when the assessment shows workloads), Cost estimate (only
// with estimates on), the contact banner, Resource details (per resource: raw type, category,
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
// its fit, best offering and estimate, and Blockers on every target), the
// contact banner, one section per target headed by its display name holding
// its sections from
// Summary to Resource details one heading level lower, and How this report is
// worked out and About this report once. It ranks no target. A comparison with
// no target, or whose targets assess different resources, is an error, and
// nothing is written.
//
// The bytes depend only on the assessment, the report metadata and the version
// line: never on the clock, the environment or map iteration. A report is
// built whole before it is written to the io.Writer in one call.
package pdfreport
