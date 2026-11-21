package jsscan_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/site/internal/jsscan"
)

// kindName names a token kind in a failure message, so a diff names what moved
// rather than a number.
func kindName(kind jsscan.Kind) string {
	switch kind {
	case jsscan.Ident:
		return "Ident"
	case jsscan.Punct:
		return "Punct"
	case jsscan.Number:
		return "Number"
	case jsscan.String:
		return "String"
	case jsscan.Template:
		return "Template"
	case jsscan.Regex:
		return "Regex"
	default:
		return fmt.Sprintf("Kind(%d)", int(kind))
	}
}

// tok is the token the table expects: line and column are 1-based.
func tok(kind jsscan.Kind, text string, line, col int) jsscan.Token {
	return jsscan.Token{Kind: kind, Text: text, Line: line, Col: col}
}

// TestTokens pins the lexer the page-script scan rests on: identifiers,
// punctuation with its multi-character operators, numbers, quoted strings,
// template literals split around their expressions and regular expression
// literals, with comments and white space dropped. A name in a comment, a
// string, a template's text or a regular expression is text, not code, so the
// lexer has to tell them apart: these cases are what keep the scan from
// refusing an innocent script, and from missing a name inside `${...}`.
func TestTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		want    []jsscan.Token
		wantErr bool
	}{
		{
			name: "an assignment",
			src:  "const a = 1;",
			want: []jsscan.Token{
				tok(jsscan.Ident, "const", 1, 1),
				tok(jsscan.Ident, "a", 1, 7),
				tok(jsscan.Punct, "=", 1, 9),
				tok(jsscan.Number, "1", 1, 11),
				tok(jsscan.Punct, ";", 1, 12),
			},
		},
		{
			name: "line and block comments are no tokens",
			src:  "// fetch(\"/x\")\n/* sendBeacon(\"/y\") */\nconst a = 1;",
			want: []jsscan.Token{
				tok(jsscan.Ident, "const", 3, 1),
				tok(jsscan.Ident, "a", 3, 7),
				tok(jsscan.Punct, "=", 3, 9),
				tok(jsscan.Number, "1", 3, 11),
				tok(jsscan.Punct, ";", 3, 12),
			},
		},
		{
			name: "quoted strings keep their quotes and escapes",
			src:  "const s = \"a\\\"b\"; const t = 'fetch';",
			want: []jsscan.Token{
				tok(jsscan.Ident, "const", 1, 1),
				tok(jsscan.Ident, "s", 1, 7),
				tok(jsscan.Punct, "=", 1, 9),
				tok(jsscan.String, `"a\"b"`, 1, 11),
				tok(jsscan.Punct, ";", 1, 17),
				tok(jsscan.Ident, "const", 1, 19),
				tok(jsscan.Ident, "t", 1, 25),
				tok(jsscan.Punct, "=", 1, 27),
				tok(jsscan.String, "'fetch'", 1, 29),
				tok(jsscan.Punct, ";", 1, 36),
			},
		},
		{
			name: "a template literal without an expression is one chunk",
			src:  "const s = `plain fetch`;",
			want: []jsscan.Token{
				tok(jsscan.Ident, "const", 1, 1),
				tok(jsscan.Ident, "s", 1, 7),
				tok(jsscan.Punct, "=", 1, 9),
				tok(jsscan.Template, "`plain fetch`", 1, 11),
				tok(jsscan.Punct, ";", 1, 24),
			},
		},
		{
			name: "a template literal splits around its expression",
			src:  "const s = `a${b}c`;",
			want: []jsscan.Token{
				tok(jsscan.Ident, "const", 1, 1),
				tok(jsscan.Ident, "s", 1, 7),
				tok(jsscan.Punct, "=", 1, 9),
				tok(jsscan.Template, "`a${", 1, 11),
				tok(jsscan.Ident, "b", 1, 15),
				tok(jsscan.Template, "}c`", 1, 16),
				tok(jsscan.Punct, ";", 1, 19),
			},
		},
		{
			name: "a slash after a value divides and a slash after an operator starts a regular expression",
			src:  "const h = total / 2; const r = /a\\/b/g;",
			want: []jsscan.Token{
				tok(jsscan.Ident, "const", 1, 1),
				tok(jsscan.Ident, "h", 1, 7),
				tok(jsscan.Punct, "=", 1, 9),
				tok(jsscan.Ident, "total", 1, 11),
				tok(jsscan.Punct, "/", 1, 17),
				tok(jsscan.Number, "2", 1, 19),
				tok(jsscan.Punct, ";", 1, 20),
				tok(jsscan.Ident, "const", 1, 22),
				tok(jsscan.Ident, "r", 1, 28),
				tok(jsscan.Punct, "=", 1, 30),
				tok(jsscan.Regex, `/a\/b/g`, 1, 32),
				tok(jsscan.Punct, ";", 1, 39),
			},
		},
		{
			name: "multi-character operators are one token",
			src:  "a !== b; c += 1; d => d;",
			want: []jsscan.Token{
				tok(jsscan.Ident, "a", 1, 1),
				tok(jsscan.Punct, "!==", 1, 3),
				tok(jsscan.Ident, "b", 1, 7),
				tok(jsscan.Punct, ";", 1, 8),
				tok(jsscan.Ident, "c", 1, 10),
				tok(jsscan.Punct, "+=", 1, 12),
				tok(jsscan.Number, "1", 1, 15),
				tok(jsscan.Punct, ";", 1, 16),
				tok(jsscan.Ident, "d", 1, 18),
				tok(jsscan.Punct, "=>", 1, 20),
				tok(jsscan.Ident, "d", 1, 23),
				tok(jsscan.Punct, ";", 1, 24),
			},
		},
		{
			name:    "an unterminated string is an error",
			src:     "const s = \"abc",
			wantErr: true,
		},
		{
			name:    "an unterminated comment is an error",
			src:     "/* abc",
			wantErr: true,
		},
		{
			name:    "an unterminated template literal is an error",
			src:     "const s = `abc",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := jsscan.Tokens([]byte(tc.src))

			if tc.wantErr {
				if err == nil {
					t.Fatalf("Tokens(%q) returned no error, want an error for a source it cannot lex: %v", tc.src, names(got))
				}

				return
			}

			if err != nil {
				t.Fatalf("Tokens(%q): %v", tc.src, err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Tokens(%q) mismatch (-want +got):\n%s", tc.src, diff)
			}
		})
	}
}

