package snapshot

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/timothymamo/shift2eu/internal/adapter/internal/jsondoc"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// schemaVersion is the only version of the snapshot format this package reads
// (ADR 0007, ADR 0024).
const schemaVersion = 2

// MaxDocumentBytes is the size of the largest snapshot Parse reads, in bytes. A
// larger one is refused before any of it is decoded. An embedded snapshot is a
// few kilobytes; the limit leaves room to grow while keeping a document that is
// not a snapshot from costing more than a moment to refuse.
const MaxDocumentBytes = 1 << 20

var (
	// ErrDecode is returned for data that is not a well-formed snapshot: larger
	// than MaxDocumentBytes, starting with a byte order mark, not valid UTF-8,
	// not one JSON object, a key that is unknown, not spelt exactly or repeated,
	// a field that is missing, null or of the wrong JSON type, a string that
	// escapes a lone surrogate, a retrieval date that is not a YYYY-MM-DD date,
	// or a declared region that is blank, not valid text (text.Validate),
	// declared twice, not a region code (ASCII letters and digits in groups
	// joined by single hyphens) or equal to another ignoring ASCII case, or a
	// number that does not follow the format's grammar (an integer with a
	// fraction or an exponent, an exact decimal with an exponent or too many
	// fractional digits, a zero with a minus sign, or a value outside int64).
	ErrDecode = errors.New("not a valid catalog document")
	// ErrSchemaVersion is returned for a document whose schemaVersion is missing
	// or is not a version this package reads.
	ErrSchemaVersion = errors.New("unsupported schema version")
	// ErrUndeclaredRegion is returned for an offering available in a region the
	// document's top-level regions list does not declare.
	ErrUndeclaredRegion = errors.New("offering in an undeclared region")
	// ErrUndeclaredDefaultRegion is returned for a document whose defaultRegion
	// is not one of the regions it declares, spelt as declared. A blank one, and
	// any default region of a document that declares none, is undeclared too.
	ErrUndeclaredDefaultRegion = errors.New("default region is not a declared region")
)

// Document is a parsed snapshot: the catalog it describes and the EU regions it
// declares.
type Document struct {
	// Catalog is the catalog the snapshot describes, built by catalog.New.
	Catalog catalog.Catalog
	// Regions are the regions the snapshot declares, in the order the document
	// lists them. Every offering of Catalog is available only in these regions.
	// The slice is the caller's own: each Parse returns a new one.
	Regions []string
}

// Parse decodes and validates data as a snapshot of schema version 2 and
// returns the catalog it describes, built by catalog.New, and the regions it
// declares. The package documentation lists every rule a document must meet.
//
// Every error is one line and wraps ErrDecode, ErrSchemaVersion,
// ErrUndeclaredDefaultRegion, ErrUndeclaredRegion, or the error of catalog.New
// (so catalog.ErrInvalidCatalog, the plan and price sentinels and the taxonomy
// sentinels stay reachable). Values from data are shown through
// text.Quote; the text of the JSON decoder and time.Parse errors is never passed
// on. Errors name neither the input nor a target: the caller does.
func Parse(data []byte) (Document, error) {
	doc, err := decode(data)
	if err != nil {
		return Document{}, err
	}

	retrievedOn, err := parseDate(doc.retrievedOn)
	if err != nil {
		return Document{}, err
	}

	declared, err := declaredRegions(doc.regions)
	if err != nil {
		return Document{}, err
	}

	if !isDeclared(declared, doc.defaultRegion) {
		return Document{}, fmt.Errorf("%w: default region %s is not one of the declared regions",
			ErrUndeclaredDefaultRegion, text.Quote(doc.defaultRegion))
	}

	offerings := make([]catalog.Offering, len(doc.offerings))

	for i, o := range doc.offerings {
		for _, region := range o.regions {
			if !isDeclared(declared, region) {
				return Document{}, fmt.Errorf(
					"%w: offering %s is available in region %s, which the regions list does not declare",
					ErrUndeclaredRegion, text.Quote(o.name), text.Quote(region))
			}
		}

		offerings[i] = catalog.Offering{
			Name:                o.name,
			Categories:          o.categories,
			Capabilities:        o.capabilities,
			Regions:             o.regions,
			Plans:               o.plans,
			StoragePrices:       o.storagePrices,
			FixedFees:           o.fixedFees,
			PlanBillingCapHours: o.planBillingCapHours,
		}
	}

	snapshot := catalog.Snapshot{
		SourceURL:     doc.sourceURL,
		RetrievedOn:   retrievedOn,
		DefaultRegion: doc.defaultRegion,
	}

	cat, err := catalog.New(doc.target, snapshot, offerings)
	if err != nil {
		return Document{}, err
	}

	return Document{Catalog: cat, Regions: doc.regions}, nil
}

