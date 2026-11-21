package jsscan

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// This file holds the page-script rules Scan applies to the tokens (PRD #297,
// Layer 1): the banned names, the computed member access and the destructuring
// assignment that could hide one, and the six exceptions, each confined to
// one named function in one fixed form: the framing guard (guardFraming), the
// download function (download), the unregister function
// (unregisterServiceWorkers), the report's one insertion point (insertReport)
// and the Trusted Types policy it inserts through, the inventory page's one
// clipboard write (copyCommand, issue #373), plus the module global, read
// once, checked and deleted.

// The named functions the exceptions are confined to.
const (
	fnGuardFraming = "guardFraming"
	fnDownload     = "download"
	fnUnregister   = "unregisterServiceWorkers"
	fnInsertReport = "insertReport"
	fnCopyCommand  = "copyCommand"
)

// policyName is the one Trusted Types policy the page creates, which the
// Content Security Policy's trusted-types directive names.
const policyName = "shift2eu-report"

// moduleGlobal is the global the module script sets and the page script reads
// once and deletes.
const moduleGlobal = "__shift2euModule"

// bannedNames are the names the page script may not hold as code anywhere,
// each with why. A name used as an exception (top, opener, serviceWorker,
// createObjectURL, click, innerHTML, ...) is checked by its own rule instead.
var bannedNames = map[string]string{
	// The APIs that send data or load a resource.
	"fetch":                    "it sends a request; the module is not fetched either",
	"fetchLater":               "it sends a request",
	"XMLHttpRequest":           "it sends a request",
	"WebSocket":                "it opens a connection",
	"WebSocketStream":          "it opens a connection",
	"WebTransport":             "it opens a connection",
	"EventSource":              "it opens a connection",
	"sendBeacon":               "it sends a request",
	"RTCPeerConnection":        "WebRTC sends data past the Content Security Policy",
	"webkitRTCPeerConnection":  "WebRTC sends data past the Content Security Policy",
	"RTCDataChannel":           "WebRTC sends data past the Content Security Policy",
	"Worker":                   "a worker runs a script under its own policy",
	"SharedWorker":             "a worker runs a script under its own policy",
	"importScripts":            "it loads a script",
	"register":                 "a service worker registration runs a script under its own policy",
	"Image":                    "an image load sends a request",
	"Audio":                    "a media load sends a request",
	"open":                     "it opens a window, a request or a database",
	"location":                 "a navigation sends data in its URL past the Content Security Policy",
	"navigation":               "a navigation sends data in its URL past the Content Security Policy",
	"navigate":                 "a navigation sends data in its URL past the Content Security Policy",
	"submit":                   "a form submission sends data",
	"requestSubmit":            "a form submission sends data",
	"ping":                     "a hyperlink's ping sends a request",
	"share":                    "it hands data to another application",
	"registerProtocolHandler":  "it hands URLs to another site",
	"bluetooth":                "it sends data to a device",
	"usb":                      "it sends data to a device",
	"serial":                   "it sends data to a device",
	"hid":                      "it sends data to a device",
	"dispatchEvent":            "a synthetic event can follow a link or submit a form",
	"setAttribute":             "it can set an href, a src or an action",
	"setAttributeNS":           "it can set an href, a src or an action",
	"setAttributeNode":         "it can set an href, a src or an action",
	"setAttributeNodeNS":       "it can set an href, a src or an action",
	"createElementNS":          "it creates an element that can load a resource",
	"cookie":                   "the site stores nothing",
	"cookieStore":              "the site stores nothing",
	"localStorage":             "the site stores nothing",
	"sessionStorage":           "the site stores nothing",
	"indexedDB":                "the site stores nothing",
	"caches":                   "the site stores nothing",
	"instantiateStreaming":     "the module is compiled from the bytes of its script, never fetched",
	"compileStreaming":         "the module is compiled from the bytes of its script, never fetched",
	"postMessage":              "it reaches another window or browsing context, which the Content Security Policy does not govern",
	"onmessage":                "a message another window posts is never read",
	"onmessageerror":           "a message another window posts is never read",
	"parent":                   "it reaches another window, which the Content Security Policy does not govern",
	"frames":                   "it reaches another window, which the Content Security Policy does not govern",
	"BroadcastChannel":         "it reaches another browsing context, which the Content Security Policy does not govern",
	"MessageChannel":           "it reaches another browsing context, which the Content Security Policy does not govern",
	"MessagePort":              "it reaches another browsing context, which the Content Security Policy does not govern",
	"outerHTML":                "an HTML sink: the report has one insertion point, insertReport",
	"insertAdjacentHTML":       "an HTML sink: the report has one insertion point, insertReport",
	"write":                    "an HTML sink: the report has one insertion point, insertReport",
	"writeln":                  "an HTML sink: the report has one insertion point, insertReport",
	"srcdoc":                   "an HTML sink: the report has one insertion point, insertReport",
	"setHTMLUnsafe":            "an HTML sink: the report has one insertion point, insertReport",
	"parseHTMLUnsafe":          "an HTML sink: the report has one insertion point, insertReport",
	"createContextualFragment": "an HTML sink: the report has one insertion point, insertReport",
	"DOMParser":                "an HTML sink: the report has one insertion point, insertReport",
	"XSLTProcessor":            "an HTML sink: the report has one insertion point, insertReport",
	"execCommand":              "an HTML sink: the report has one insertion point, insertReport",
	"createScript":             "the one Trusted Types policy creates HTML only",
	"createScriptURL":          "the one Trusted Types policy creates HTML only",
	"defaultPolicy":            "the one Trusted Types policy is shift2eu-report",
	"eval":                     "it runs a string as code, which can name any API",
	"Function":                 "it runs a string as code, which can name any API",
	"constructor":              "it reaches Function, which runs a string as code",
	"prototype":                "it reaches a built-in's methods by another path",
	"__proto__":                "it reaches a built-in's methods by another path",
	"setTimeout":               "it runs a string as code, which can name any API",
	"setInterval":              "it runs a string as code, which can name any API",
	"setImmediate":             "it runs a string as code, which can name any API",
	"import":                   "it loads a module",
	"with":                     "it makes a name resolve on an object at run time",
	"Proxy":                    "it names a property at run time",
	"assign":                   "it navigates (location.assign) or sets a property by a name computed at run time (Object.assign)",
	"getOwnPropertyDescriptor": "it reaches a property by a name computed at run time",
}

