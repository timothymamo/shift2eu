//go:build js && wasm

// Command shift2eu-web is the site's WebAssembly module (PRD #297): the
// assessment of `shift2eu assess`, run in the user's browser, so no inventory
// leaves it. It is built with Go's standard toolchain, GOOS=js GOARCH=wasm.
//
// main is the composition root and the thin glue between the page's script
// and internal/web: it takes the adapters from the wire package, which builds
// them, and registers one JavaScript function, shift2euAssess (nameAssess),
// that converts the page's values to web.Options, calls web.Assessor.Assess
// and converts the result back. It holds no assessment logic. It is the only
// package that imports syscall/js, and it imports only wire, internal/web,
// syscall/js and a named list of standard packages: never internal/cli,
// internal/httpapi, a request decoder or a cloud SDK.
//
// The glue reads, sets and calls JavaScript names only through get, set and
// call, which take a jsName: the constants of type jsName are its named allow
// list, and it never computes a name (PRD #297, Layer 1). It refuses, as a
// usage failure with a constant message, targets or regions that are not
// real arrays (Array.isArray), have more than maxEntries (64) entries, or
// hold entries of the wrong type, and any mistyped option. The bound only
// stops a hostile length before any entry is read; any realistic list reaches
// the option rules, whose message is the CLI's usage error. A panic during an assessment is recovered and returned as an
// internal failure with a constant message, so the page stays usable. A
// JavaScript exception is not a panic: a getter in the options object that
// throws while the glue reads it unwinds through Value.Get, which syscall/js
// cannot catch, so shift2euAssess throws to its caller instead of returning
// an internal failure. The page's own call catches it and shows a constant
// message (ADR 0036).
//
// The trust boundary of the returned object: error and kind are plain text,
// which the page shows with textContent and never as markup; markdown and json
// are download blobs, never inserted as HTML; only html is markup, the HTML
// reporter's fragment. None of this can be tested natively (the package builds
// only for js && wasm); milestone 2's browser smoke test pins it (ADR 0035).
package main
