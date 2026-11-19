package site_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// This file checks the stylesheet's colour pairs against WCAG 2.2 AA: 4.5:1 for
// text, and 3:1 for a focus outline or a control's border (PRD #297,
// Accessibility).
//
// How pairs are read, since a stylesheet states its colours in rules rather than
// in pairs:
//
//   - Every rule that declares `color` is a text pair. Its background is the
//     rule's own `background` or `background-color` when it declares one, and
//     the page's background otherwise, which is the background the first rule
//     whose selector is :root, html or body declares. So a rule that colours
//     muted text on the page's background is checked too.
//   - Every rule that declares a border colour (`border`, `border-color`,
//     `border-top`, `border-bottom`, `border-left`, `border-right` or one of the
//     -color forms) or an outline colour (`outline`, `outline-color`) is a
//     control pair against the same background, at 3:1. A border of
//     `border: none` names no colour and forms no pair.
//   - A colour is read from a hexadecimal literal (#rgb or #rrggbb), an rgb() or
//     rgba() call with an opaque alpha, or one of the keyword colours in
//     namedColours; var() is resolved against the custom properties the
//     stylesheet declares (css_test.go). Any other colour fails the check, so no
//     pair escapes it: a translucent colour, or one this file cannot read, is
//     refused rather than assumed.
//   - A pair counts once per rule and declaration, so a rule repeated in a media
//     query is checked in both places.
//
// The check also fails when it reads no text pair and no control pair at all, so
// a stylesheet that states none of them cannot pass it vacuously.

// contrastPair is one colour pair the stylesheet states, with the ratio it must
// reach.
type contrastPair struct {
	// rule names the selector and the element's part the pair is for.
	rule string
	// foreground and background are the pair's colours.
	foreground, background colour
	// minimum is the ratio WCAG 2.2 AA requires: 4.5 for text, 3 for a focus
	// outline or a control's border.
	minimum float64
}

// TestStylesheetContrast reads the built stylesheet's colour pairs as this
// file's doc comment defines them and fails on one below its WCAG 2.2 AA ratio.
func TestStylesheetContrast(t *testing.T) {
	t.Parallel()

	name, data := theBundle(t).asset(t, "style", ".css")

	rules := parseCSS(t, name, data)
	props := customProperties(rules)

	pageBackground, ok := pageBackgroundColour(t, name, rules, props)
	if !ok {
		t.Fatalf("%s declares no background for :root, html or body, so the colours of the text and controls it sets cannot be read: state the page's background there", name)
	}

	var pairs []contrastPair

	for _, rule := range rules {
		selector := strings.Join(rule.selectors, ", ")

		background := pageBackground

		for _, property := range []string{"background-color", "background"} {
			if value, ok := rule.decl(property); ok {
				if colourValue, ok := colourOf(t, selector, value, props); ok {
					background = colourValue
				}
			}
		}

		if value, ok := rule.decl("color"); ok {
			if foreground, ok := colourOf(t, selector, value, props); ok {
				pairs = append(pairs, contrastPair{
					rule:       selector + " text on " + backgroundString(background),
					foreground: foreground,
					background: background,
					minimum:    4.5,
				})
			}
		}

		for _, property := range []string{
			"border", "border-color", "border-top", "border-bottom", "border-left", "border-right",
			"border-top-color", "border-bottom-color", "border-left-color", "border-right-color",
			"outline", "outline-color",
		} {
			value, ok := rule.decl(property)
			if !ok {
				continue
			}

			foreground, ok := colourOf(t, selector, value, props)
			if !ok {
				continue
			}

			pairs = append(pairs, contrastPair{
				rule:       selector + " " + property + " on " + backgroundString(background),
				foreground: foreground,
				background: background,
				minimum:    3,
			})
		}
	}

	textPairs := 0
	controlPairs := 0

	for _, pair := range pairs {
		if pair.minimum >= 4.5 {
			textPairs++
		} else {
			controlPairs++
		}

		if ratio := contrastRatio(pair.foreground, pair.background); ratio < pair.minimum {
			t.Errorf("%s's contrast is %.2f:1, under the %.1f:1 WCAG 2.2 AA requires (%s): text needs 4.5:1, a focus outline or a control's border 3:1",
				pair.rule, ratio, pair.minimum, name)
		}
	}

	if textPairs == 0 {
		t.Errorf("%s states no text colour pair, so the check read nothing: a rule declares color and background", name)
	}

	if controlPairs == 0 {
		t.Errorf("%s states no border or outline colour pair, so the check read nothing: a control's border and the focus outline need 3:1 against their background", name)
	}
}

// pageBackgroundColour returns the background the page sets on :root, html or
// body.
func pageBackgroundColour(t *testing.T, name string, rules []cssRule, props map[string]string) (colour, bool) {
	t.Helper()

	for _, rule := range rules {
		if !slices.ContainsFunc(rule.selectors, func(selector string) bool {
			return selector == ":root" || selector == "html" || selector == "body"
		}) {
			continue
		}

		for _, property := range []string{"background-color", "background"} {
			value, ok := rule.decl(property)
			if !ok {
				continue
			}

			if background, ok := colourOf(t, name, value, props); ok {
				return background, true
			}
		}
	}

	return colour{}, false
}

// colourOf returns the colour a declaration's value names, and false when it
// names none: `border: none` and `background: transparent` name none.
func colourOf(t *testing.T, name, value string, props map[string]string) (colour, bool) {
	t.Helper()

	resolved := resolveValue(t, name, value, props)

	token, ok := firstColour(resolved)
	if !ok {
		return colour{}, false
	}

	return parseColour(t, name, token), true
}

// backgroundString names a colour in a failure message.
func backgroundString(c colour) string {
	return fmt.Sprintf("rgb(%.0f, %.0f, %.0f)", c.r, c.g, c.b)
}