// reflectionObjects are the built-ins whose methods name a property at run
// time: every member of them is banned and named in full.
var reflectionObjects = map[string]bool{"Object": true, "Reflect": true}

// navigationProperties are the properties through which a location or an
// <a> navigates or changes its target: assigning any of them, on any object,
// is banned, except the download function's one href.
var navigationProperties = map[string]bool{
	"href": true, "src": true, "action": true, "formAction": true, "search": true, "pathname": true,
	"hash": true, "host": true, "hostname": true, "port": true, "protocol": true,
	"username": true, "password": true,
}

// BannedJSNames returns every name Scan refuses or confines to one place, as a
// list of JavaScript names an allow list may never hold: bannedNames, the
// reflection objects, the navigation properties, and innerHTML and
// createPolicy, which Scan allows only inside one named function (insertReport
// and the Trusted Types policy). Sorted, so two callers compare sets.
//
// A name Scan confines rather than bans is here too: a caller whose code has no
// insertion point and no policy, as the site's module glue (cmd/shift2eu-web)
// has none, must not name it either, so the glue's check shares this set
// instead of keeping a second list that could drift from this one.
func BannedJSNames() []string {
	names := make(map[string]bool, len(bannedNames)+len(reflectionObjects)+len(navigationProperties)+2)

	for name := range bannedNames {
		names[name] = true
	}

	for name := range reflectionObjects {
		names[name] = true
	}

	for name := range navigationProperties {
		names[name] = true
	}

	// The names Scan allows inside one function only: checkInnerHTML,
	// checkCreatePolicy, and checkClipboard and checkWriteText.
	names["innerHTML"] = true
	names["createPolicy"] = true
	names["clipboard"] = true
	names["writeText"] = true

	return slices.Sorted(maps.Keys(names))
}