// names renders tokens for a failure message.
func names(tokens []jsscan.Token) string {
	out := make([]string, 0, len(tokens))

	for _, token := range tokens {
		out = append(out, fmt.Sprintf("%s(%q)", kindName(token.Kind), token.Text))
	}

	return strings.Join(out, " ")
}

// scanRow is one snippet with the word its findings must name: a rule that
// fired for another reason would leave the snippet refused but the finding
// confusing, so each row says which construct its message has to name.
type scanRow struct {
	name string
	src  string
	// want are the words, compared case-insensitively, that the messages must
	// hold. Empty means any finding is enough.
	want []string
}

// TestScanRejectsEveryBannedApi pins rule by rule that the page-script scan
// refuses what PRD #297's first trust layer bans: every browser API that can
// send data or load a resource, every messaging API that reaches another
// window or browsing context (which the CSP does not govern), every computed
// property name that could hide one, the reflection calls that name a property
// at run time, and every HTML sink, since only one insertion point may exist.
// One row per name, so a rule that stops firing names itself.
func TestScanRejectsEveryBannedApi(t *testing.T) {
	t.Parallel()

	rows := []scanRow{
		// The sending and loading APIs.
		{name: "fetch", src: `fetch("/inventory");`, want: []string{"fetch"}},
		{name: "XMLHttpRequest", src: `const r = new XMLHttpRequest();`, want: []string{"XMLHttpRequest"}},
		{name: "WebSocket", src: `const s = new WebSocket("wss://example.com");`, want: []string{"WebSocket"}},
		{name: "EventSource", src: `const s = new EventSource("/events");`, want: []string{"EventSource"}},
		{name: "sendBeacon", src: `navigator.sendBeacon("/collect", body);`, want: []string{"sendBeacon"}},
		{name: "RTCPeerConnection", src: `const p = new RTCPeerConnection();`, want: []string{"RTCPeerConnection"}},
		{name: "Worker", src: `const w = new Worker("/w.js");`, want: []string{"Worker"}},
		{name: "SharedWorker", src: `const w = new SharedWorker("/w.js");`, want: []string{"SharedWorker"}},
		{name: "importScripts", src: `importScripts("/w.js");`, want: []string{"importScripts"}},
		{name: "open", src: `window.open("/x");`, want: []string{"open"}},
		{name: "location navigation", src: `location.assign("/x");`, want: []string{"location"}},
		{name: "location replacement", src: `location.replace("/x");`, want: []string{"location"}},
		{name: "location assignment", src: `location = "/x";`, want: []string{"location"}},
		{name: "setAttribute", src: `el.setAttribute("href", "/x");`, want: []string{"setAttribute"}},
		{name: "setAttributeNS", src: `el.setAttributeNS(null, "src", "/x");`, want: []string{"setAttributeNS"}},
		{name: "dispatchEvent", src: `el.dispatchEvent(new Event("click"));`, want: []string{"dispatchEvent"}},
		{name: "document.cookie", src: `document.cookie = "a=1";`, want: []string{"cookie"}},
		{name: "localStorage", src: `localStorage.setItem("k", "v");`, want: []string{"localStorage"}},
		{name: "sessionStorage", src: `sessionStorage.getItem("k");`, want: []string{"sessionStorage"}},
		{name: "indexedDB", src: `indexedDB.open("db");`, want: []string{"indexedDB"}},
		{name: "a service worker registration", src: `navigator.serviceWorker.register("/sw.js");`, want: []string{"serviceWorker"}},

		// The messaging APIs, which no CSP governs.
		{name: "postMessage", src: `window.postMessage(inventory, "*");`, want: []string{"postMessage"}},
		{name: "opener", src: `const o = window.opener;`, want: []string{"opener"}},
		{name: "parent", src: `const p = window.parent;`, want: []string{"parent"}},
		{name: "top", src: `const t = window.top;`, want: []string{"top"}},
		{name: "name", src: `window.name = encoded;`, want: []string{"name"}},
		{name: "BroadcastChannel", src: `const c = new BroadcastChannel("c");`, want: []string{"BroadcastChannel"}},
		{name: "MessageChannel", src: `const c = new MessageChannel();`, want: []string{"MessageChannel"}},
		{name: "a message listener", src: `window.addEventListener("message", handler);`, want: []string{"message"}},
		{name: "an onmessage handler", src: `window.onmessage = handler;`, want: []string{"onmessage"}},

		// Computed names and the reflection that names one at run time.
		{name: "a computed read", src: `const v = object[key];`, want: []string{"computed"}},
		{name: "a computed write", src: `object[key] = value;`, want: []string{"computed"}},
		{name: "Object.assign", src: `Object.assign(target, source);`, want: []string{"Object.assign"}},
		{name: "Reflect.set", src: `Reflect.set(object, "k", value);`, want: []string{"Reflect.set"}},
		{name: "Reflect.get", src: `const v = Reflect.get(object, "k");`, want: []string{"Reflect.get"}},
		{name: "Object.defineProperty", src: `Object.defineProperty(object, "k", {});`, want: []string{"Object.defineProperty"}},
		{name: "Object.defineProperties", src: `Object.defineProperties(object, {});`, want: []string{"Object.defineProperties"}},

		// The HTML sinks: exactly one insertion point may exist.
		{name: "innerHTML", src: `target.innerHTML = "<b>x</b>";`, want: []string{"innerHTML"}},
		{name: "outerHTML", src: `target.outerHTML = "<b>x</b>";`, want: []string{"outerHTML"}},
		{name: "insertAdjacentHTML", src: `target.insertAdjacentHTML("beforeend", "<b>x</b>");`, want: []string{"insertAdjacentHTML"}},
		{name: "document.write", src: `document.write("<b>x</b>");`, want: []string{"write"}},
		{name: "document.writeln", src: `document.writeln("<b>x</b>");`, want: []string{"writeln"}},
		{name: "srcdoc", src: `frame.srcdoc = "<b>x</b>";`, want: []string{"srcdoc"}},
		{name: "setHTMLUnsafe", src: `target.setHTMLUnsafe("<b>x</b>");`, want: []string{"setHTMLUnsafe"}},
		{name: "parseHTMLUnsafe", src: `const d = Document.parseHTMLUnsafe("<b>x</b>");`, want: []string{"parseHTMLUnsafe"}},
		{name: "createContextualFragment", src: `const f = range.createContextualFragment("<b>x</b>");`, want: []string{"createContextualFragment"}},
		{name: "DOMParser", src: `const d = new DOMParser();`, want: []string{"DOMParser"}},
		{name: "eval", src: `eval("fetch(\"/x\")");`, want: []string{"eval"}},
		{name: "Function", src: `const f = new Function("return 1");`, want: []string{"Function"}},
		{name: "a string in setTimeout", src: `setTimeout("fetch('/x')", 1);`, want: []string{"setTimeout"}},
		{name: "a string in setInterval", src: `setInterval("fetch('/x')", 1);`, want: []string{"setInterval"}},
		{name: "a name hidden in a template expression", src: "const s = `${fetch(\"/x\")}`;", want: []string{"fetch"}},

		// The result's fields: only html is markup, and only one insertion
		// point takes it.
		{name: "error into a sink", src: `target.innerHTML = result.error;`, want: []string{"error"}},
		{name: "kind into a sink", src: `target.innerHTML = result.kind;`, want: []string{"kind"}},
		{name: "error into the report", src: `insertReport(target, result.error);`, want: []string{"error"}},
		{name: "markdown into the report", src: `insertReport(target, result.markdown);`, want: []string{"markdown"}},
		{name: "json into the report", src: `insertReport(target, result.json);`, want: []string{"json"}},
		{name: "markdown away from a Blob", src: `const copy = result.markdown;`, want: []string{"markdown"}},
		{name: "json away from a Blob", src: `const copy = result.json;`, want: []string{"json"}},

		// Trusted Types: one policy, of one fixed name.
		{name: "two policies", src: `const a = trustedTypes.createPolicy("shift2eu-report", {}); const b = trustedTypes.createPolicy("shift2eu-report", {});`, want: []string{"createPolicy"}},
		{name: "another policy name", src: `const a = trustedTypes.createPolicy("other", {});`, want: []string{"createPolicy"}},
		{name: "no policy at all", src: `const a = 1;`, want: []string{"createPolicy"}},

		// The module global: read once, checked, deleted.
		{name: "the module global read twice", src: `const a = globalThis.__shift2euModule; const b = globalThis.__shift2euModule;`, want: []string{"__shift2euModule"}},
		{name: "the module global unchecked", src: `const a = globalThis.__shift2euModule; delete globalThis.__shift2euModule;`, want: []string{"typeof"}},
		{name: "the module global kept", src: `const a = globalThis.__shift2euModule; if (typeof a !== "string") { return; }`, want: []string{"delete"}},
		{name: "instantiateStreaming", src: `WebAssembly.instantiateStreaming(module);`, want: []string{"instantiateStreaming"}},
		{name: "compileStreaming", src: `WebAssembly.compileStreaming(module);`, want: []string{"compileStreaming"}},

		// The clipboard: a write is confined to the copy function, which a click
		// handler calls, so no other function puts text on it.
		{name: "a clipboard write at the top level", src: `navigator.clipboard.writeText(text);`, want: []string{"clipboard"}},
		{name: "a clipboard write in another function", src: `function copyText(text) { navigator.clipboard.writeText(text); }`, want: []string{"clipboard"}},
		{name: "a clipboard write in the copy function, twice", src: `function copyCommand(button) { navigator.clipboard.writeText(button.dataset.command); navigator.clipboard.writeText(button.dataset.command); }`, want: []string{"clipboard"}},
		{name: "the clipboard read instead of written", src: `function copyCommand(button) { return navigator.clipboard.readText(); }`, want: []string{"clipboard"}},
		{name: "the write taken out of the clipboard's member", src: `function copyCommand(button) { const writer = navigator.clipboard.writeText; writer(button.dataset.command); }`, want: []string{"clipboard"}},
		{name: "the clipboard written through another object", src: `function copyCommand(button) { window.clipboard.writeText(button.dataset.command); }`, want: []string{"clipboard"}},

		// A binding that takes the name navigator: the clipboard and service worker
		// rules key on the name, so a local navigator would stand in for the browser's
		// and carry any object the script likes into an allowed form (audit finding 4).
		{name: "navigator declared by const", src: `const navigator = stand_in;`, want: []string{"navigator"}},
		{name: "navigator declared by let", src: `let navigator = stand_in;`, want: []string{"navigator"}},
		{name: "navigator declared by var", src: `var navigator = stand_in;`, want: []string{"navigator"}},
		{name: "navigator as a function parameter", src: `function standIn(navigator) { return navigator; }`, want: []string{"navigator"}},
		{name: "navigator as a destructuring target", src: `const { navigator } = stand_in;`, want: []string{"navigator"}},
	}

	// Every assignment to a property that navigates or changes a target, on
	// any object: a location or an <a> reaches the network through each.
	for _, property := range []string{
		"href", "src", "action", "search", "pathname", "hash",
		"host", "hostname", "port", "protocol", "username", "password",
	} {
		rows = append(rows, scanRow{
			name: "assignment to " + property,
			src:  `target.` + property + ` = "/inventory";`,
			want: []string{property},
		})
	}

	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			violations, err := jsscan.Scan([]byte(tc.src))
			if err != nil {
				t.Fatalf("Scan(%q): %v", tc.src, err)
			}

			if len(violations) == 0 {
				t.Fatalf("Scan(%q) reported no violation, want the scan to refuse it", tc.src)
			}

			joined := strings.ToLower(messages(violations))

			for _, want := range tc.want {
				if !strings.Contains(joined, strings.ToLower(want)) {
					t.Errorf("Scan(%q) reported %q, want a finding that names %q, so the failure says which rule fired", tc.src, joined, want)
				}
			}
		})
	}
}

