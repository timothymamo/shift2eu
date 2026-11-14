package assessmentrequest

import (
	"errors"
	"fmt"

	"github.com/timothymamo/shift2eu/internal/adapter/internal/jsondoc"
)

// MaxBodySize is the largest request body Decode reads, in bytes: the inventory
// reader's cap (inventoryfile.MaxFileSize, 10 MiB) plus 64 KiB for the envelope
// and the target list. It is written out because an adapter imports no other
// adapter; a test pins it to that sum.
const MaxBodySize = 10<<20 + 64<<10

// ErrDecode is wrapped by every error Decode returns: a body that is not one
// well-formed request envelope.
var ErrDecode = errors.New("not a valid assessment request")

// MaxRegions is the most members the regions object may hold: far more than
// there are targets, so a valid request never meets it.
const MaxRegions = 64

// The keys of the request envelope.
const (
	keyInventory = "inventory"
	keyTargets   = "targets"
	keyRegions   = "regions"
	keyEstimate  = "estimate"
)

// envelopeKeys lists the keys of the request object: inventory and targets are
// required, regions and estimate optional. It is never modified.
var envelopeKeys = []jsondoc.Key{
	{Name: keyInventory, Required: true},
	{Name: keyTargets, Required: true},
	{Name: keyRegions},
	{Name: keyEstimate},
}

// Request is a decoded request body.
type Request struct {
	// Inventory is the inventory object as the raw bytes the body holds,
	// unchanged.
	Inventory []byte
	// Targets are the target keys in the order they were sent.
	Targets []string
	// Regions maps a target key to the one region to assess that target in, as
	// sent; nil when the body has no regions or an empty object. Neither the
	// keys nor the regions are validated here.
	Regions map[string]string
	// Estimate turns plans and estimates on: true unless the body says false.
	Estimate bool
}

// Decode strictly decodes body, the body of POST /v1/assessments, and returns
// the Request it holds. It refuses, with an error that wraps ErrDecode and the
// zero Request: a body over MaxBodySize, anything jsondoc.Open refuses, an
// unknown, inexact or repeated key, a missing required key, an inventory that
// is not an object, targets that are not a non-empty array of strings, regions
// that are not an object of strings (at most MaxRegions, no key twice), and an
// estimate that is not a boolean. null is refused for every key. No target
// key and no region is validated here.
func Decode(body []byte) (Request, error) {
	doc, err := jsondoc.Open(body, MaxBodySize, ErrDecode)
	if err != nil {
		return Request{}, err
	}

	w := doc.Walker()
	req := Request{Estimate: true}

	err = w.Object(envelopeKeys, func(key string) error {
		var err error

		switch key {
		case keyInventory:
			req.Inventory, err = object(w)
		case keyTargets:
			req.Targets, err = targetKeys(w)
		case keyRegions:
			req.Regions, err = regions(w)
		case keyEstimate:
			req.Estimate, err = w.Bool()
		}

		return err
	})
	if err != nil {
		return Request{}, err
	}

	return req, nil
}

// regions reads the next value, which must be an object of strings, and
// returns its members, or nil for an empty object. A region is never empty: an
// empty one would silently mean every region, so it is refused, as the CLI's
// --region refuses it. The walker names a member by its index, never by its key,
// so no error quotes a key or a value the client sent.
func regions(w *jsondoc.Walker) (map[string]string, error) {
	at := w.Position()

	var out map[string]string

	err := w.Members("an object of strings", func(i int, key string) error {
		if i >= MaxRegions {
			return fmt.Errorf("%w: %s at %s holds more than %d members", ErrDecode, keyRegions, at, MaxRegions)
		}

		// The path names the member by its index (regions[0]), never by its
		// key, so it holds nothing the client sent.
		path := w.Path()

		region, err := w.String()
		if err != nil {
			return err
		}

		if region == "" {
			return fmt.Errorf("%w: %q is empty, want a region", ErrDecode, path)
		}

		if out == nil {
			out = make(map[string]string)
		}

		out[key] = region

		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

// object reads the next value, which must be a JSON object, and returns its
// JSON text unchanged. Only its type is checked: its contents are the inventory
// reader's to decode.
func object(w *jsondoc.Walker) ([]byte, error) {
	at := w.Position()

	raw, err := w.Raw()
	if err != nil {
		return nil, err
	}

	if raw[0] != '{' {
		return nil, fmt.Errorf("%w: %s at %s is %s, want an object", ErrDecode, keyInventory, at, kindOf(raw))
	}

	return raw, nil
}

// kindOf names the JSON type of raw, with its article, never its value.
func kindOf(raw []byte) string {
	switch raw[0] {
	case '[':
		return "an array"
	case '"':
		return "a string"
	case 't', 'f':
		return "a boolean"
	case 'n':
		return "null"
	default:
		return "a number"
	}
}

// targetKeys reads the next value, which must be a non-empty array of strings,
// and returns its strings in order.
func targetKeys(w *jsondoc.Walker) ([]string, error) {
	at := w.Position()

	var keys []string

	err := w.Array("an array of strings", func(int) error {
		key, err := w.String()
		if err != nil {
			return err
		}

		keys = append(keys, key)

		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: %s at %s is empty, want at least one target", ErrDecode, keyTargets, at)
	}

	return keys, nil
}