// assignmentOperators are the operators that assign to their left side.
var assignmentOperators = map[string]bool{
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true, "**=": true,
	"<<=": true, ">>=": true, ">>>=": true, "&=": true, "|=": true, "^=": true,
	"&&=": true, "||=": true, "??=": true,
}

// declarations are the keywords that declare a binding.
var declarations = map[string]bool{"const": true, "let": true, "var": true}

// function is one named function declaration: its name, its parameters and
// the indexes of its body's braces.
type function struct {
	name   string
	params []string
	open   int
	close  int
}

// scanner holds one Scan's tokens and findings.
type scanner struct {
	tokens []Token
	// match maps the index of every bracket to the index of its partner.
	match map[int]int
	// owner is, per token, the innermost named function whose body holds it.
	owner []string
	// functions are the named function declarations, in source order.
	functions []function

	violations []Violation

	// policyVar is the binding of the one Trusted Types policy.
	policyVar string
	// urlVar is the binding of the download function's blob URL.
	urlVar string
	// moduleVar is the binding of the module global's one read.
	moduleVar string

	topUsed, openerUsed, innerHTMLUsed, clipboardUsed bool
	policies                                          int
	moduleReads, moduleDeletes                        int
	moduleSeen                                        bool
}

// newScanner pairs the brackets and finds the named functions, failing on a
// bracket that pairs with none.
func newScanner(tokens []Token) (*scanner, error) {
	s := &scanner{tokens: tokens, match: make(map[int]int), owner: make([]string, len(tokens))}

	var stack []int

	pairs := map[string]string{")": "(", "]": "[", "}": "{"}

	for i, t := range tokens {
		if t.Kind != Punct {
			continue
		}

		switch t.Text {
		case "(", "[", "{":
			stack = append(stack, i)
		case ")", "]", "}":
			if len(stack) == 0 || tokens[stack[len(stack)-1]].Text != pairs[t.Text] {
				return nil, fmt.Errorf("%w: %d:%d: %q closes no bracket", ErrLex, t.Line, t.Col, t.Text)
			}

			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			s.match[open] = i
			s.match[i] = open
		}
	}

	if len(stack) != 0 {
		t := tokens[stack[len(stack)-1]]

		return nil, fmt.Errorf("%w: %d:%d: %q is never closed", ErrLex, t.Line, t.Col, t.Text)
	}

	for i := range tokens {
		if !s.isIdent(i, "function") || s.tok(i+1).Kind != Ident || !s.isPunct(i+2, "(") {
			continue
		}

		closeParen := s.match[i+2]
		if !s.isPunct(closeParen+1, "{") {
			continue
		}

		fn := function{name: tokens[i+1].Text, open: closeParen + 1, close: s.match[closeParen+1]}

		for j := i + 3; j < closeParen; j++ {
			if tokens[j].Kind == Ident && (s.isPunct(j-1, "(") || s.isPunct(j-1, ",")) {
				fn.params = append(fn.params, tokens[j].Text)
			}
		}

		s.functions = append(s.functions, fn)

		// Functions come in source order, so an inner one overwrites its
		// outer one's name over its own body.
		for j := fn.open; j <= fn.close; j++ {
			s.owner[j] = fn.name
		}
	}

	return s, nil
}

// tok returns the token at i, or a token of no kind outside the script.
func (s *scanner) tok(i int) Token {
	if i < 0 || i >= len(s.tokens) {
		return Token{Kind: -1}
	}

	return s.tokens[i]
}

// isPunct reports whether the token at i is the punctuator text.
func (s *scanner) isPunct(i int, text string) bool {
	t := s.tok(i)

	return t.Kind == Punct && t.Text == text
}