// TestScanConfinesTheExceptions pins the other half of the rule: the framing
// guard's two uses, the download function's four, the unregister function's
// two and the report's one insertion point are allowed in one named function
// each, in the exact form PRD #297 fixes, and nowhere else. Every row breaks
// one of those confinements.
func TestScanConfinesTheExceptions(t *testing.T) {
	t.Parallel()

	tests := []scanRow{
		{
			name: "top outside the framing guard",
			src:  `function frameGuard() { if (window.top !== window.self) { return false; } return true; }`,
			want: []string{"top"},
		},
		{
			name: "the framing guard using top twice",
			src: `function guardFraming() { if (window.top !== window.self) { return false; } ` +
				`if (window.top !== window.self) { return false; } window.opener = null; return true; }`,
			want: []string{"top"},
		},
		{
			name: "the framing guard reading opener",
			src:  `function guardFraming() { if (window.top !== window.self) { return false; } const o = window.opener; return true; }`,
			want: []string{"opener"},
		},
		{
			name: "the framing guard assigning something else to opener",
			src:  `function guardFraming() { window.opener = "keep"; return true; }`,
			want: []string{"opener"},
		},
		{
			name: "the framing guard using parent",
			src:  `function guardFraming() { if (window.top !== window.self) { return false; } window.parent.focus(); return true; }`,
			want: []string{"parent"},
		},
		{
			name: "a banned API inside the framing guard",
			src:  `function guardFraming() { window.opener = null; fetch("/x"); return true; }`,
			want: []string{"fetch"},
		},
		{
			name: "createObjectURL outside the download function",
			src:  `function makeLink(blob) { return URL.createObjectURL(blob); }`,
			want: []string{"createObjectURL"},
		},
		{
			name: "click outside the download function",
			src:  `function press(link) { link.click(); }`,
			want: []string{"click"},
		},
		{
			name: "a banned API inside the download function",
			src:  `function download(name, blob) { fetch("/x"); }`,
			want: []string{"fetch"},
		},
		{
			name: "an href assignment inside the download function to another value",
			src:  `function download(name, blob) { const link = document.createElement("a"); link.href = "/elsewhere"; }`,
			want: []string{"href"},
		},
		{
			name: "serviceWorker outside the unregister function",
			src:  `function probe() { return navigator.serviceWorker.ready; }`,
			want: []string{"serviceWorker"},
		},
		{
			name: "a registration inside the unregister function",
			src:  `function unregisterServiceWorkers() { navigator.serviceWorker.register("/sw.js"); }`,
			want: []string{"serviceWorker"},
		},
		{
			name: "two sinks in the one insertion point",
			src:  `function insertReport(target, html) { target.innerHTML = html; target.outerHTML = html; }`,
			want: []string{"outerHTML"},
		},
		{
			name: "a sink in a function that is not the insertion point",
			src:  `function showReport(target, html) { target.innerHTML = html; }`,
			want: []string{"innerHTML"},
		},
		{
			name: "the insertion point taking error",
			src:  `function insertReport(target) { target.innerHTML = result.error; }`,
			want: []string{"error"},
		},
		{
			name: "the insertion point taking markdown",
			src:  `function insertReport(target, markdown) { target.innerHTML = markdown; }`,
			want: []string{"markdown"},
		},
		{
			name: "a banned API inside the copy function",
			src:  `function copyCommand(button) { fetch("/x"); navigator.clipboard.writeText(button.dataset.command); }`,
			want: []string{"fetch"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			violations, err := jsscan.Scan([]byte(tc.src))
			if err != nil {
				t.Fatalf("Scan(%q): %v", tc.src, err)
			}

			if len(violations) == 0 {
				t.Fatalf("Scan(%q) reported no violation, want the scan to refuse it", tc.src)
			}

			joined := strings.ToLower(messages(violations))

			for _, want := range tc.want {
				if !strings.Contains(joined, strings.ToLower(want)) {
					t.Errorf("Scan(%q) reported %q, want a finding that names %q", tc.src, joined, want)
				}
			}
		})
	}
}

