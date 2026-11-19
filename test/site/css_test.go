package site_test

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// This file holds the stylesheet reader the site's checks use: one style rule's
// selectors and declarations, the custom properties a rule declares, and the
// contrast ratio of two colours. It reads the subset of CSS a hand-written
// stylesheet of this site uses (rules, grouped selectors, at-rules with a block
// and declarations), and it fails on a stylesheet it cannot read rather than
// skipping it.

// cssDecl is one declaration of a style rule.
type cssDecl struct {
	// property is the declaration's property name, lower case.
	property string
	// value is the declaration's value, trimmed and with its comments removed.
	value string
}

// cssRule is one style rule.
type cssRule struct {
	// selectors are the rule's selectors, split on commas and trimmed.
	selectors []string
	// decls are the rule's declarations, in source order.
	decls []cssDecl
	// atRules are the at-rule preludes the rule sits in, outermost first, such
	// as "@media (min-width: 60rem)"; empty at the top level.
	atRules []string
}

// decl returns the value of the rule's last declaration of property.
func (r cssRule) decl(property string) (string, bool) {
	value := ""
	found := false

	for _, decl := range r.decls {
		if decl.property == property {
			value = decl.value
			found = true
		}
	}

	return value, found
}

// blockAtRules are the at-rules whose block holds style rules.
var blockAtRules = map[string]bool{
	"@media": true, "@supports": true, "@layer": true, "@container": true, "@scope": true,
}

// parseCSS reads src as a stylesheet, failing the test when it cannot.
func parseCSS(t *testing.T, name string, src []byte) []cssRule {
	t.Helper()

	return parseCSSRules(t, name, stripCSSComments(t, name, string(src)), nil)
}

// parseCSSRules reads the style rules of src, which sit inside atRules.
func parseCSSRules(t *testing.T, name, src string, atRules []string) []cssRule {
	t.Helper()

	var rules []cssRule

	i := 0
	for {
		for i < len(src) && isCSSSpace(src[i]) {
			i++
		}

		if i >= len(src) {
			return rules
		}

		start := i
		for i < len(src) && src[i] != '{' && src[i] != ';' {
			i++
		}

		if i >= len(src) {
			t.Fatalf("%s: the prelude %q has no block", name, strings.TrimSpace(src[start:]))
		}

		prelude := strings.TrimSpace(src[start:i])

		if src[i] == ';' {
			// A statement at-rule, such as @import.
			i++

			continue
		}

		// src[i] is the opening brace: find its partner.
		depth := 1

		blockStart := i + 1
		i++

		for i < len(src) && depth > 0 {
			switch src[i] {
			case '{':
				depth++
			case '}':
				depth--
			}

			i++
		}

		if depth != 0 {
			t.Fatalf("%s: the block of %q never ends", name, prelude)
		}

		block := src[blockStart : i-1]

		if strings.HasPrefix(prelude, "@") {
			if blockAtRules[strings.Fields(prelude + " ")[0]] {
				rules = append(rules, parseCSSRules(t, name, block, append(atRuleContext(atRules), prelude))...)
			}

			continue
		}

		rules = append(rules, cssRule{
			selectors: splitSelectors(prelude),
			decls:     parseDecls(t, name, block),
			atRules:   atRuleContext(atRules),
		})
	}
}

// atRuleContext returns a copy of atRules, so an appended prelude never reaches
// a sibling rule.
func atRuleContext(atRules []string) []string {
	return append([]string(nil), atRules...)
}

// splitSelectors splits a selector list on commas and trims each selector.
func splitSelectors(prelude string) []string {
	parts := strings.Split(prelude, ",")
	out := make([]string, 0, len(parts))

	for _, part := range parts {
		out = append(out, strings.Join(strings.Fields(part), " "))
	}

	return out
}

