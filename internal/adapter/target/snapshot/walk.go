package snapshot

import (
	"fmt"

	"github.com/timothymamo/shift2eu/internal/adapter/internal/jsondoc"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// walk reads the whole document with w, a walker at its start, and returns what
// it holds. The document is a JSON object whose exact schemaVersion key decode
// has found to be 2. jsondoc refuses an unknown, inexact or repeated key, a
// value of the wrong JSON type, a missing key and a string that escapes a lone
// surrogate; walk decides the keys, all of them required but an offering's
// fixedFees (ADR 0026) and planBillingCapHours (ADR 0029), and reads every
// value without jsondoc.Walker.Null, so null is refused like any other value of
// the wrong type, and is never read as an absent optional key.
func walk(w *jsondoc.Walker) (document, error) {
	var doc document

	err := w.Object(documentKeys, func(key string) error {
		var err error

		switch key {
		case keySchemaVersion:
			// decode has found the version to be 2, and jsondoc refuses a
			// second schemaVersion key as repeated before its value is read;
			// the walk checks the value again all the same, so it never relies
			// on that.
			err = w.Version(schemaVersion, ErrSchemaVersion)
		case keyTarget:
			doc.target, err = w.String()
		case keySourceURL:
			doc.sourceURL, err = w.String()
		case keyRetrievedOn:
			doc.retrievedOn, err = w.String()
		case keyRegions:
			doc.regions, err = list[string](w)
		case keyDefaultRegion:
			doc.defaultRegion, err = w.String()
		case keyOfferings:
			doc.offerings, err = offerings(w)
		}

		return err
	})
	if err != nil {
		return document{}, err
	}

	return doc, nil
}

// list reads the next value: an array of strings, each escaping no lone
// surrogate, as values of type T. An empty array is an empty, non-nil list.
func list[T ~string](w *jsondoc.Walker) ([]T, error) {
	values := []T{}

	err := w.Array("an array of strings", func(int) error {
		s, err := w.String()
		values = append(values, T(s))

		return err
	})
	if err != nil {
		return nil, err
	}

	return values, nil
}

// offerings reads the offerings list: an array of offering objects, each with
// every key of offeringKeys.
func offerings(w *jsondoc.Walker) ([]offering, error) {
	values := []offering{}

	err := w.Array("an array of objects", func(int) error {
		o, err := readOffering(w)
		values = append(values, o)

		return err
	})
	if err != nil {
		return nil, err
	}

	return values, nil
}

// readOffering reads the next value: an offering object.
func readOffering(w *jsondoc.Walker) (offering, error) {
	var (
		o offering
		// emptyFees refuses an empty fixedFees array, once the object is read.
		emptyFees error
		// zeroCap refuses a planBillingCapHours of 0, once the object is read.
		zeroCap error
	)

	err := w.Object(offeringKeys, func(key string) error {
		var err error

		switch key {
		case keyName:
			o.name, err = w.String()
		case keyCategories:
			o.categories, err = list[taxonomy.Category](w)
		case keyCapabilities:
			o.capabilities, err = list[taxonomy.Capability](w)
		case keyRegions:
			o.regions, err = list[string](w)
		case keyPlans:
			o.plans, err = plans(w)
		case keyStoragePrices:
			o.storagePrices, err = prices(w)
		case keyFixedFees:
			o.fixedFees, emptyFees, err = fixedFees(w)
		case keyPlanBillingCapHours:
			o.planBillingCapHours, zeroCap, err = planBillingCapHours(w)
		}

		return err
	})
	if err != nil {
		return offering{}, err
	}

	if emptyFees != nil {
		return offering{}, emptyFees
	}

	if zeroCap != nil {
		return offering{}, zeroCap
	}

	return o, nil
}

// planBillingCapHours reads the next value: an integer, the most hours a month
// the offering bills one instance of one of its plans (ADR 0029). For 0 it
// returns, besides the value, the ErrDecode error that refuses it, so
// "uncapped" has one spelling (the key left out); the caller reports that error
// once the offering's object has been read, as it does an empty fixedFees. A
// negative value or one above catalog.MaxBillingCapHours is returned: that is
// catalog.New's rule.
func planBillingCapHours(w *jsondoc.Walker) (hours int64, zero, err error) {
	path, at := w.Path(), w.Position()

	hours, err = integer(w)
	if err != nil {
		return 0, nil, err
	}

	if hours == 0 {
		zero = fmt.Errorf("%w: %s at %s is 0, want 1 to %d (leave the key out for no cap)",
			ErrDecode, text.Quote(path), at, catalog.MaxBillingCapHours)
	}

	return hours, zero, nil
}

// fixedFees reads the next value: an array of price objects, an offering's
// fixed fees (ADR 0026). For an empty array it returns, besides the empty
// list, the ErrDecode error that refuses it, so "no fee" has one spelling (the
// key left out); the caller reports that error once the offering's object has
// been read, so a repeated or unknown key in it is reported first.
func fixedFees(w *jsondoc.Walker) (values []catalog.Price, empty, err error) {
	path, at := w.Path(), w.Position()

	values, err = prices(w)
	if err != nil {
		return nil, nil, err
	}

	if len(values) == 0 {
		empty = fmt.Errorf("%w: %s at %s is an empty array, want at least one fee (leave the key out for none)",
			ErrDecode, text.Quote(path), at)
	}

	return values, empty, nil
}

// plans reads the next value: an array of plan objects, each with every key of
// planKeys. An empty array is an empty, non-nil list.
func plans(w *jsondoc.Walker) ([]catalog.Plan, error) {
	values := []catalog.Plan{}

	err := w.Array("an array of objects", func(int) error {
		p, err := readPlan(w)
		values = append(values, p)

		return err
	})
	if err != nil {
		return nil, err
	}

	return values, nil
}

// readPlan reads the next value: a plan object. Its numbers are read from their
// JSON text (number.go); their ranges are checked by catalog.New.
func readPlan(w *jsondoc.Walker) (catalog.Plan, error) {
	var p catalog.Plan

	err := w.Object(planKeys, func(key string) error {
		var err error

		switch key {
		case keyName:
			p.Name, err = w.String()
		case keyVCPUs:
			p.VCPUs, err = integer(w)
		case keyMemoryGiB:
			p.MemoryMilliGiB, err = exactDecimal(w, memoryDigits)
		case keyStorageGiB:
			p.IncludedStorageGiB, err = integer(w)
		case keyPrices:
			p.Prices, err = prices(w)
		}

		return err
	})
	if err != nil {
		return catalog.Plan{}, err
	}

	return p, nil
}

// prices reads the next value: an array of price objects, each with every key
// of priceKeys. An empty array is an empty, non-nil list.
func prices(w *jsondoc.Walker) ([]catalog.Price, error) {
	values := []catalog.Price{}

	err := w.Array("an array of objects", func(int) error {
		p, err := readPrice(w)
		values = append(values, p)

		return err
	})
	if err != nil {
		return nil, err
	}

	return values, nil
}

// readPrice reads the next value: a price object. Its amount is read from its
// JSON text (number.go); its range and currency are checked by catalog.New.
func readPrice(w *jsondoc.Walker) (catalog.Price, error) {
	var p catalog.Price

	err := w.Object(priceKeys, func(key string) error {
		var err error

		switch key {
		case keyAmount:
			var amount int64

			amount, err = exactDecimal(w, amountDigits)
			p.Amount = catalog.Amount(amount)
		case keyCurrency:
			p.Currency, err = w.String()
		case keyRegions:
			p.Regions, err = list[string](w)
		}

		return err
	})
	if err != nil {
		return catalog.Price{}, err
	}

	return p, nil
}