// isIdent reports whether the token at i is the identifier text.
func (s *scanner) isIdent(i int, text string) bool {
	t := s.tok(i)

	return t.Kind == Ident && t.Text == text
}

// isMember reports whether the token at i is a property name after a dot.
func (s *scanner) isMember(i int) bool {
	return s.isPunct(i-1, ".") || s.isPunct(i-1, "?.")
}

// isAssigned reports whether the token at i is the left side of an
// assignment.
func (s *scanner) isAssigned(i int) bool {
	t := s.tok(i + 1)

	return t.Kind == Punct && assignmentOperators[t.Text]
}

// isDeclared reports whether the token at i is the name a declaration binds:
// const, let or var before it.
func (s *scanner) isDeclared(i int) bool {
	t := s.tok(i - 1)

	return t.Kind == Ident && declarations[t.Text]
}

// stringIs reports whether the token at i is a string literal of exactly
// value, with no escape.
func (s *scanner) stringIs(i int, value string) bool {
	t := s.tok(i)

	return t.Kind == String && len(t.Text) >= 2 && t.Text[1:len(t.Text)-1] == value
}

// report records a violation at the token at i.
func (s *scanner) report(i int, format string, args ...any) {
	t := s.tok(i)
	if t.Kind < 0 {
		t = Token{Line: 1, Col: 1}
	}

	s.violations = append(s.violations, Violation{Line: t.Line, Col: t.Col, Message: fmt.Sprintf(format, args...)})
}

// sorted returns the violations in source order.
func (s *scanner) sorted() []Violation {
	slices.SortStableFunc(s.violations, func(a, b Violation) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}

		return a.Col - b.Col
	})

	return s.violations
}

// run applies every rule.
func (s *scanner) run() {
	s.checkFunctions()
	s.findPolicy()

	for i, t := range s.tokens {
		switch t.Kind {
		case Ident:
			s.checkIdent(i)
		case Punct:
			s.checkPunct(i)
		default:
			// A literal is text: a banned name in it is no code.
		}
	}

	s.checkPolicyCount()
	s.checkModuleGlobal()
}

// checkFunctions refuses a second declaration of a function an exception is
// confined to: the exception would hold in both.
func (s *scanner) checkFunctions() {
	seen := make(map[string]bool)

	for _, fn := range s.functions {
		switch fn.name {
		case fnGuardFraming, fnDownload, fnUnregister, fnInsertReport, fnCopyCommand:
		default:
			continue
		}

		if seen[fn.name] {
			s.report(fn.open, "the function %s is declared more than once: each exception is confined to one function", fn.name)
		}

		seen[fn.name] = true
	}
}

// findPolicy finds the binding of the first Trusted Types policy, so the
// insertion point may name it wherever it is declared.
func (s *scanner) findPolicy() {
	for i := range s.tokens {
		if !s.isIdent(i, "createPolicy") {
			continue
		}

		for k := i - 1; k >= 2 && !s.isPunct(k, ";"); k-- {
			if s.isPunct(k, "=") && s.tok(k-1).Kind == Ident && s.isDeclared(k-1) {
				s.policyVar = s.tokens[k-1].Text

				return
			}
		}

		return
	}
}