// parseDecls splits a rule's block into declarations.
func parseDecls(t *testing.T, name, block string) []cssDecl {
	t.Helper()

	var decls []cssDecl

	depth := 0
	start := 0

	flush := func(end int) {
		text := strings.TrimSpace(block[start:end])
		start = end + 1

		if text == "" {
			return
		}

		property, value, ok := strings.Cut(text, ":")
		if !ok {
			t.Fatalf("%s: the declaration %q has no colon", name, text)
		}

		decls = append(decls, cssDecl{
			property: strings.ToLower(strings.TrimSpace(property)),
			value:    strings.Join(strings.Fields(value), " "),
		})
	}

	for i := 0; i < len(block); i++ {
		switch block[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ';':
			if depth == 0 {
				flush(i)
			}
		}
	}

	flush(len(block))

	return decls
}

// stripCSSComments removes every comment from src, failing the test on one that
// never ends.
func stripCSSComments(t *testing.T, name, src string) string {
	t.Helper()

	var b strings.Builder

	for i := 0; i < len(src); i++ {
		if !strings.HasPrefix(src[i:], "/*") {
			b.WriteByte(src[i])

			continue
		}

		end := strings.Index(src[i+2:], "*/")
		if end < 0 {
			t.Fatalf("%s: a comment never ends", name)
		}

		i += end + len("/*") + len("*/") - 1
	}

	return b.String()
}

// isCSSSpace reports whether b is white space in CSS.
func isCSSSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}

// customProperties returns every custom property the rules declare, later
// declarations winning, as a rule's use of var() resolves them.
func customProperties(rules []cssRule) map[string]string {
	props := make(map[string]string)

	for _, rule := range rules {
		for _, decl := range rule.decls {
			if strings.HasPrefix(decl.property, "--") {
				props[decl.property] = decl.value
			}
		}
	}

	return props
}

// colour is a colour's red, green and blue channels, each 0 to 255.
type colour struct {
	r, g, b float64
}

// namedColours are the colour keywords the checks read. Any other keyword is
// refused, so the stylesheet's colours stay readable here.
var namedColours = map[string]colour{
	"black":         {0, 0, 0},
	"white":         {255, 255, 255},
	"red":           {255, 0, 0},
	"green":         {0, 128, 0},
	"blue":          {0, 0, 255},
	"gray":          {128, 128, 128},
	"grey":          {128, 128, 128},
	"silver":        {192, 192, 192},
	"navy":          {0, 0, 128},
	"maroon":        {128, 0, 0},
	"darkslategray": {47, 79, 79},
}

// resolveValue substitutes every var(--name) in value, in one pass per
// substitution, and returns the result. It fails the test on a var() whose
// custom property the stylesheet does not declare.
func resolveValue(t *testing.T, name, value string, props map[string]string) string {
	t.Helper()

	for range 8 {
		start := strings.Index(value, "var(")
		if start < 0 {
			return value
		}

		end := strings.Index(value[start:], ")")
		if end < 0 {
			t.Fatalf("%s: the value %q has an unterminated var()", name, value)
		}

		inside := value[start+len("var(") : start+end]
		property, fallback, hasFallback := strings.Cut(inside, ",")
		property = strings.TrimSpace(property)

		replacement, ok := props[property]
		if !ok {
			if hasFallback {
				replacement = strings.TrimSpace(fallback)
			} else {
				t.Fatalf("%s: the value %q uses %s, which the stylesheet does not declare", name, value, property)
			}
		}

		value = value[:start] + replacement + value[start+end+1:]
	}

	t.Fatalf("%s: the value %q has too many nested var() calls", name, value)

	return ""
}

// parseColour reads one colour value: a hexadecimal literal, an rgb() or
// rgba() call with an opaque alpha, or one of namedColours. It fails the test
// on a value it does not read, so a colour never escapes the check.
func parseColour(t *testing.T, name, value string) colour {
	t.Helper()

	trimmed := strings.ToLower(strings.TrimSpace(value))

	if named, ok := namedColours[trimmed]; ok {
		return named
	}

	if strings.HasPrefix(trimmed, "#") {
		return parseHexColour(t, name, trimmed)
	}

	if strings.HasPrefix(trimmed, "rgb(") || strings.HasPrefix(trimmed, "rgba(") {
		return parseRGBColour(t, name, trimmed)
	}

	t.Fatalf("%s: the checks read no colour from %q; write it as #rrggbb, #rgb or rgb(r, g, b)", name, value)

	return colour{}
}

