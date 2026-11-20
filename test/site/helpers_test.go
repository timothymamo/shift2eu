package site_test

import (
	"strings"
	"testing"
)

// This file holds the site checks' own tests: the HTML reader, the stylesheet
// reader, the selector match the Hidden definition needs, and the contrast
// ratio. A bug in one of them would either let a page pass a check it should
// fail or make an innocent page fail, and neither shows in a check whose
// subject is the bundle, so they are pinned here.

// TestParseHTMLReadsElementsAndText pins the HTML reader on the constructs the
// built pages and the reporter's goldens hold.
func TestParseHTMLReadsElementsAndText(t *testing.T) {
	t.Parallel()

	const source = `<!DOCTYPE html>
<!-- a comment holding <script> and fetch("/x") -->
<html lang="en">
<head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'self'"></head>
<body data-page="assessment" hidden>
<h1 id="title">Hello &amp; goodbye &lt;script&gt;</h1>
<input type="checkbox" checked>
<script src="page.0123456789abcdef.js" defer>var inline = "<b>";</script>
<label for="targets">Targets</label>
</body>
</html>`

	doc := parseHTML(t, "synthetic.html", []byte(source))

	body := doc.body(t)

	if got, _ := body.attr("data-page"); got != "assessment" {
		t.Errorf("the <body> carries data-page=%q, want %q", got, "assessment")
	}

	if !body.hasAttr("hidden") {
		t.Error("the <body> carries no hidden attribute after the reader read it")
	}

	head := doc.head(t)
	if len(head.children) == 0 || head.children[0].tag != "meta" {
		t.Fatalf("the reader read the head's first child as %v, want a <meta>", head.children)
	}

	title := doc.find("h1")[0]

	if got, want := title.textContent(), "Hello & goodbye <script>"; got != want {
		t.Errorf("the h1's text content is %q, want %q: entities are decoded and a tag in the text stays text", got, want)
	}

	inputs := doc.find("input")
	if len(inputs) != 1 || !inputs[0].hasAttr("checked") {
		t.Fatalf("the reader read %d <input> elements, want one with the checked attribute", len(inputs))
	}

	scripts := doc.find("script")
	if len(scripts) != 1 {
		t.Fatalf("the reader read %d <script> elements, want one", len(scripts))
	}

	if got, want := scripts[0].textContent(), `var inline = "<b>";`; got != want {
		t.Errorf("the script's raw text is %q, want %q: a script's content is text and is read to its own end tag", got, want)
	}

	if got, _ := scripts[0].attr("src"); got != "page.0123456789abcdef.js" {
		t.Errorf("the script's src is %q", got)
	}

	labels := doc.find("label")
	if len(labels) != 1 {
		t.Fatalf("the reader read %d <label> elements, want one", len(labels))
	}

	if got, _ := labels[0].attr("for"); got != "targets" {
		t.Errorf("the label's for is %q", got)
	}
}

// TestParseCSSReadsRules pins the stylesheet reader: custom properties, grouped
// selectors, an at-rule's rules and declarations holding functions.
func TestParseCSSReadsRules(t *testing.T) {
	t.Parallel()

	const source = `
/* a comment: url(image.png) and @import "other.css"; */
:root {
  --fg: #111111;
  --bg: #ffffff;
}
body, .page {
  color: var(--fg);
  background-color: var(--bg);
  font-family: system-ui, sans-serif;
}
@media (min-width: 60rem) {
  a:focus-visible {
    outline: 2px solid rgb(0, 90, 200);
  }
}`

	rules := parseCSS(t, "synthetic.css", []byte(source))

	if len(rules) != 3 {
		t.Fatalf("the reader read %d rules, want three: :root, the grouped rule, and the one in the media query: %v", len(rules), rules)
	}

	props := customProperties(rules)

	if got, want := props["--fg"], "#111111"; got != want {
		t.Errorf("the custom property --fg is %q, want %q", got, want)
	}

	if got := rules[1].selectors; len(got) != 2 || got[0] != "body" || got[1] != ".page" {
		t.Errorf("the grouped rule's selectors are %v, want [body .page]", got)
	}

	if got, want := rules[1].decls[0], (cssDecl{property: "color", value: "var(--fg)"}); got != want {
		t.Errorf("the rule's first declaration is %v, want %v", got, want)
	}

	if got, want := rules[2].atRules, []string{"@media (min-width: 60rem)"}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("the focus rule's at-rule context is %v, want %v", got, want)
	}

	if got, want := rules[2].decls[0].property, "outline"; got != want {
		t.Errorf("the focus rule's first declaration is %q, want %q", got, want)
	}
}

