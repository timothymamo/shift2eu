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
//     muted text on the page's background is checked too. The limit of that
//     reading is that a rule nested inside another surface -- a card on
//     `--surface`, a notice on its own tint, a table header cell on `--surface-2`
//     -- declares no background of its own, so its `color` is measured against
//     the page's background and not the surface the text really sits on. A rule
//     whose text sits on a surface other than the page must therefore declare
//     that surface's background for this check to pair its colour correctly;
//     otherwise the pair it reads is one no browser paints, and it can pass a
//     colour that is unreadable where the text is drawn.
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
// A stylesheet that states a light and a dark palette (a
// `@media (prefers-color-scheme: dark)` block that redefines the tokens) is read
// as two palettes, and every pair is checked once in each, as palettes below
// defines them: the light palette resolves var() against the tokens declared
// outside the dark block, and the dark palette against those with the dark
// block's tokens on top. Every check here runs for both schemes, and a failure
// names the scheme it was read in. A scheme whose palette uses a token the other
// palette declares is refused with an error: the check reads no colour from a
// var() it cannot resolve, so it never guesses one.
//
// The check also fails when it reads no text pair and no control pair at all, so
// a stylesheet that states none of them cannot pass it vacuously.

// contrastPair is one colour pair the stylesheet states, with the ratio it must
// reach.
type contrastPair struct {
	// scheme names the palette the pair was read in: light or dark.
	scheme string
	// rule names the selector and the element's part the pair is for.
	rule string
	// foreground and background are the pair's colours.
	foreground, background colour
	// minimum is the ratio WCAG 2.2 AA requires: 4.5 for text, 3 for a focus
	// outline or a control's border.
	minimum float64
}

// The colour schemes a stylesheet states, as a failure names them.
const (
	lightScheme = "light"
	darkScheme  = "dark"
)

// TestStylesheetContrast reads the built stylesheet's colour pairs as this
// file's doc comment defines them and fails on one below its WCAG 2.2 AA ratio.
func TestStylesheetContrast(t *testing.T) {
	t.Parallel()

	name, data := theBundle(t).asset(t, "style", ".css")

	findings, textPairs, controlPairs, err := stylesheetContrastFailures(t, name, data)
	if err != nil {
		t.Fatalf("the check could not read %s: %v", name, err)
	}

	for _, finding := range findings {
		t.Error(finding)
	}

	if textPairs == 0 {
		t.Errorf("%s states no text colour pair, so the check read nothing: a rule declares color and background", name)
	}

	if controlPairs == 0 {
		t.Errorf("%s states no border or outline colour pair, so the check read nothing: a control's border and the focus outline need 3:1 against their background", name)
	}
}

// stylesheetContrastFailures reads the colour pairs of src's palettes as this
// file's doc comment defines them, and returns one message per pair under its
// ratio, with the number of text pairs and of control pairs it read. It returns
// an error, and no pairs, when a palette uses a custom property it does not
// declare: the check refuses a stylesheet it cannot read rather than guess.
func stylesheetContrastFailures(t *testing.T, name string, src []byte) ([]string, int, int, error) {
	t.Helper()

	var (
		findings                []string
		textPairs, controlPairs int
	)

	for _, p := range palettes(t, name, parseCSS(t, name, src)) {
		pairs, err := palettePairs(t, name, p)
		if err != nil {
			return nil, 0, 0, err
		}

		for _, pair := range pairs {
			if pair.minimum >= 4.5 {
				textPairs++
			} else {
				controlPairs++
			}

			if ratio := contrastRatio(pair.foreground, pair.background); ratio < pair.minimum {
				findings = append(findings, fmt.Sprintf(
					"%s scheme: %s's contrast is %.2f:1, under the %.1f:1 WCAG 2.2 AA requires (%s): text needs 4.5:1, a focus outline or a control's border 3:1",
					pair.scheme, pair.rule, ratio, pair.minimum, name))
			}
		}
	}

	return findings, textPairs, controlPairs, nil
}

