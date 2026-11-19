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
package jsscan
