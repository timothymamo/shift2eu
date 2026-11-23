package livecheck

import (
	"fmt"
	"strings"
)

// The statement markers of ADR 0036: the ids of the elements the deployed
// pages carry when a record asks for the statement (behaviour 8).
const (
	// MarkerHSTSWaived must be carried by privacy.html and verify.html when
	// the HSTS record says waived.
	MarkerHSTSWaived = "hsts-waived"
	// MarkerDowngradeRisk must be carried by privacy.html when the preload
	// record does not say on_preload_list: true.
	MarkerDowngradeRisk = "downgrade-risk"
)

// VersionMarker is the id of the element of the root page (index.html) whose
// text is the live version (behaviour 9).
const VersionMarker = "site-version"

// MaxVersionBytes caps the version text VersionText accepts.
const MaxVersionBytes = 64

// Carries reports whether page, an HTML document, has an element whose id
// attribute is exactly marker (the attribute's value compared after HTML
// entity decoding, in single, double or no quotes, the attribute name without
// regard to case), which has no hidden attribute (in any case, with or
// without a value), and whose text content, with tags removed, entities
// decoded and white space trimmed, is not empty. An id inside a comment,
// a <script>, a <style> or a <template> does not count, nor does an id
// attribute of another name ("data-id"). Two elements with the marker's id
// are not carried (an ambiguous page).
func Carries(page []byte, marker string) bool {
	found := findMarked(page, marker)

	return found.count == 1 && !found.hidden && strings.TrimSpace(found.text) != ""
}

// VersionText returns the text content of the one element of page whose id is
// VersionMarker, under the rules of Carries for finding it but with no
// trimming: the text is returned exactly as the page has it, after entity
// decoding, so MatchVersion can refuse surrounding white space. No such
// element, two, a hidden one, or a text longer than MaxVersionBytes is an
// error.
func VersionText(page []byte) (string, error) {
	found := findMarked(page, VersionMarker)

	switch {
	case found.count != 1:
		return "", fmt.Errorf("the page has %d elements with id=%q, want one", found.count, VersionMarker)
	case found.hidden:
		return "", fmt.Errorf("the element with id=%q is hidden", VersionMarker)
	case found.text == "":
		return "", fmt.Errorf("the element with id=%q has no text", VersionMarker)
	case len(found.text) > MaxVersionBytes:
		return "", fmt.Errorf("the element with id=%q holds %d bytes, more than %d", VersionMarker, len(found.text), MaxVersionBytes)
	}

	return found.text, nil
}