// palette is one colour scheme's view of a stylesheet: the rules that apply in
// it, the custom properties its var() resolves against, and the page's
// background.
type palette struct {
	// name is the scheme's name, as a failure names it.
	name string
	// rules are the style rules that apply in the scheme.
	rules []cssRule
	// props are the custom properties the scheme's var() resolves against.
	props map[string]string
}

// palettes returns the palettes rules state, light first:
//
//   - light holds every rule outside a `@media (prefers-color-scheme: dark)`
//     block, resolved against the custom properties those rules declare. The
//     dark scheme's query is read however it is spelled: alone, or after
//     `screen and`, as site/style.css states it
//     (`@media screen and (prefers-color-scheme: dark)`). A rule inside
//     `@media print` is here too: print states the page on paper, which is the
//     light palette.
//   - dark holds the light rules and the rules the dark block states, resolved
//     against the light properties with every custom property the dark block
//     declares on top of them. The print rules are left out: print is light.
//
// A stylesheet with no dark block has one palette twice, the light one, so both
// schemes are checked against the same tokens and report the same pairs.
func palettes(t *testing.T, name string, rules []cssRule) []palette {
	t.Helper()

	var light, dark []cssRule

	for _, rule := range rules {
		darkOnly := inMedia(rule.atRules, darkSchemeQuery, darkScheme)

		if darkOnly {
			dark = append(dark, rule)

			continue
		}

		light = append(light, rule)

		if !inMedia(rule.atRules, printQuery) {
			dark = append(dark, rule)
		}
	}

	lightProps := customProperties(light)
	darkProps := customProperties(light)

	for _, rule := range dark {
		if !inMedia(rule.atRules, darkSchemeQuery, darkScheme) {
			continue
		}

		for _, decl := range rule.decls {
			if strings.HasPrefix(decl.property, "--") {
				darkProps[decl.property] = decl.value
			}
		}
	}

	return []palette{
		{name: lightScheme, rules: light, props: lightProps},
		{name: darkScheme, rules: dark, props: darkProps},
	}
}

// The media queries a palette is stated in: a dark colour scheme, and print,
// which states paper.
const (
	darkSchemeQuery = "prefers-color-scheme"
	printQuery      = "print"
)

// inMedia reports whether the rule sits inside an `@media` block whose query
// holds every one of words, compared in lower case.
func inMedia(atRules []string, words ...string) bool {
	for _, prelude := range atRules {
		fields := strings.Fields(strings.ToLower(prelude))
		if len(fields) < 2 || fields[0] != "@media" {
			continue
		}

		query := strings.Join(fields[1:], " ")

		if !slices.ContainsFunc(words, func(word string) bool {
			return !strings.Contains(query, word)
		}) {
			return true
		}
	}

	return false
}