// parseHexColour reads #rgb or #rrggbb.
func parseHexColour(t *testing.T, name, value string) colour {
	t.Helper()

	digits := strings.TrimPrefix(value, "#")
	if len(digits) == 3 {
		digits = string([]byte{digits[0], digits[0], digits[1], digits[1], digits[2], digits[2]})
	}

	if len(digits) != 6 {
		t.Fatalf("%s: the colour %q is no #rgb or #rrggbb", name, value)
	}

	parse := func(s string) float64 {
		n, err := strconv.ParseUint(s, 16, 8)
		if err != nil {
			t.Fatalf("%s: the colour %q holds no hexadecimal byte %q", name, value, s)
		}

		return float64(n)
	}

	return colour{r: parse(digits[0:2]), g: parse(digits[2:4]), b: parse(digits[4:6])}
}

// parseRGBColour reads rgb(r, g, b) or rgba(r, g, b, alpha) with an opaque
// alpha.
func parseRGBColour(t *testing.T, name, value string) colour {
	t.Helper()

	inside := value[strings.Index(value, "(")+1:]
	inside = strings.TrimSuffix(inside, ")")

	parts := strings.Split(strings.ReplaceAll(inside, " ", ","), ",")
	channels := make([]float64, 0, 3)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		channel, err := strconv.ParseFloat(strings.TrimSuffix(part, "%"), 64)
		if err != nil {
			t.Fatalf("%s: the colour %q holds no number %q", name, value, part)
		}

		channels = append(channels, channel)
	}

	if len(channels) == 4 && channels[3] != 1 {
		t.Fatalf("%s: the colour %q is translucent, so the contrast of what it covers cannot be read here", name, value)
	}

	if len(channels) < 3 {
		t.Fatalf("%s: the colour %q names fewer than three channels", name, value)
	}

	return colour{r: channels[0], g: channels[1], b: channels[2]}
}

// luminance returns the colour's WCAG 2 relative luminance.
func (c colour) luminance() float64 {
	channel := func(v float64) float64 {
		v /= 255

		if v <= 0.03928 {
			return v / 12.92
		}

		return math.Pow((v+0.055)/1.055, 2.4)
	}

	return 0.2126*channel(c.r) + 0.7152*channel(c.g) + 0.0722*channel(c.b)
}

// contrastRatio returns the WCAG 2 contrast ratio of the two colours, from 1 to
// 21.
func contrastRatio(a, b colour) float64 {
	first, second := a.luminance(), b.luminance()
	if first < second {
		first, second = second, first
	}

	return (first + 0.05) / (second + 0.05)
}

// firstColour returns the first colour of a declaration's value, which is the
// whole value for a plain colour and one token of a shorthand such as
// `border: 1px solid #cccccc`. ok is false when the value names no colour.
func firstColour(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)

	switch strings.ToLower(trimmed) {
	case "", "none", "transparent", "inherit", "initial", "unset", "currentcolor", "revert", "revert-layer":
		return "", false
	}

	for _, token := range splitCSSTokens(trimmed) {
		token = strings.Trim(token, ",")

		if isColourToken(token) {
			return token, true
		}

		if _, ok := namedColours[strings.ToLower(token)]; ok {
			return token, true
		}
	}

	return "", false
}

// isColourToken reports whether the token is a colour literal: a hexadecimal
// one or an rgb() or rgba() call.
func isColourToken(token string) bool {
	lower := strings.ToLower(token)

	if strings.HasPrefix(lower, "#") {
		return strings.Trim(lower[1:], "0123456789abcdef") == ""
	}

	return (strings.HasPrefix(lower, "rgb(") || strings.HasPrefix(lower, "rgba(")) && strings.HasSuffix(lower, ")")
}

// splitCSSTokens splits a declaration's value on white space, keeping a group
// inside parentheses together, so `rgb(0, 90, 200)` is one token.
func splitCSSTokens(value string) []string {
	var tokens []string

	depth := 0
	start := 0

	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ' ', '\t':
			if depth == 0 {
				if start < i {
					tokens = append(tokens, value[start:i])
				}

				start = i + 1
			}
		}
	}

	if start < len(value) {
		tokens = append(tokens, value[start:])
	}

	return tokens
}
