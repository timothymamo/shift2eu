package pdfreport

// colour is one of the site's light-theme tokens (site/style.css), held as its
// hex value 0xRRGGBB. The writer turns each component into thousandths at write
// time with integer division (ADR 0049, Colours). The zero value, black, is no
// token: a run or shape never carries it.
type colour int

// The palette: the site's light-theme tokens, nothing else (PRD #584, The PDF,
// Look). --warn-bg and --partial-bg share a value, as do --gold-ink and --mast,
// and --info-rule and --accent.
const (
	inkColour        colour = 0x14213d // --ink: body text, values, table cells
	mutedColour      colour = 0x4a5568 // --muted: page furniture, header facts, captions
	mastColour       colour = 0x0b2a66 // --mast: the title band and the banner's fill
	mastInkColour    colour = 0xffffff // --mast-ink: text on --mast
	mastLinkColour   colour = 0xc9d8ff // --mast-link: the banner's eyebrow
	mastTextColour   colour = 0xe6ebf5 // --mast-text: the banner's text
	accentColour     colour = 0x0b3d91 // --accent: section headings, the estimate tile
	goldColour       colour = 0xffcc00 // --gold: left rules, the banner's top rule and email box
	goldInkColour    colour = 0x0b2a66 // --gold-ink: the address on --gold
	directInkColour  colour = 0x0f6b3a // --direct-ink
	directBgColour   colour = 0xe3f4ea // --direct-bg
	partialInkColour colour = 0x874600 // --partial-ink
	partialBgColour  colour = 0xfdf0d5 // --partial-bg
	noneInkColour    colour = 0xa3201a // --none-ink
	noneBgColour     colour = 0xfde4e1 // --none-bg
	infoBgColour     colour = 0xe8eefb // --info-bg: the required-changes box
	infoRuleColour   colour = 0x0b3d91 // --info-rule: its left rule
	warnBgColour     colour = 0xfdf0d5 // --warn-bg: the blockers box
	warnRuleColour   colour = 0xa86400 // --warn-rule: its left rule and warning triangle
	warnInkColour    colour = 0x5c3a00 // --warn-ink: the blockers box's heading
	ringColour       colour = 0xdde2ea // --ring: row rules, outlines, heading and furniture rules
	surface2Colour   colour = 0xeef1f6 // --surface-2: table header rows, Resource details ID bands
	bgColour         colour = 0xf6f7f9 // --bg: workload cards, even table rows
)

// thousandths returns one 0 to 255 component of a colour as the nearest
// thousandth, (byte × 1000 + 127) / 255, in integer arithmetic (ADR 0049).
func thousandths(component int) int { return (component*milli + 127) / 255 }

// operands returns the colour's three components as rg and RG write them:
// three decimals of three places, "0.043 0.165 0.400".
func (c colour) operands() string {
	r, g, b := int(c)>>16&0xff, int(c)>>8&0xff, int(c)&0xff

	return decimal(thousandths(r)) + " " + decimal(thousandths(g)) + " " + decimal(thousandths(b))
}