// TestSelectorMatches pins the selector match the Hidden definition uses: the
// subject compound, by id, class, tag and [hidden].
func TestSelectorMatches(t *testing.T) {
	t.Parallel()

	doc := parseHTML(t, "synthetic.html", []byte(`<div class="notice warning" id="risk"><p><span id="inner">text</span></p></div>`))

	inner := doc.find("span")[0]
	risk := doc.find("div")[0]

	tests := []struct {
		selector string
		element  *element
		want     bool
	}{
		{selector: "#risk", element: risk, want: true},
		{selector: "#risk", element: inner, want: true},
		{selector: ".notice", element: inner, want: true},
		{selector: ".missing", element: risk, want: false},
		{selector: "span", element: inner, want: true},
		{selector: "span", element: risk, want: false},
		{selector: "*", element: inner, want: true},
		{selector: "div .missing", element: inner, want: false},
		{selector: "p > .warning", element: inner, want: true},
		{selector: "[hidden]", element: inner, want: false},
		{selector: "span::before", element: inner, want: false},
		{selector: "#inner", element: inner, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.selector, func(t *testing.T) {
			t.Parallel()

			if got := selectorMatches(tc.selector, tc.element); got != tc.want {
				t.Errorf("selectorMatches(%q, <%s>) = %t, want %t", tc.selector, tc.element.tag, got, tc.want)
			}
		})
	}
}

// TestHiddenReason pins how the checks read the Privacy statement's Hidden
// definition: each row states how a marked element is hidden, and the last row
// states an element nothing hides.
func TestHiddenReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		html string
		css  string
		want bool
	}{
		{
			name: "shown",
			html: `<div class="page"><p id="marker">the statement</p></div>`,
			css:  `#marker { color: #111111; }`,
			want: false,
		},
		{
			name: "the hidden attribute on the element",
			html: `<p id="marker" hidden>the statement</p>`,
			want: true,
		},
		{
			name: "aria-hidden on an ancestor",
			html: `<div aria-hidden="true"><p id="marker">the statement</p></div>`,
			want: true,
		},
		{
			name: "a closed details ancestor",
			html: `<details><summary>more</summary><p id="marker">the statement</p></details>`,
			want: true,
		},
		{
			name: "an open details ancestor",
			html: `<details open><summary>more</summary><p id="marker">the statement</p></details>`,
			want: false,
		},
		{
			name: "display: none by id",
			html: `<p id="marker">the statement</p>`,
			css:  `#marker { display: none; }`,
			want: true,
		},
		{
			name: "display: none by class",
			html: `<div class="hidden"><p id="marker">the statement</p></div>`,
			css:  `.hidden { display: none; }`,
			want: true,
		},
		{
			name: "display: none by tag",
			html: `<p id="marker">the statement</p>`,
			css:  `p { display: none; }`,
			want: true,
		},
		{
			name: "display: none in a media query",
			html: `<p id="marker">the statement</p>`,
			css:  `@media (max-width: 40rem) { #marker { display: none; } }`,
			want: true,
		},
		{
			name: "visibility: hidden",
			html: `<p id="marker">the statement</p>`,
			css:  `p { visibility: hidden; }`,
			want: true,
		},
		{
			name: "a zero height",
			html: `<p id="marker">the statement</p>`,
			css:  `#marker { height: 0; }`,
			want: true,
		},
		{
			name: "a zero width with a unit",
			html: `<p id="marker">the statement</p>`,
			css:  `#marker { width: 0px; }`,
			want: true,
		},
		{
			name: "a position off-screen",
			html: `<p id="marker">the statement</p>`,
			css:  `#marker { position: absolute; left: -9999px; }`,
			want: true,
		},
		{
			name: "a rule that matches nothing",
			html: `<p id="marker">the statement</p>`,
			css:  `.elsewhere { display: none; }`,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc := parseHTML(t, "synthetic.html", []byte(tc.html))

			marker := doc.find("p")[0]
			rules := parseCSS(t, "synthetic.css", []byte(tc.css))
			props := customProperties(rules)

			reason := hiddenReason(t, marker, rules, props)

			if tc.want && reason == "" {
				t.Errorf("hiddenReason reported the element as shown, want it hidden")
			}

			if !tc.want && reason != "" {
				t.Errorf("hiddenReason reported the element hidden because %q, want it shown", reason)
			}
		})
	}
}