// palettePairs returns the colour pairs p's rules state, as this file's doc
// comment defines them. It returns an error naming the scheme it read when p
// uses a custom property it does not declare.
func palettePairs(t *testing.T, name string, p palette) ([]contrastPair, error) {
	t.Helper()

	pageBackground, ok, err := pageBackgroundColour(t, name, p.rules, p.props)
	if err != nil {
		return nil, fmt.Errorf("%s scheme: %w", p.name, err)
	}

	if !ok {
		t.Fatalf("%s declares no background for :root, html or body in its %s palette, so the colours of the text and controls it sets cannot be read: state the page's background there", name, p.name)
	}

	var pairs []contrastPair

	for _, rule := range p.rules {
		selector := strings.Join(rule.selectors, ", ")

		background := pageBackground

		for _, property := range []string{"background-color", "background"} {
			if value, ok := rule.decl(property); ok {
				colourValue, ok, err := colourOf(t, selector, value, p.props)
				if err != nil {
					return nil, fmt.Errorf("%s scheme: %w", p.name, err)
				}

				if ok {
					background = colourValue
				}
			}
		}

		if value, ok := rule.decl("color"); ok {
			foreground, ok, err := colourOf(t, selector, value, p.props)
			if err != nil {
				return nil, fmt.Errorf("%s scheme: %w", p.name, err)
			}

			if ok {
				pairs = append(pairs, contrastPair{
					scheme:     p.name,
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

			foreground, ok, err := colourOf(t, selector, value, p.props)
			if err != nil {
				return nil, fmt.Errorf("%s scheme: %w", p.name, err)
			}

			if !ok {
				continue
			}

			pairs = append(pairs, contrastPair{
				scheme:     p.name,
				rule:       selector + " " + property + " on " + backgroundString(background),
				foreground: foreground,
				background: background,
				minimum:    3,
			})
		}
	}

	return pairs, nil
}

// TestContrastCheckReadsEachPaletteOnItsOwnTokens pins that the check reads a
// stylesheet that states a light and a dark palette as two palettes: each
// scheme's colour pairs are resolved from that scheme's custom properties, the
// light ones outside a `@media (prefers-color-scheme: dark)` block and the dark
// ones with that block's declarations on top of them. A check that resolves one
// set of properties for the whole sheet keeps the dark declaration of each token
// (later in the source wins) and so never sees a light pair at all, which is
// what the first fixture below fails on. A failure names the scheme it was read
// in, so a reader knows which palette to fix. The dark block is also read in the
// `screen and` form site/style.css uses, and a stylesheet whose light palette
// uses a token only the dark block declares is refused with an error naming the
// scheme and the token.
func TestContrastCheckReadsEachPaletteOnItsOwnTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// stylesheet is a fixture in the subset of CSS the checks read.
		stylesheet string
		// want are substrings one finding must hold, together.
		want []string
		// unwanted are substrings no finding may hold.
		unwanted []string
		// wantErr are substrings the error the check returns must hold,
		// together: the check refused to read the stylesheet.
		wantErr []string
	}{
		{
			// The light palette's text pair is 2.85:1, under 4.5:1; the dark
			// palette's is 21:1, and both borders clear 3:1.
			name:       "a light pair under AA whose dark pair passes",
			stylesheet: lightPaletteFailsStylesheet(prefersDarkQuery),
			want:       []string{"light scheme", "body", "2.85:1"},
			unwanted:   []string{"dark scheme"},
		},
		{
			// The rule is stated inside the dark block only: it exists in the dark
			// palette, and the light palette must not report it.
			name:       "a rule the dark block restyles",
			stylesheet: darkPaletteRuleFailsStylesheet(prefersDarkQuery),
			want:       []string{"dark scheme", ".badge", "1.00:1"},
			unwanted:   []string{"light scheme"},
		},
		{
			// site/style.css states its dark block this way, and the check must
			// read the light palette of that form too.
			name:       "a light pair under AA, with the dark block written after screen and",
			stylesheet: lightPaletteFailsStylesheet(screenAndPrefersDarkQuery),
			want:       []string{"light scheme", "body", "2.85:1"},
			unwanted:   []string{"dark scheme"},
		},
		{
			name:       "a rule the screen and dark block restyles",
			stylesheet: darkPaletteRuleFailsStylesheet(screenAndPrefersDarkQuery),
			want:       []string{"dark scheme", ".badge", "1.00:1"},
			unwanted:   []string{"light scheme"},
		},
		{
			// The light palette uses --badge-ink, which only the dark block
			// declares: the check cannot read the light pair, so it refuses the
			// stylesheet rather than guess the token's value from the other scheme.
			name:       "a light rule using a token only the dark block declares",
			stylesheet: lightRuleUsesADarkDeclaredPropertyFixture,
			wantErr:    []string{"light scheme", "--badge-ink", "does not declare"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			findings, textPairs, controlPairs, err := stylesheetContrastFailures(t, "fixture.css", []byte(tc.stylesheet))

			if len(tc.wantErr) > 0 {
				if err == nil {
					t.Fatalf("the check read the fixture with no error, want one holding %q: a token the palette does not declare yields no colour",
						tc.wantErr)
				}

				for _, want := range tc.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("the check returned the error %q, want it to hold %q", err, want)
					}
				}

				return
			}

			if err != nil {
				t.Fatalf("the check refused the fixture: %v, want it read: the fixture declares every token its rules use", err)
			}

			if textPairs == 0 || controlPairs == 0 {
				t.Fatalf("the check read %d text pairs and %d control pairs of the fixture, want at least one of each: the fixture states both",
					textPairs, controlPairs)
			}

			joined := strings.Join(findings, "\n")

			for _, want := range tc.want {
				if !strings.Contains(joined, want) {
					t.Errorf("the check reported no finding naming %q, so it did not read that palette's pair:\nit reported:\n%s\nwant one finding holding %q",
						want, joined, tc.want)
				}
			}

			for _, unwanted := range tc.unwanted {
				if strings.Contains(joined, unwanted) {
					t.Errorf("the check reported a finding naming %q, want none for that palette:\nit reported:\n%s",
						unwanted, joined)
				}
			}
		})
	}
}

