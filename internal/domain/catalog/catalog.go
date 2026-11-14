package catalog

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// Snapshot records where and when a catalog's data was taken, so a report can say
// how current it is.
type Snapshot struct {
	// SourceURL is the page or API the data was taken from: an absolute https URL
	// with an ASCII DNS host name that does not end in a number (no IPv4 address
	// in any form a browser reads, such as 0x7f.1), no user info, no square
	// brackets and RFC 3986 characters only, with every '%' starting a
	// two-hex-digit escape, no HTML character reference (a numeric one with or
	// without its ';') and escapes that decode to valid text (see New).
	SourceURL string
	// RetrievedOn is when the data was retrieved. In a Catalog it is in UTC, and
	// reporters format it as a UTC calendar date.
	RetrievedOn time.Time
	// DefaultRegion is the region estimates use when the user names none:
	// non-blank valid free text, stored as given. New does not check it against
	// the offerings' regions; the snapshot reader checks it against the regions
	// the snapshot declares (ADR 0024).
	DefaultRegion string
}

// Offering is one service a target sells, described in taxonomy terms.
type Offering struct {
	// Name is the target's name for the service, for example Kapsule. It is unique
	// within a catalog, compared with every run of white space treated as a
	// single space.
	Name string
	// Categories lists the taxonomy categories the offering serves. One offering
	// may serve several. In a Catalog they are sorted and free of duplicates.
	Categories []taxonomy.Category
	// Capabilities lists the features the offering provides. In a Catalog they are
	// sorted and free of duplicates, and nil when there are none.
	Capabilities []taxonomy.Capability
	// Regions lists the EU regions the offering is available in, as the target
	// names them. In a Catalog they are sorted and free of duplicates.
	Regions []string
	// Plans lists the plans the offering sells, with their monthly prices. In a
	// Catalog they are sorted by name, and nil when there are none.
	Plans []Plan
	// StoragePrices lists the monthly price of one GiB of the offering's
	// storage, by region. In a Catalog they are sorted by their first region,
	// the every-region price first, and nil when there are none.
	StoragePrices []Price
	// FixedFees lists the offering's fixed monthly fee per instance (one
	// Kubernetes cluster), charged on top of its plans and storage, by region.
	// When the offering has fees, they cover every region of the offering exactly
	// once. In a Catalog they are sorted by their first region, the every-region
	// fee first, and nil when the offering charges no fixed fee.
	FixedFees []Price
	// PlanBillingCapHours is the most hours a month the target bills for one
	// instance of one of the offering's plans, whose monthly prices are the price
	// of that many hours; 0 means uncapped, the prices standing for 730 hours.
	// It applies to plan prices only, never to StoragePrices or FixedFees
	// (ADR 0029). In a Catalog it is 0 or from 1 to MaxBillingCapHours.
	PlanBillingCapHours int64
}

// FixedFee returns the fixed monthly fee the offering charges per instance in
// region, and true. It returns 0 and false when the offering charges no fixed
// fee (FixedFees is nil) or region is not one of its regions. region is
// compared exactly with the offering's regions and the fees' regions, as plan
// prices are: no case folding, no trimming. A fee whose Regions is nil applies
// to every region of the offering.
func (o Offering) FixedFee(region string) (Amount, bool) {
	if len(o.FixedFees) == 0 || !slices.Contains(o.Regions, region) {
		return 0, false
	}

	for _, fee := range o.FixedFees {
		if fee.Regions == nil || slices.Contains(fee.Regions, region) {
			return fee.Amount, true
		}
	}

	return 0, false
}

// Has reports whether the offering provides capability c.
func (o Offering) Has(c taxonomy.Capability) bool {
	return slices.Contains(o.Capabilities, c)
}

// serves reports whether the offering serves category c.
func (o Offering) serves(c taxonomy.Category) bool {
	return slices.Contains(o.Categories, c)
}

// Catalog is a validated description of what one target offers, taken from a
// dated snapshot. Build it with New; its contents cannot be changed afterwards.
// A zero Catalog has not been validated: later layers only ever receive values
// built by New.
type Catalog struct {
	target    string
	snapshot  Snapshot
	offerings []Offering
}

