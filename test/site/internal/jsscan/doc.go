// Package jsscan lexes JavaScript and holds one script to the page-script rules
// of PRD #297's first trust layer.
//
// The site's page script, site/page.js, is the one hand-written script the
// bundle ships (the site's Scope and Decisions forbid a framework, npm and any
// JavaScript build tool). "Nothing leaves the browser" therefore rests on that
// script being readable and reviewed, and on a static rule that refuses every
// browser API through which a script can send what it holds, every way of
// naming one at run time, and every way of reaching another window or browsing
// context (PRD #297, Layer 1). This package is that rule, as a test helper:
// test/site scans the page script with it, and its own tests show every rule
// firing on a hostile snippet beside the real page.js it accepts.
//
// It is a helper package of the site's tests, not part of the product: nothing
// but test/site imports it, Go's internal rule keeps every other tree out, and
// no composition root, adapter or domain package may import it.
//
// Tokens is the lexer: it needs comments, string and template literals and
// regular expression literals, because a name in any of those is text, not
// code, and because a template's expression is code again. Scan is the rule
// set, which needs function scopes: three banned APIs and the one HTML sink
// are allowed inside one named function each, and nowhere else.
//
// Both fail closed. The lexer reads ASCII code only, and refuses a backslash
// outside a literal (an escaped identifier such as fetch names fetch
// without spelling it), an unterminated literal or comment, and a bracket that
// pairs with none; Scan returns that error rather than a pass.
//
// The rules (rules.go), each a violation that names the construct it fired on:
//
//   - every name of bannedNames anywhere in the code: the APIs that send data
//     or load a resource, store data, reach another window or browsing
//     context, write markup or run a string as code; every member of Object
//     and Reflect, named in full (Object.assign); and an assignment to a
//     navigation property (href, src, action, ...) or to name, on any object;
//   - a computed member access x[...] and a destructuring assignment, which
//     could name any of those where no scan sees it;
//   - addEventListener with an event name that is no plain string literal, or
//     that is message or messageerror;
//   - the exceptions outside their function or form: in guardFraming, one
//     window.top !== window.self and one window.opener = null; in download,
//     const url = URL.createObjectURL(...), document.createElement("a"),
//     link.href = url and link.click(); in unregisterServiceWorkers,
//     navigator.serviceWorker.getRegistrations(); in insertReport, one
//     target.innerHTML = ... whose value names only html, the policy and
//     createHTML; in copyCommand, the inventory page's copy function (issue
//     #373), one navigator.clipboard.writeText(...) called in place, and no
//     other use of clipboard or writeText anywhere. A second declaration of
//     one of these functions is refused, and so is every use of navigator
//     but a member base (navigator. or navigator?.), the right operand of in
//     and a member name, since any other could bind a stand-in for the
//     browser's navigator these rules key on;
//   - Trusted Types: exactly one trustedTypes.createPolicy("shift2eu-report",
//     ...), its binding never assigned again, and createHTML called only by
//     insertReport;
//   - the assessment result's fields: error and kind only as
//     element.textContent = result.error; or under typeof, markdown and json
//     only as new Blob([result.markdown], ...), html only as
//     insertReport(target, result.html) or under typeof;
//   - the module global: read once, as const x = globalThis.__shift2euModule;,
//     checked with typeof x !== "string" and deleted.
package jsscan