// checkIdent applies the rules for the identifier at i.
func (s *scanner) checkIdent(i int) {
	t := s.tokens[i]
	owner := s.owner[i]

	if t.Text == s.policyVar && s.isAssigned(i) && !s.isDeclared(i) {
		s.report(i, "%s, the Trusted Types policy, is assigned again: the report is inserted through the one policy %s", t.Text, policyName)
	}

	switch t.Text {
	case "top":
		s.checkTop(i, owner)
	case "opener":
		s.checkOpener(i, owner)
	case "serviceWorker":
		s.checkServiceWorker(i, owner)
	case "createObjectURL":
		s.checkCreateObjectURL(i, owner)
	case "click":
		if owner != fnDownload || !s.isMember(i) || !s.isPunct(i+1, "(") || !s.isPunct(i+2, ")") {
			s.report(i, "click() is allowed only in the download function, %s, on the link it creates", fnDownload)
		}
	case "createElement":
		if owner != fnDownload || !s.isMember(i) || !s.isPunct(i+1, "(") || !s.stringIs(i+2, "a") || !s.isPunct(i+3, ")") {
			s.report(i, "createElement is allowed only in the download function, %s, for its one <a download>", fnDownload)
		}
	case "innerHTML":
		s.checkInnerHTML(i, owner)
	case "navigator":
		s.checkNavigator(i)
	case "clipboard":
		s.checkClipboard(i, owner)
	case "writeText":
		s.checkWriteText(i, owner)
	case "createHTML":
		if s.isMember(i) && owner != fnInsertReport {
			s.report(i, "createHTML is called only by the report's insertion point, %s", fnInsertReport)
		}
	case "createPolicy":
		s.checkCreatePolicy(i)
	case "trustedTypes":
		if s.isPunct(i+1, ".") && !s.isIdent(i+2, "createPolicy") {
			s.report(i, "trustedTypes is used only to create the one policy %s", policyName)
		}
	case "error", "kind":
		s.checkPlainTextField(i)
	case "markdown", "json":
		s.checkDownloadField(i)
	case "html":
		s.checkHTMLField(i)
	case moduleGlobal:
		s.checkModuleRead(i)
	case "addEventListener":
		s.checkListener(i)
	case "name":
		if s.isAssigned(i) {
			s.report(i, "an assignment to name is banned: window.name survives a navigation to another origin")
		}
	default:
		s.checkBanned(i)
	}
}

// checkBanned refuses a banned name, a member of a reflection object and an
// assignment to a navigation property.
func (s *scanner) checkBanned(i int) {
	t := s.tokens[i]

	if reflectionObjects[t.Text] {
		if s.isPunct(i+1, ".") && s.tok(i+2).Kind == Ident {
			s.report(i, "%s.%s is banned: it names a property at run time, which no name scan can see", t.Text, s.tokens[i+2].Text)
		} else {
			s.report(i, "%s is banned: its methods name a property at run time, which no name scan can see", t.Text)
		}

		return
	}

	if reason, ok := bannedNames[t.Text]; ok {
		s.report(i, "%s is banned: %s", t.Text, reason)

		return
	}

	if navigationProperties[t.Text] && s.isAssigned(i) {
		if t.Text == "href" && s.allowedHref(i) {
			return
		}

		s.report(i, "an assignment to %s is banned: a location or an <a> navigates or changes its target through it; the download function's one href to its blob URL is the only exception", t.Text)
	}
}

// allowedHref reports whether the href assignment at i is the download
// function's: link.href = url, url being its blob URL.
func (s *scanner) allowedHref(i int) bool {
	return s.owner[i] == fnDownload && s.isMember(i) && s.tok(i-2).Kind == Ident &&
		s.isPunct(i+1, "=") && s.urlVar != "" && s.isIdent(i+2, s.urlVar) && s.isPunct(i+3, ";")
}

// checkTop allows the framing guard's one comparison, window.top !==
// window.self.
func (s *scanner) checkTop(i int, owner string) {
	form := s.isIdent(i-2, "window") && s.isPunct(i-1, ".") && s.isPunct(i+1, "!==") &&
		s.isIdent(i+2, "window") && s.isPunct(i+3, ".") && s.isIdent(i+4, "self")

	if owner == fnGuardFraming && form && !s.topUsed {
		s.topUsed = true

		return
	}

	s.report(i, "top is allowed only once, in the framing guard %s, as window.top !== window.self", fnGuardFraming)
}

// checkOpener allows the framing guard's one assignment, window.opener = null.
func (s *scanner) checkOpener(i int, owner string) {
	form := s.isIdent(i-2, "window") && s.isPunct(i-1, ".") && s.isPunct(i+1, "=") &&
		s.isIdent(i+2, "null") && s.isPunct(i+3, ";")

	if owner == fnGuardFraming && form && !s.openerUsed {
		s.openerUsed = true

		return
	}

	s.report(i, "opener is allowed only once, in the framing guard %s, as window.opener = null;", fnGuardFraming)
}