var (
	// ErrInvalidCatalog is returned for a catalog without a valid target, a
	// retrieval date or a valid source URL (an absolute https URL with an ASCII
	// DNS host name, see New), or with an offering that claims to serve
	// taxonomy.Unclassified.
	ErrInvalidCatalog = errors.New("invalid catalog")
	// ErrEmptyOfferingName is returned for an offering whose name is empty or only
	// whitespace.
	ErrEmptyOfferingName = errors.New("offering has an empty name")
	// ErrDuplicateOffering is returned when two offerings share a name, compared
	// with every run of white space treated as a single space.
	ErrDuplicateOffering = errors.New("duplicate offering name")
	// ErrNoCategory is returned for an offering that serves no category.
	ErrNoCategory = errors.New("offering serves no taxonomy category")
	// ErrNoRegion is returned for an offering that lists no region, or lists a
	// region whose name is empty or only whitespace.
	ErrNoRegion = errors.New("offering is available in no region")
)

// New validates offerings and returns them as the catalog of target, taken from
// snapshot.
//
// target is the target's human-readable display name, used as is in reports, for
// example Scaleway. It must not be blank and must be valid free text
// (text.Validate).
//
// snapshot.SourceURL must be an absolute https URL with no user info (no '@'
// before the host), valid free text, and made of RFC 3986 characters only:
// unreserved characters, the reserved general and sub-delimiters except the
// square brackets '[' and ']', which are Markdown link syntax, and '%', which
// must start a two-hex-digit escape. Its host must be an ASCII DNS name: only
// letters, digits, dots and hyphens, so neither percent-encoded nor an IP
// address. A port, a query and a fragment are allowed. As defence in depth for
// a reader who sees the URL rendered, it must hold no ampersand that starts the
// shape of an HTML character reference (&amp;, &#38;, &#x26;; a bare '&'
// between query parameters is allowed), and its percent escapes, decoded
// together as bytes, must decode to valid free text (so not %0A, %00,
// %E2%80%AE or %FF). The URL is stored as given, never decoded; escaping it for
// an output format is the reporter's job.
// snapshot.RetrievedOn must be set; the catalog keeps it in UTC.
//
// Every offering needs a unique name that is not blank, at least one category and
// at least one region. Names are unique when compared with every run of white
// space treated as a single space, so "Object Storage" and "Object  Storage" are
// duplicates; look-alike letters (homoglyphs) and Unicode normalisation (NFC and
// NFD) are not compared. Names and regions must be valid free text and are stored
// as given, never trimmed. Categories and capabilities must come from the
// taxonomy, and taxonomy.Unclassified is never a category an offering serves. The
// catalog holds its own copy of the offerings, sorted by name, with each
// offering's categories, capabilities and regions sorted, de-duplicated and nil
// when empty.
//
// Errors name the field and the offering at fault. Values echoed in errors are
// quoted with text.Quote, so errors stay short however long the input is. The
// source URL is never echoed, in whole or in part: it may carry credentials, so a
// URL with user info is rejected with the same message whatever the user info
// holds.
func New(target string, snapshot Snapshot, offerings []Offering) (Catalog, error) {
	if err := validateTarget(target); err != nil {
		return Catalog{}, err
	}

	if err := validateSnapshot(snapshot); err != nil {
		return Catalog{}, err
	}

	snapshot.RetrievedOn = snapshot.RetrievedOn.UTC()

	seen := make(map[string]struct{}, len(offerings))
	normalised := make([]Offering, 0, len(offerings))

	for i, o := range offerings {
		if err := validateName(o.Name); err != nil {
			return Catalog{}, fmt.Errorf("offerings[%d]: %w", i, err)
		}

		key := collapseSpace(o.Name)
		if _, dup := seen[key]; dup {
			return Catalog{}, fmt.Errorf("offerings[%d]: %w %s", i, ErrDuplicateOffering, text.Quote(o.Name))
		}

		seen[key] = struct{}{}

		n, err := normalise(o)
		if err != nil {
			return Catalog{}, fmt.Errorf("offering %s: %w", text.Quote(o.Name), err)
		}

		normalised = append(normalised, n)
	}

	slices.SortFunc(normalised, func(a, b Offering) int {
		return strings.Compare(a.Name, b.Name)
	})

	return Catalog{target: target, snapshot: snapshot, offerings: normalised}, nil
}

// Target is the human-readable display name of the target the catalog
// describes, as reports show it, for example Scaleway.
func (c Catalog) Target() string {
	return c.target
}

// Snapshot returns where and when the catalog's data was taken. RetrievedOn is
// in UTC.
func (c Catalog) Snapshot() Snapshot {
	return c.snapshot
}