// pageSkeleton is a page script that holds only what PRD #297 allows, in the
// forms it allows them in: one Trusted Types policy of the fixed name, the
// framing guard's one comparison of window.top and its one assignment to
// window.opener, the download function's blob URL and its one href assignment
// on an <a download> it clicks, the unregister function's two service-worker
// calls, the report's one insertion point, the module global read once, checked
// and deleted, and the result's error shown only through textContent while
// markdown and json go only to a Blob. The scan must accept it whole: this is
// what it means for the exceptions to be allowed in their named functions.
const pageSkeleton = `const reportPolicy = trustedTypes.createPolicy("shift2eu-report", {
  createHTML: function (html) {
    return html;
  },
});

function guardFraming() {
  if (window.top !== window.self) {
    return false;
  }
  window.opener = null;
  return true;
}

function download(name, blob) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.download = name;
  link.href = url;
  link.click();
  URL.revokeObjectURL(url);
}

function unregisterServiceWorkers() {
  navigator.serviceWorker.getRegistrations().then(function (registrations) {
    registrations.forEach(function (registration) {
      registration.unregister();
    });
  });
}

function insertReport(target, html) {
  target.innerHTML = html;
}

function copyCommand(button) {
  navigator.clipboard.writeText(button.dataset.command);
}

function showFailure(status, result) {
  status.textContent = result.error;
}

function startAssessment() {
  const encoded = globalThis.__shift2euModule;
  if (typeof encoded !== "string") {
    return;
  }
  delete globalThis.__shift2euModule;
  const bytes = Uint8Array.from(atob(encoded), function (c) {
    return c.charCodeAt(0);
  });
  WebAssembly.instantiate(bytes.buffer);
}
`