// checkServiceWorker allows the unregister function's
// navigator.serviceWorker.getRegistrations().
func (s *scanner) checkServiceWorker(i int, owner string) {
	form := s.isIdent(i-2, "navigator") && s.isPunct(i-1, ".") && s.isPunct(i+1, ".") &&
		s.isIdent(i+2, "getRegistrations") && s.isPunct(i+3, "(")

	if owner == fnUnregister && form {
		return
	}

	s.report(i, "serviceWorker is allowed only in the unregister function %s, as navigator.serviceWorker.getRegistrations()", fnUnregister)
}

// checkCreateObjectURL allows the download function's
// const url = URL.createObjectURL(blob).
func (s *scanner) checkCreateObjectURL(i int, owner string) {
	if owner != fnDownload || !s.isIdent(i-2, "URL") || !s.isPunct(i-1, ".") || !s.isPunct(i+1, "(") {
		s.report(i, "createObjectURL is allowed only in the download function, %s", fnDownload)

		return
	}

	if s.isPunct(i-3, "=") && s.tok(i-4).Kind == Ident && s.isDeclared(i-4) {
		s.urlVar = s.tokens[i-4].Text
	}
}

// checkInnerHTML allows the insertion point's one assignment to innerHTML,
// whose value names only its html parameter, the policy and createHTML.
func (s *scanner) checkInnerHTML(i int, owner string) {
	if owner != fnInsertReport || s.innerHTMLUsed || !s.isMember(i) || !s.isPunct(i+1, "=") {
		s.report(i, "innerHTML is an HTML sink: it is allowed once, in the report's insertion point %s, as target.innerHTML = <its html through the policy>", fnInsertReport)

		return
	}

	s.innerHTMLUsed = true

	allowed := map[string]bool{"html": true, "createHTML": true, "null": true}
	if s.policyVar != "" {
		allowed[s.policyVar] = true
	}

	namesHTML := false
	depth := 0

	for j := i + 2; j < len(s.tokens); j++ {
		t := s.tokens[j]

		if t.Kind == Punct {
			switch t.Text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth--
			}

			if depth < 0 || (depth == 0 && (t.Text == ";" || t.Text == ",")) {
				break
			}

			continue
		}

		if t.Kind != Ident {
			s.report(j, "%s inserts only its html parameter: the value %s is no part of it", fnInsertReport, t.Text)

			continue
		}

		if !allowed[t.Text] {
			s.report(j, "%s inserts only its html parameter, through the policy: it may not insert %s", fnInsertReport, t.Text)

			continue
		}

		if t.Text == "html" {
			namesHTML = true
		}
	}

	if !namesHTML {
		s.report(i, "%s's innerHTML takes no html parameter: the report's insertion point inserts the HTML reporter's fragment", fnInsertReport)
	}
}

// checkClipboard allows the copy function's one clipboard write,
// navigator.clipboard.writeText(...) called in place (issue #373): no read, no
// other member, no other object's clipboard, and no second write.
func (s *scanner) checkClipboard(i int, owner string) {
	form := s.isIdent(i-2, "navigator") && s.isPunct(i-1, ".") && s.isPunct(i+1, ".") &&
		s.isIdent(i+2, "writeText") && s.isPunct(i+3, "(")

	if owner == fnCopyCommand && form && !s.clipboardUsed {
		s.clipboardUsed = true

		return
	}

	s.report(i, "clipboard is allowed only once, in the copy function %s, as navigator.clipboard.writeText(...): the page writes one command to the clipboard and never reads it", fnCopyCommand)
}