// TestContrastRatio pins the WCAG 2 ratios the stylesheet's pairs are held to, at
// the two thresholds: 21:1 for black on white, and a known pair just above and
// one just below 4.5:1.
func TestContrastRatio(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fore string
		back string
		want float64
	}{
		{name: "black on white", fore: "#000000", back: "#ffffff", want: 21},
		{name: "white on black", fore: "#ffffff", back: "#000000", want: 21},
		{name: "AA grey on white", fore: "#767676", back: "#ffffff", want: 4.54},
		{name: "a grey under AA", fore: "#999999", back: "#ffffff", want: 2.85},
		{name: "a short hex is the same colour as its long form", fore: "#fff", back: "#000", want: 21},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ratio := contrastRatio(parseColour(t, tc.name, tc.fore), parseColour(t, tc.name, tc.back))

			if difference := ratio - tc.want; difference > 0.01 || difference < -0.01 {
				t.Errorf("the contrast of %s on %s is %.2f:1, want %.2f:1", tc.fore, tc.back, ratio, tc.want)
			}

			if aboveAA, wantAboveAA := ratio >= 4.5, tc.want >= 4.5; aboveAA != wantAboveAA {
				t.Errorf("the contrast of %s on %s is %.2f:1, which reads as %s 4.5:1, want %s", tc.fore, tc.back, ratio, aboveOrUnder(aboveAA), aboveOrUnder(wantAboveAA))
			}
		})
	}
}

// aboveOrUnder names the side of a contrast threshold a ratio falls on.
func aboveOrUnder(above bool) string {
	if above {
		return "above"
	}

	return "under"
}

// TestFirstColour pins the colour a shorthand names, which is how a
// `border: 1px solid #cccccc` becomes a control pair.
func TestFirstColour(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  string
		ok    bool
	}{
		{value: "#cccccc", want: "#cccccc", ok: true},
		{value: "1px solid #cccccc", want: "#cccccc", ok: true},
		{value: "rgb(0, 90, 200)", want: "rgb(0,90,200)", ok: true},
		{value: "none"},
		{value: "transparent"},
		{value: "inherit"},
		{value: "1px solid currentColor"},
	}

	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()

			got, ok := firstColour(tc.value)
			if ok != tc.ok || (ok && !strings.EqualFold(strings.ReplaceAll(got, " ", ""), strings.ReplaceAll(tc.want, " ", ""))) {
				t.Errorf("firstColour(%q) = %q, %t, want %q, %t", tc.value, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestResolveBundleFile pins how a page's reference becomes a bundle file, and
// which references are refused: an absolute one, one that leaves the bundle and
// one that names a file the bundle does not hold.
func TestResolveBundleFile(t *testing.T) {
	t.Parallel()

	bundle := &bundle{files: []string{"index.html", "page.0123456789abcdef.js", "privacy.html", "style.0123456789abcdef.css", "verify.html"}}

	tests := []struct {
		name string
		ref  string
		want string
		bad  bool
	}{
		{name: "a file", ref: "privacy.html", want: "privacy.html"},
		{name: "a file with a fragment", ref: "verify.html#hsts-waived", want: "verify.html"},
		{name: "a file with a query", ref: "style.0123456789abcdef.css?v=1", want: "style.0123456789abcdef.css"},
		{name: "a file written with ./ first", ref: "./page.0123456789abcdef.js", want: "page.0123456789abcdef.js"},
		{name: "an absolute URL", ref: "https://www.shift2eu.com/index.html", bad: true},
		{name: "a protocol-relative URL", ref: "//cdn.example.com/page.js", bad: true},
		{name: "a root-relative path", ref: "/index.html", bad: true},
		{name: "a path that leaves the bundle", ref: "../index.html", bad: true},
		{name: "a file the bundle does not hold", ref: "records/hsts.json", bad: true},
		{name: "an empty reference", ref: "", bad: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveBundleFile(bundle, tc.ref)

			if tc.bad {
				if err == nil {
					t.Errorf("resolveBundleFile(%q) = %q, want an error: the reference names no file of the bundle", tc.ref, got)
				}

				return
			}

			if err != nil {
				t.Fatalf("resolveBundleFile(%q): %v", tc.ref, err)
			}

			if got != tc.want {
				t.Errorf("resolveBundleFile(%q) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
}