// Offerings returns every offering, sorted by name. The result is a deep copy:
// changing it does not change the catalog.
func (c Catalog) Offerings() []Offering {
	out := make([]Offering, len(c.offerings))
	for i, o := range c.offerings {
		out[i] = clone(o)
	}

	return out
}

// OfferingsFor returns the offerings that serve category, sorted by name, or nil
// if none does. The result is a deep copy: changing it does not change the
// catalog.
func (c Catalog) OfferingsFor(category taxonomy.Category) []Offering {
	var out []Offering

	for _, o := range c.offerings {
		if o.serves(category) {
			out = append(out, clone(o))
		}
	}

	return out
}

// validateTarget checks that target is valid free text and not blank.
func validateTarget(target string) error {
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("%w: empty target", ErrInvalidCatalog)
	}

	if err := text.Validate(target); err != nil {
		return fmt.Errorf("%w: target: %w", ErrInvalidCatalog, err)
	}

	return nil
}

// validateSnapshot checks that snapshot is dated, has a valid source URL and a
// default region that is valid free text and not blank.
func validateSnapshot(snapshot Snapshot) error {
	if err := validateSourceURL(snapshot.SourceURL); err != nil {
		return fmt.Errorf("%w: snapshot source URL: %w", ErrInvalidCatalog, err)
	}

	if snapshot.RetrievedOn.IsZero() {
		return fmt.Errorf("%w: snapshot has no retrieval date", ErrInvalidCatalog)
	}

	if strings.TrimSpace(snapshot.DefaultRegion) == "" {
		return fmt.Errorf("%w: snapshot has an empty default region", ErrInvalidCatalog)
	}

	if err := text.Validate(snapshot.DefaultRegion); err != nil {
		return fmt.Errorf("%w: snapshot default region: %w", ErrInvalidCatalog, err)
	}

	return nil
}

// errBadSourceURL is the reason given for a source URL that is not an
// acceptable URL. The URL itself is never echoed: it may carry credentials.
var errBadSourceURL = errors.New("not an absolute https URL with an ASCII DNS host name, no user info, " +
	"RFC 3986 characters only, no character references, percent encoding that decodes to valid text")

// validateSourceURL checks that raw is an absolute https URL with no user info,
// valid free text made of RFC 3986 characters other than square brackets, with
// complete percent escapes and an ASCII DNS host name. Then, as defence in depth
// against a renderer that would show a reader another URL than the catalog
// holds, it refuses an ampersand that starts an HTML character reference shape
// (characterReference) and percent escapes that decode to text text.Validate
// refuses (checkDecodedEscapes).
//
// User info is checked first and rejected with errBadSourceURL alone: any detail
// about a character or an offset after that point could be about a password.
// hasUserInfo cannot speak for every parser (a browser strips leading spaces
// and C0 controls first, and an RFC 3986 parser does not end the authority at
// a backslash), so until url.Parse has confirmed that raw has no user info,
// every error for a raw that holds an '@' anywhere is errBadSourceURL alone
// too. The other errors name what they found and its byte offset in raw, never
// the URL or a part of it.
func validateSourceURL(raw string) error {
	if raw == "" {
		return errors.New("missing")
	}

	if hasUserInfo(raw) {
		return errBadSourceURL
	}

	if err := checkSourceURLText(raw); err != nil {
		if strings.Contains(raw, "@") {
			return errBadSourceURL
		}

		return err
	}

	// The parse error is not wrapped: it echoes the whole URL.
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || !isDNSName(u.Hostname()) {
		return errBadSourceURL
	}

	if i := characterReference(raw); i >= 0 {
		return fmt.Errorf("%w: ampersand starting an HTML character reference at byte %d", errBadSourceURL, i)
	}

	return checkDecodedEscapes(raw)
}

// checkSourceURLText checks that raw is valid free text made of RFC 3986
// characters other than square brackets, with complete percent escapes. Its
// errors name the character or the escape at fault and its byte offset in raw,
// so validateSourceURL gives them only for a raw that holds no '@'.
func checkSourceURLText(raw string) error {
	if err := text.Validate(raw); err != nil {
		return err
	}

	for i, r := range raw {
		if !isURLChar(r) {
			return fmt.Errorf("%w: disallowed character %U at byte %d", errBadSourceURL, r, i)
		}
	}

	if i := badEscape(raw); i >= 0 {
		return fmt.Errorf("%w: incomplete percent escape at byte %d", errBadSourceURL, i)
	}

	return nil
}