// checkNavigator allows the name navigator in three uses only (issue #373, the
// audit's finding 4): as a member base (navigator. or navigator?.), as the
// right operand of in, and as a member name after . or ?.. The clipboard and
// service worker rules key on the name, so any other use - a declaration, a
// parameter, a destructuring target, a function or class name, a shorthand
// property, an assignment or the bare value passed on - could bind something
// that stands in for the browser's navigator and passes them. No list of
// binding shapes can be complete, so every other use is refused.
func (s *scanner) checkNavigator(i int) {
	if s.isMember(i) || s.isPunct(i+1, ".") || s.isPunct(i+1, "?.") || s.isIdent(i-1, "in") {
		return
	}

	s.report(i, "navigator is allowed only as a member base (navigator.x), the right operand of in or a member name: any other use could bind a stand-in for the browser's navigator, on which the clipboard and service worker rules key")
}

// checkWriteText allows writeText only as the clipboard's member, called in
// place in the copy function: taken off the member, it could be called from
// anywhere.
func (s *scanner) checkWriteText(i int, owner string) {
	if owner == fnCopyCommand && s.isMember(i) && s.isIdent(i-2, "clipboard") && s.isPunct(i+1, "(") {
		return
	}

	s.report(i, "writeText is allowed only in the copy function %s, as navigator.clipboard.writeText(...)", fnCopyCommand)
}

// checkCreatePolicy allows one Trusted Types policy, of the fixed name.
func (s *scanner) checkCreatePolicy(i int) {
	s.policies++

	switch {
	case s.policies > 1:
		s.report(i, "createPolicy is called more than once: the page creates exactly one Trusted Types policy, %s", policyName)
	case !s.isIdent(i-2, "trustedTypes") || !s.isPunct(i-1, ".") || !s.isPunct(i+1, "(") || !s.stringIs(i+2, policyName):
		s.report(i, "createPolicy must be trustedTypes.createPolicy(%q, ...): the Content Security Policy names that policy and no other", policyName)
	}
}

// checkPolicyCount refuses a script with no policy.
func (s *scanner) checkPolicyCount() {
	if s.policies == 0 {
		s.report(-1, "the script creates no Trusted Types policy: it must call trustedTypes.createPolicy(%q, ...) once, the one policy the report is inserted through", policyName)
	}
}

// checkPlainTextField allows result.error and result.kind only as plain text:
// assigned to textContent, or under typeof.
func (s *scanner) checkPlainTextField(i int) {
	if !s.isMember(i) {
		return
	}

	field := s.tokens[i].Text

	toText := s.tok(i-2).Kind == Ident && s.isPunct(i-3, "=") && s.isIdent(i-4, "textContent") &&
		s.isPunct(i-5, ".") && s.isPunct(i+1, ";")
	typeOf := s.isIdent(i-3, "typeof")

	if toText || typeOf {
		return
	}

	s.report(i, "the result's %s is plain text: it is shown only as element.textContent = result.%s; or tested with typeof", field, field)
}

// checkDownloadField allows result.markdown and result.json only as the one
// part of a Blob: new Blob([result.markdown], ...).
func (s *scanner) checkDownloadField(i int) {
	if !s.isMember(i) {
		return
	}

	field := s.tokens[i].Text

	inBlob := s.tok(i-2).Kind == Ident && s.isPunct(i-3, "[") && s.isPunct(i-4, "(") &&
		s.isIdent(i-5, "Blob") && s.isIdent(i-6, "new") && s.isPunct(i+1, "]")

	if !inBlob {
		s.report(i, "the result's %s is a download's bytes: it goes only into new Blob([result.%s], ...), never into the page", field, field)
	}
}

// checkHTMLField allows result.html only as the insertion point's argument,
// insertReport(target, result.html), or under typeof; and no assignment to
// html.
func (s *scanner) checkHTMLField(i int) {
	if s.isAssigned(i) && !s.isDeclared(i) {
		s.report(i, "an assignment to html is banned: the insertion point inserts the HTML reporter's fragment it is given")

		return
	}

	if !s.isMember(i) {
		return
	}

	toReport := s.tok(i-2).Kind == Ident && s.isPunct(i-3, ",") && s.tok(i-4).Kind == Ident &&
		s.isPunct(i-5, "(") && s.isIdent(i-6, fnInsertReport) && s.isPunct(i+1, ")")
	typeOf := s.isIdent(i-3, "typeof")

	if !toReport && !typeOf {
		s.report(i, "the result's html is markup: it goes only to %s(target, result.html)", fnInsertReport)
	}
}