// The two forms a stylesheet states its dark palette in: the query alone, and
// after `screen and`, as site/style.css states it. The check reads a palette of
// either form.
const (
	prefersDarkQuery          = "@media (prefers-color-scheme: dark)"
	screenAndPrefersDarkQuery = "@media screen and (prefers-color-scheme: dark)"
)

// lightPaletteFailsStylesheet returns a stylesheet whose light palette states a
// text pair under AA and whose dark palette states none: #999999 on #ffffff is
// 2.85:1, and #ffffff on #000000 is 21:1. Both borders clear 3:1. The dark block
// is stated in darkQuery, so a case pins each form a stylesheet may spell it in.
func lightPaletteFailsStylesheet(darkQuery string) string {
	return `
:root {
  --bg: #ffffff;
  --ink: #999999;
  --line: #6b6b6b;
}
body {
  color: var(--ink);
  background-color: var(--bg);
}
.control {
  border: 1px solid var(--line);
}
` + darkQuery + ` {
  :root {
    --bg: #000000;
    --ink: #ffffff;
  }
}
`
}

// darkPaletteRuleFailsStylesheet returns a stylesheet whose light palette passes
// and whose dark block restyles one element onto a text pair under AA: #101010 on
// #101010 is 1:1.
func darkPaletteRuleFailsStylesheet(darkQuery string) string {
	return `
:root {
  --bg: #ffffff;
  --ink: #1a1a1a;
  --line: #6b6b6b;
}
body {
  color: var(--ink);
  background-color: var(--bg);
}
.control {
  border: 1px solid var(--line);
}
` + darkQuery + ` {
  .badge {
    color: #101010;
    background-color: #101010;
  }
}
`
}

// lightRuleUsesADarkDeclaredPropertyFixture is a stylesheet whose light palette
// passes and whose .only-dark rule uses --badge-ink, a custom property only the
// dark block declares: the light palette holds no such property, so the check
// returns an error naming the scheme and the property.
const lightRuleUsesADarkDeclaredPropertyFixture = `
:root {
  --bg: #ffffff;
  --ink: #1a1a1a;
  --line: #6b6b6b;
}
body {
  color: var(--ink);
  background-color: var(--bg);
}
.control {
  border: 1px solid var(--line);
}
.only-dark {
  color: var(--badge-ink);
}
@media (prefers-color-scheme: dark) {
  :root {
    --badge-ink: #ffffff;
  }
}
`

// pageBackgroundColour returns the background the page sets on :root, html or
// body, and an error when a value of those rules names a custom property the
// palette does not declare.
func pageBackgroundColour(t *testing.T, name string, rules []cssRule, props map[string]string) (colour, bool, error) {
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

			background, ok, err := colourOf(t, name, value, props)
			if err != nil {
				return colour{}, false, err
			}

			if ok {
				return background, true, nil
			}
		}
	}

	return colour{}, false, nil
}

// colourOf returns the colour a declaration's value names, and false when it
// names none: `border: none` and `background: transparent` name none. It returns
// an error when the value's var() names a custom property the palette does not
// declare, which the check cannot read as a colour.
func colourOf(t *testing.T, name, value string, props map[string]string) (colour, bool, error) {
	t.Helper()

	resolved, err := resolveValue(name, value, props)
	if err != nil {
		return colour{}, false, err
	}

	token, ok := firstColour(resolved)
	if !ok {
		return colour{}, false, nil
	}

	return parseColour(t, name, token), true, nil
}

// backgroundString names a colour in a failure message.
func backgroundString(c colour) string {
	return fmt.Sprintf("rgb(%.0f, %.0f, %.0f)", c.r, c.g, c.b)
}