// characterReference returns the byte offset of the first '&' in raw that starts
// the shape of an HTML character reference, or -1 if there is none. The shapes
// are a named reference, '&', an ASCII letter, ASCII letters or digits and ';'
// (&amp;, &lt1;), and a numeric one, "&#" and a decimal digit or "&#x" or "&#X"
// and a hexadecimal digit, with or without a ';' after its digits (&#38;, &#38,
// &#x26). An HTML or Markdown renderer would show the character such a
// reference names, not the URL the catalog holds; HTML decodes a numeric
// reference without its ';' too. A '&' that starts no such shape, as between
// query parameters (?a=1&b=2) or before a hash and a letter (&#frag), is
// allowed.
//
// A named reference needs its ';' here, although HTML also decodes some legacy
// names without one (&amp, &copy, &not, &reg, &sect): refusing those by prefix
// would refuse ordinary query parameters such as &region=, &section= and
// &notify=. ADR 0009 records the decision; an HTML reporter escapes '&' itself.
func characterReference(raw string) int {
	for i := range len(raw) {
		if raw[i] == '&' && isCharacterReference(raw[i+1:]) {
			return i
		}
	}

	return -1
}

// isCharacterReference reports whether s, the text after a '&', starts with the
// rest of an HTML character reference shape (see characterReference).
func isCharacterReference(s string) bool {
	switch {
	case strings.HasPrefix(s, "#x"), strings.HasPrefix(s, "#X"):
		return len(s) > 2 && isHex(s[2])
	case strings.HasPrefix(s, "#"):
		return len(s) > 1 && isDigit(s[1])
	case s != "" && isLetter(s[0]):
		// The letter is part of the name: what follows it may be empty.
		return isNameEnd(s[1:])
	default:
		return false
	}
}

// isNameEnd reports whether s starts with ASCII letters or digits, none
// included, followed by ';': the rest of a named reference after its first
// letter.
func isNameEnd(s string) bool {
	n := 0
	for n < len(s) && isAlphanumeric(s[n]) {
		n++
	}

	return n < len(s) && s[n] == ';'
}

// checkDecodedEscapes decodes every percent escape of raw, all at once and as
// bytes, so a character whose UTF-8 encoding is spread over several escapes
// decodes as one, and refuses raw if the decoded text fails text.Validate: an
// escape that decodes to a byte that is not UTF-8 (%FF), to a character
// text.Validate refuses (%0A, %00, the bidi override %E2%80%AE), or to white
// space at the end of the URL. raw holds only complete escapes (badEscape) and,
// outside them, only ASCII characters text.Validate accepts, so whatever fails
// comes from an escape, and the error names the byte of raw that escape starts
// at.
func checkDecodedEscapes(raw string) error {
	decoded := make([]byte, 0, len(raw))
	// from[k] is the byte offset in raw that decoded byte k comes from: the '%'
	// of its escape, or the byte itself.
	from := make([]int, 0, len(raw))

	for i := 0; i < len(raw); i++ {
		from = append(from, i)

		if raw[i] != '%' {
			decoded = append(decoded, raw[i])

			continue
		}

		decoded = append(decoded, unhex(raw[i+1])<<4|unhex(raw[i+2]))
		i += 2
	}

	if text.Validate(string(decoded)) == nil {
		return nil
	}

	for k := 0; k < len(decoded); {
		r, size := utf8.DecodeRune(decoded[k:])
		if r == utf8.RuneError && size == 1 {
			return fmt.Errorf("%w: percent escape at byte %d decodes to a byte that is not UTF-8",
				errBadSourceURL, from[k])
		}

		// Between two letters, text.Validate checks the character for what it
		// is, not for white space at the edge of a value.
		if text.Validate("a"+string(r)+"a") != nil {
			return fmt.Errorf("%w: percent escape at byte %d decodes to disallowed character %U",
				errBadSourceURL, from[k], r)
		}

		k += size
	}

	// Every character is valid text, so the decoded URL has white space at an
	// edge, and it starts with its scheme: the white space is at the end.
	if end := len(strings.TrimRightFunc(string(decoded), unicode.IsSpace)); end < len(decoded) {
		return fmt.Errorf("%w: percent escape at byte %d decodes to white space at the end of the URL",
			errBadSourceURL, from[end])
	}

	return fmt.Errorf("%w: percent escapes decode to text that is not valid", errBadSourceURL)
}