// document is a snapshot of schema version 2 as the walk reads it, before its
// values are checked. Every field but an offering's fixedFees and
// planBillingCapHours is required by the format (ADR 0026, ADR 0029); the walk
// refuses a document that leaves one out or gives any field, the optional ones
// included, as null.
type document struct {
	target        string
	sourceURL     string
	retrievedOn   string
	regions       []string
	defaultRegion string
	offerings     []offering
}

// offering is one offering of a document. Its values are checked by catalog.New,
// not here.
type offering struct {
	name          string
	categories    []taxonomy.Category
	capabilities  []taxonomy.Capability
	regions       []string
	plans         []catalog.Plan
	storagePrices []catalog.Price
	// fixedFees is nil when the offering has no fixedFees key (ADR 0026).
	fixedFees []catalog.Price
	// planBillingCapHours is 0 when the offering has no planBillingCapHours key
	// (ADR 0029).
	planBillingCapHours int64
}

// The keys of schema version 2, spelt exactly as ADR 0007, ADR 0024, ADR 0026
// and ADR 0029 record them.
const (
	keySchemaVersion       = "schemaVersion"
	keyTarget              = "target"
	keySourceURL           = "sourceURL"
	keyRetrievedOn         = "retrievedOn"
	keyRegions             = "regions"
	keyOfferings           = "offerings"
	keyName                = "name"
	keyCategories          = "categories"
	keyCapabilities        = "capabilities"
	keyDefaultRegion       = "defaultRegion"
	keyPlans               = "plans"
	keyStoragePrices       = "storagePrices"
	keyFixedFees           = "fixedFees"
	keyPlanBillingCapHours = "planBillingCapHours"
	keyVCPUs               = "vcpus"
	keyMemoryGiB           = "memoryGiB"
	keyStorageGiB          = "storageGiB"
	keyPrices              = "prices"
	keyAmount              = "amount"
	keyCurrency            = "currency"
)

// documentKeys, offeringKeys, planKeys and priceKeys list the keys of the
// document object, an offering, a plan and a price, every one of them required
// but two: an offering's fixedFees (ADR 0026) and planBillingCapHours
// (ADR 0029), the format's only optional keys, recorded exceptions to ADR 0007.
// They are never modified.
var (
	documentKeys = []jsondoc.Key{
		{Name: keySchemaVersion, Required: true},
		{Name: keyTarget, Required: true},
		{Name: keySourceURL, Required: true},
		{Name: keyRetrievedOn, Required: true},
		{Name: keyRegions, Required: true},
		{Name: keyDefaultRegion, Required: true},
		{Name: keyOfferings, Required: true},
	}
	offeringKeys = []jsondoc.Key{
		{Name: keyName, Required: true},
		{Name: keyCategories, Required: true},
		{Name: keyCapabilities, Required: true},
		{Name: keyRegions, Required: true},
		{Name: keyPlans, Required: true},
		{Name: keyStoragePrices, Required: true},
		{Name: keyFixedFees, Required: false},
		{Name: keyPlanBillingCapHours, Required: false},
	}
	planKeys = []jsondoc.Key{
		{Name: keyName, Required: true},
		{Name: keyVCPUs, Required: true},
		{Name: keyMemoryGiB, Required: true},
		{Name: keyStorageGiB, Required: true},
		{Name: keyPrices, Required: true},
	}
	priceKeys = []jsondoc.Key{
		{Name: keyAmount, Required: true},
		{Name: keyCurrency, Required: true},
		{Name: keyRegions, Required: true},
	}
)