// checkModuleRead allows the module global's one read,
// const x = globalThis.__shift2euModule;, and its deletion.
func (s *scanner) checkModuleRead(i int) {
	s.moduleSeen = true

	if s.isIdent(i-3, "delete") && s.isIdent(i-2, "globalThis") && s.isPunct(i-1, ".") {
		s.moduleDeletes++

		return
	}

	read := s.isPunct(i-1, ".") && s.isIdent(i-2, "globalThis") && s.isPunct(i-3, "=") &&
		s.tok(i-4).Kind == Ident && s.isDeclared(i-4) && s.isPunct(i+1, ";")

	if !read {
		s.report(i, "globalThis.%s is read only as const x = globalThis.%s; and then deleted", moduleGlobal, moduleGlobal)

		return
	}

	s.moduleReads++

	if s.moduleReads > 1 {
		s.report(i, "globalThis.%s is read more than once: the page script reads it once and deletes it", moduleGlobal)

		return
	}

	s.moduleVar = s.tokens[i-4].Text
}

// checkModuleGlobal requires, of a script that reads the module global, the
// typeof check and the deletion.
func (s *scanner) checkModuleGlobal() {
	if !s.moduleSeen {
		return
	}

	if s.moduleDeletes == 0 {
		s.report(-1, "globalThis.%s is never deleted: delete it after its one read", moduleGlobal)
	}

	if s.moduleVar == "" {
		return
	}

	for i := range s.tokens {
		if s.isIdent(i, "typeof") && s.isIdent(i+1, s.moduleVar) &&
			(s.isPunct(i+2, "!==") || s.isPunct(i+2, "===")) && s.stringIs(i+3, "string") {
			return
		}
	}

	s.report(-1, "the module global's value is never checked: typeof %s !== \"string\" must stop the page before the bytes are decoded", s.moduleVar)
}

// checkListener refuses a message listener and a listener whose event name is
// not a plain string literal.
func (s *scanner) checkListener(i int) {
	if !s.isPunct(i+1, "(") || s.tok(i+2).Kind != String || strings.Contains(s.tokens[i+2].Text, `\`) {
		s.report(i, "addEventListener takes its event name as a plain string literal, so the scan can see that it is no message listener")

		return
	}

	event := s.tokens[i+2].Text[1 : len(s.tokens[i+2].Text)-1]
	if event == "message" || event == "messageerror" {
		s.report(i, "a %s listener is banned: a message another window posts is never read", event)
	}
}

// checkPunct refuses a computed member access and a destructuring assignment,
// either of which can name a property the scan cannot see.
func (s *scanner) checkPunct(i int) {
	t := s.tokens[i]

	switch t.Text {
	case "[":
		if s.computed(i) {
			s.report(i, "a computed member access x[...] is banned: a computed name can hide any banned API from the scan; use for...of or forEach")
		}
	case "]", "}":
		if !s.isAssigned(i) {
			return
		}

		if open, ok := s.match[i]; ok && s.tok(open-1).Kind == Ident && declarations[s.tok(open-1).Text] {
			return
		}

		s.report(i, "a destructuring assignment is banned: its targets can assign a navigation property the scan cannot see")
	}
}

// computed reports whether the bracket at i opens a member access rather than
// an array literal or a pattern: whether the token before it ends an
// expression.
func (s *scanner) computed(i int) bool {
	prev := s.tok(i - 1)

	switch prev.Kind {
	case Ident:
		return !keywordsBeforeExpression[prev.Text]
	case Number, String, Regex:
		return true
	case Template:
		return strings.HasSuffix(prev.Text, "`")
	case Punct:
		switch prev.Text {
		case ")", "]", "}", "?.":
			return true
		}
	}

	return false
}