// unhex returns the value of the hexadecimal digit b.
func unhex(b byte) byte {
	switch {
	case '0' <= b && b <= '9':
		return b - '0'
	case 'a' <= b && b <= 'f':
		return b - 'a' + 10
	default:
		return b - 'A' + 10
	}
}

// isLetter reports whether b is an ASCII letter.
func isLetter(b byte) bool {
	return 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z'
}

// isDigit reports whether b is an ASCII decimal digit.
func isDigit(b byte) bool {
	return '0' <= b && b <= '9'
}

// isAlphanumeric reports whether b is an ASCII letter or decimal digit.
func isAlphanumeric(b byte) bool {
	return isLetter(b) || isDigit(b)
}

// hasUserInfo reports whether raw has user info as a browser's URL parser (the
// WHATWG URL Standard) finds it in an https URL: an '@' in the authority, which
// starts after the scheme and any run of '/' or '\', of any length, none
// included, and ends at the next '/', '\', '?' or '#'. So https:/user:pass@host
// and https:user:pass@host have user info, as https://user:pass@host has, and
// an '@' in the path or the query is not user info. It reads the raw string
// because url.Parse fails, instead of reporting user info, when the user info
// holds a character it does not allow, and finds none unless exactly two
// slashes come before it.
func hasUserInfo(raw string) bool {
	rest := raw
	if scheme, after, ok := strings.Cut(raw, ":"); ok && isScheme(scheme) {
		rest = after
	}

	authority := strings.TrimLeft(rest, `/\`)
	if end := strings.IndexAny(authority, `/\?#`); end >= 0 {
		authority = authority[:end]
	}

	return strings.Contains(authority, "@")
}

// isScheme reports whether s is a URL scheme: an ASCII letter, then ASCII
// letters, digits, '+', '-' or '.' (RFC 3986, section 3.1).
func isScheme(s string) bool {
	if s == "" || !isLetter(s[0]) {
		return false
	}

	for i := 1; i < len(s); i++ {
		if !isAlphanumeric(s[i]) && s[i] != '+' && s[i] != '-' && s[i] != '.' {
			return false
		}
	}

	return true
}

// isURLChar reports whether r may appear as given in a source URL: an ASCII
// unreserved character, a general or sub-delimiter other than '[' and ']' (RFC
// 3986), or the '%' that starts a percent escape.
func isURLChar(r rune) bool {
	switch {
	case 'a' <= r && r <= 'z', 'A' <= r && r <= 'Z', '0' <= r && r <= '9':
		return true
	default:
		return strings.ContainsRune("-._~:/?#@!$&'()*+,;=%", r)
	}
}

// badEscape returns the byte offset of the first '%' in raw that is not followed
// by two hex digits, or -1 if every '%' starts a complete escape.
func badEscape(raw string) int {
	for i := range len(raw) {
		if raw[i] == '%' && (i+2 >= len(raw) || !isHex(raw[i+1]) || !isHex(raw[i+2])) {
			return i
		}
	}

	return -1
}

// isHex reports whether b is a hexadecimal digit.
func isHex(b byte) bool {
	return '0' <= b && b <= '9' || 'a' <= b && b <= 'f' || 'A' <= b && b <= 'F'
}

// isDNSName reports whether host, as url.URL.Hostname returns it (decoded, port
// removed), is a non-empty ASCII DNS name of letters, digits, dots and hyphens
// that does not end in a number (endsInNumber), which a browser would read as
// an IPv4 address. An IPv6 address needs square brackets, which
// validateSourceURL refuses before.
func isDNSName(host string) bool {
	if host == "" {
		return false
	}

	for i := range len(host) {
		if !isDNSByte(host[i]) {
			return false
		}
	}

	return !endsInNumber(host)
}

// endsInNumber reports whether host ends in a number, as the WHATWG URL
// Standard's host parser decides it: its last label, after one trailing dot is
// dropped, is decimal digits only, or 0x or 0X followed by hexadecimal digits
// only. A browser parses such a host as an IPv4 address, in any of the forms
// its parser accepts (192.0.2.1; 0x7f.1 and 2130706433, which are both
// 127.0.0.1), so it names no DNS host. A number in any other label, or a
// last label that mixes a digit with a letter, is a name: 1password.com,
// 123.example.com and example.co1 do not end in a number.
func endsInNumber(host string) bool {
	host = strings.TrimSuffix(host, ".")
	last := host[strings.LastIndexByte(host, '.')+1:]

	if len(last) >= 2 && last[0] == '0' && (last[1] == 'x' || last[1] == 'X') {
		return strings.TrimLeft(last[2:], "0123456789abcdefABCDEF") == ""
	}

	return last != "" && strings.TrimLeft(last, "0123456789") == ""
}