// TestScanAcceptsTheAllowedForms pins that the scan refuses nothing it allows:
// the page skeleton above in full, and the text a script may name a banned API
// in without reaching it, a string, a regular expression, a comment and a
// template's text. A scan that failed these would push the author away from
// the one form the rule set asks for.
func TestScanAcceptsTheAllowedForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
	}{
		{name: "the page skeleton", src: pageSkeleton},
		{
			name: "a banned name in a string, a regular expression, a comment and a template's text",
			src: `const policy = trustedTypes.createPolicy("shift2eu-report", { createHTML: function (html) { return html; } });
const text = "fetch is banned, and so are sendBeacon and postMessage";
const pattern = /fetch|XMLHttpRequest|innerHTML/g;
// fetch("/x") and eval and innerHTML are banned here too
const hint = ` + "`" + `never fetch ${1 + 1} times` + "`" + `;
`,
		},
		{
			name: "the result's plain text fields shown through textContent",
			src: `const policy = trustedTypes.createPolicy("shift2eu-report", { createHTML: function (html) { return html; } });
function showFailure(status, result) {
  status.textContent = result.error;
}
`,
		},
		{
			name: "markdown and json go to a Blob",
			src: `const policy = trustedTypes.createPolicy("shift2eu-report", { createHTML: function (html) { return html; } });
const md = new Blob([result.markdown], { type: "text/markdown" });
const raw = new Blob([result.json], { type: "application/json" });
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			violations, err := jsscan.Scan([]byte(tc.src))
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}

			if len(violations) != 0 {
				t.Errorf("Scan refused a script the rules allow: %s\nsrc:\n%s", messages(violations), tc.src)
			}
		})
	}
}

// TestScanFailsClosed pins that a script the lexer cannot read is an error, not
// a pass: a scan that reported no violation for a source it could not lex would
// accept a script past its own rules by breaking them.
func TestScanFailsClosed(t *testing.T) {
	t.Parallel()

	violations, err := jsscan.Scan([]byte(`const policy = trustedTypes.createPolicy("shift2eu-report", { createHTML: function (html) { return "unterminated; }`))

	if err == nil {
		t.Fatalf("Scan returned no error for a source it cannot lex, and %q as its violations", messages(violations))
	}
}

// TestBannedJSNames pins the set the page-script scan shares with the module
// glue's check (PRD #297, Layer 1): every name bannedNames holds, the reflection
// objects, the navigation properties and the two names the rules confine to one
// function, sorted and without a duplicate, and nothing the rules allow.
//
// It is a set another check reads, so it must not lose a group without a failure
// here: a set that quietly held only the messaging names would leave the glue's
// own check weaker than it says.
func TestBannedJSNames(t *testing.T) {
	t.Parallel()

	names := jsscan.BannedJSNames()

	for _, want := range []struct {
		// group is the list of the rules the name comes from.
		group string
		names []string
	}{
		{group: "the sending, loading, storing, markup and code names", names: []string{"fetch", "XMLHttpRequest", "Worker", "postMessage", "eval", "Function", "outerHTML", "cookie"}},
		{group: "the reflection objects", names: []string{"Object", "Reflect"}},
		{group: "the navigation properties", names: []string{"href", "src", "action", "hostname"}},
		{group: "the names Scan confines to one function", names: []string{"innerHTML", "createPolicy", "clipboard", "writeText"}},
	} {
		for _, name := range want.names {
			if !slices.Contains(names, name) {
				t.Errorf("BannedJSNames() does not hold %q, one of %s: the set is every name the page script may not hold, and a caller compares its own allow list with it", name, want.group)
			}
		}
	}

	for _, allowed := range []string{"textContent", "getElementById", "Blob", "addEventListener", "name", "error"} {
		if slices.Contains(names, allowed) {
			t.Errorf("BannedJSNames() holds %q, which the page script's rules allow: the set is the names the rules refuse or confine, not every name a script uses", allowed)
		}
	}

	if !slices.IsSorted(names) {
		t.Errorf("BannedJSNames() is not sorted: %v", names)
	}

	if compact := slices.Compact(slices.Clone(names)); len(compact) != len(names) {
		t.Errorf("BannedJSNames() holds a duplicate name: %v", names)
	}
}

// messages renders violations for a failure message.
func messages(violations []jsscan.Violation) string {
	out := make([]string, 0, len(violations))

	for _, violation := range violations {
		out = append(out, fmt.Sprintf("%d:%d: %s", violation.Line, violation.Col, violation.Message))
	}

	return strings.Join(out, "\n")
}
