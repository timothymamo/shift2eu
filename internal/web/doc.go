// Package web is the driving adapter the site's page calls (PRD #297), beside
// internal/cli and internal/httpapi: it takes an inventory document and the
// options the user chose in the page, runs the assessment, or the comparison
// of several targets, and returns the Markdown report and the JSON report the
// page offers as downloads, byte for byte what `shift2eu assess` writes for the
// same inventory and options, and the HTML fragment the page shows.
//
// It parses the values the page supplies (Options) and checks them by the
// application's option rules (analysis.SelectTargets and its siblings), the
// rules the CLI checks its flags by, so both accept the same options and
// refuse the rest with the same messages. An error is an *Error whose Kind
// tells a usage error (the options) from an inventory error (the document the
// inventory reader refused, named by position and field, never by a value it
// holds) and an internal one.
//
// It gets every adapter through Deps (the inventory reader over bytes, the
// target catalogs, the Markdown, JSON and HTML reporters) from its composition
// root, cmd/shift2eu-web, whose wire package builds them, and constructs none.
// It imports only the application, the domain, internal/version and a named
// list of standard packages: no os, no net and no syscall/js, which only
// cmd/shift2eu-web, the thin glue between the page's script and this package,
// imports. It runs natively too, which is how its tests run.
//
// One assessment reads the inventory once and renders every format from the
// same assessment: the use case is given one reporter that renders the
// Markdown report, the JSON report and the HTML fragment in turn, each into its
// own buffer, so the downloads are the bytes the CLI's reporters write for the
// same options. The inventory source Deps gives is wrapped so that a document
// the reader refuses is told apart from every other failure (KindInventory)
// however the use case wraps the error; its message is the reader's, without
// the use case's "load inventory: " prefix (docs/adr/0035).
package web