// isDNSByte reports whether b may appear in an ASCII DNS name: a letter, a
// digit, a dot or a hyphen.
func isDNSByte(b byte) bool {
	return 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' || '0' <= b && b <= '9' || b == '.' || b == '-'
}

// collapseSpace returns s with every run of white space replaced by one ASCII
// space: the key under which two names that look the same are the same.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// validateName checks that an offering name is valid free text and not blank.
func validateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrEmptyOfferingName
	}

	if err := text.Validate(name); err != nil {
		return fmt.Errorf("name: %w", err)
	}

	return nil
}

// normalise checks the categories, capabilities, regions, plans, storage
// prices and fixed fees of an offering with a valid name and returns a copy of it whose slices
// are sorted, de-duplicated and nil when empty.
func normalise(o Offering) (Offering, error) {
	if len(o.Categories) == 0 {
		return Offering{}, ErrNoCategory
	}

	for _, cat := range o.Categories {
		if _, err := taxonomy.ParseCategory(string(cat)); err != nil {
			return Offering{}, err
		}

		if cat == taxonomy.Unclassified {
			return Offering{}, fmt.Errorf("%w: an offering cannot serve %s",
				ErrInvalidCatalog, text.Quote(string(taxonomy.Unclassified)))
		}
	}

	for _, capability := range o.Capabilities {
		if _, err := taxonomy.ParseCapability(string(capability)); err != nil {
			return Offering{}, err
		}
	}

	if len(o.Regions) == 0 {
		return Offering{}, ErrNoRegion
	}

	for i, region := range o.Regions {
		if strings.TrimSpace(region) == "" {
			return Offering{}, fmt.Errorf("regions[%d]: %w", i, ErrNoRegion)
		}

		if err := text.Validate(region); err != nil {
			return Offering{}, fmt.Errorf("regions[%d]: %w", i, err)
		}
	}

	regions := newRegionSet(o.Regions)

	plans, err := normalisePlans(o.Plans, regions)
	if err != nil {
		return Offering{}, err
	}

	storagePrices, err := normalisePrices("storagePrices", o.StoragePrices, regions)
	if err != nil {
		return Offering{}, err
	}

	fixedFees, err := normaliseFixedFees(o.FixedFees, regions)
	if err != nil {
		return Offering{}, err
	}

	if o.PlanBillingCapHours < 0 || o.PlanBillingCapHours > MaxBillingCapHours {
		return Offering{}, fmt.Errorf("planBillingCapHours: %w: %d, want 0 (uncapped) or 1 to %d",
			ErrInvalidBillingCap, o.PlanBillingCapHours, MaxBillingCapHours)
	}

	o = clone(o)
	o.Categories = sortedUnique(o.Categories)
	o.Capabilities = sortedUnique(withImplied(o.Capabilities))
	o.Regions = sortedUnique(o.Regions)
	o.Plans = plans
	o.StoragePrices = storagePrices
	o.FixedFees = fixedFees

	return o, nil
}

// withImplied returns caps followed by every capability they imply,
// transitively. The result may hold duplicates; sortedUnique removes them.
func withImplied(caps []taxonomy.Capability) []taxonomy.Capability {
	seen := make(map[taxonomy.Capability]struct{}, len(caps))
	for _, c := range caps {
		seen[c] = struct{}{}
	}

	for i := 0; i < len(caps); i++ {
		for _, implied := range caps[i].Implies() {
			if _, ok := seen[implied]; !ok {
				seen[implied] = struct{}{}
				caps = append(caps, implied)
			}
		}
	}

	return caps
}

// sortedUnique sorts s in place and removes duplicates, returning nil when
// nothing is left.
func sortedUnique[T ~string](s []T) []T {
	slices.Sort(s)

	s = slices.Compact(s)
	if len(s) == 0 {
		return nil
	}

	return s
}

// clone returns a copy of o that shares no memory with it.
func clone(o Offering) Offering {
	o.Categories = slices.Clone(o.Categories)
	o.Capabilities = slices.Clone(o.Capabilities)
	o.Regions = slices.Clone(o.Regions)
	o.Plans = clonePlans(o.Plans)
	o.StoragePrices = clonePrices(o.StoragePrices)
	o.FixedFees = clonePrices(o.FixedFees)

	return o
}