// decode reads data as a snapshot of schema version 2 through the shared strict
// decoder (jsondoc), in this order: at most MaxDocumentBytes; UTF-8 without a
// byte order mark; one JSON value, which is an object; schemaVersion 2
// (jsondoc.Document.Version), found before any other key or value is checked,
// so a document of another version is refused for its version alone; then,
// token by token (walk), every key exact, known and not repeated, every field
// present, not null and of its JSON type, every number following the format's
// grammar (number.go), and no string that escapes a lone surrogate.
func decode(data []byte) (document, error) {
	doc, err := jsondoc.Open(data, MaxDocumentBytes, ErrDecode)
	if err != nil {
		return document{}, err
	}

	if err := doc.Version(keySchemaVersion, schemaVersion, ErrSchemaVersion); err != nil {
		return document{}, err
	}

	return walk(doc.Walker())
}

// parseDate reads the retrieval date of a document: a YYYY-MM-DD calendar date,
// as UTC midnight.
func parseDate(raw string) (time.Time, error) {
	// A layout without a zone parses as UTC.
	date, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: retrievedOn %s is not a YYYY-MM-DD date", ErrDecode, text.Quote(raw))
	}

	return date, nil
}

// declaredRegions checks the regions a document declares, each in this order:
// non-blank, valid text, a region code (isRegionCode), and not equal to a
// region declared before it, exactly or ignoring ASCII case. It returns them as
// a map from the code in ASCII lower case to the code as declared, so each
// check, and each offering region later (isDeclared), takes constant time
// however many regions are declared.
func declaredRegions(regions []string) (map[string]string, error) {
	declared := make(map[string]string, len(regions))

	for i, region := range regions {
		if strings.TrimSpace(region) == "" {
			return nil, fmt.Errorf("%w: declared region %s (regions[%d]) is blank",
				ErrDecode, text.Quote(region), i)
		}

		if err := text.Validate(region); err != nil {
			return nil, fmt.Errorf("%w: declared region %s (regions[%d]): %w",
				ErrDecode, text.Quote(region), i, err)
		}

		if !isRegionCode(region) {
			return nil, fmt.Errorf("%w: declared region %s (regions[%d]) is not a region code: "+
				"ASCII letters and digits in groups joined by single hyphens", ErrDecode, text.Quote(region), i)
		}

		key := asciiLower(region)

		if earlier, dup := declared[key]; dup {
			if earlier == region {
				return nil, fmt.Errorf("%w: region %s is declared more than once (again at regions[%d])",
					ErrDecode, text.Quote(region), i)
			}

			return nil, fmt.Errorf("%w: declared region %s (regions[%d]) is region %s again: region codes are compared ignoring case",
				ErrDecode, text.Quote(region), i, text.Quote(earlier))
		}

		declared[key] = region
	}

	return declared, nil
}

// isDeclared reports whether region is one of declared, the map declaredRegions
// returns, spelt as the document declares it.
func isDeclared(declared map[string]string, region string) bool {
	spelling, ok := declared[asciiLower(region)]

	return ok && spelling == region
}

// isRegionCode reports whether s is a region code: ASCII letters and digits in
// one or more groups joined by single hyphens, ^[A-Za-z0-9]+(-[A-Za-z0-9]+)*$.
// fr-par, GRA and eu-central-1 are region codes. A look-alike letter (the
// Cyrillic р of fr-раr), any space, an underscore, a dot and a hyphen at an end
// or after another are not: such a code would declare a region no reader can
// tell from a real one.
func isRegionCode(s string) bool {
	// afterHyphen starts true, so a code that is empty or starts with a hyphen
	// is refused like one that ends with a hyphen.
	afterHyphen := true

	for i := range len(s) {
		switch b := s[i]; {
		case b == '-' && !afterHyphen:
			afterHyphen = true
		case 'a' <= b && b <= 'z', 'A' <= b && b <= 'Z', '0' <= b && b <= '9':
			afterHyphen = false
		default:
			return false
		}
	}

	return !afterHyphen
}

// asciiLower returns s with its ASCII upper-case letters in lower case, and s
// itself when it has none, so the usual lower-case code costs no allocation.
func asciiLower(s string) string {
	i := strings.IndexFunc(s, func(r rune) bool { return 'A' <= r && r <= 'Z' })
	if i < 0 {
		return s
	}

	b := []byte(s)
	for j := i; j < len(b); j++ {
		if 'A' <= b[j] && b[j] <= 'Z' {
			b[j] += 'a' - 'A'
		}
	}

	return string(b)
}
