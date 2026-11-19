//go:build js && wasm

package main

import (
	"context"
	"errors"
	"syscall/js"

	"github.com/timothymamo/shift2eu/cmd/shift2eu-web/internal/wire"
	"github.com/timothymamo/shift2eu/internal/web"
)

// jsName is a JavaScript property or method name the glue reads, sets or
// calls. Every name the glue passes to js.Value's Get, Set and Call is one of
// the constants below, through get, set and call, which take only a jsName:
// they are the glue's named allow list (PRD #297, Layer 1), and the glue never
// computes a name. None of them reaches another window or browsing context.
type jsName string

// The glue's allow list of JavaScript names.
const (
	// nameAssess is the global function main registers:
	// shift2euAssess(inventory, options) runs one assessment. inventory is the
	// inventory document as a string; options is an object with targets (an
	// array of target key strings, or ["all"]), regions (an array of
	// {target, region} objects of strings), estimate (a boolean),
	// schemaVersion (a string, "" for the default) and inventoryName (a
	// string, "" for a pasted inventory). A missing property is its zero
	// value; a mistyped one, or an array of more than maxEntries entries, is a
	// usage error.
	nameAssess jsName = "shift2euAssess"

	nameTargets       jsName = "targets"
	nameRegions       jsName = "regions"
	nameEstimate      jsName = "estimate"
	nameSchemaVersion jsName = "schemaVersion"
	nameInventoryName jsName = "inventoryName"
	nameTarget        jsName = "target"
	nameRegion        jsName = "region"

	// nameArray and nameIsArray are Array.isArray, which tells a real array
	// from an object that only has a length.
	nameArray   jsName = "Array"
	nameIsArray jsName = "isArray"
)

// The kinds of a failure, as the page reads them.
const (
	kindUsage     = "usage"
	kindInventory = "inventory"
	kindInternal  = "internal"
)

// The constant messages of the glue's own failures. They echo no value the
// page passed.
const (
	msgWrongArguments = "the page called the module with the wrong arguments"
	msgBadOptions     = "the options are malformed: targets and regions must be arrays of at most 64 entries, " +
		"of strings and of {target, region} objects of strings, estimate a boolean, schemaVersion and inventoryName strings"
	msgPanic = "the assessment failed unexpectedly; the page can run another one"
)

// maxEntries bounds the length of each list the page passes, targets and
// regions alike. A longer array is refused before any entry is read, so a
// hostile length never makes the glue convert millions of entries. The bound
// is fixed and generous, far above any choice the page can make, so for any
// realistic input (a target named twice, more regions than targets) the
// option rules decide and their message, the CLI's usage error, is what the
// page shows. msgBadOptions names the same number.
const maxEntries = 64

func main() {
	deps := wire.Deps()

	assessor, err := web.New(deps)
	if err != nil {
		// A wiring mistake: the page finds no function and shows that the
		// module failed to start.
		panic(err)
	}

	set(js.Global(), nameAssess, js.FuncOf(func(_ js.Value, args []js.Value) any {
		return assess(assessor, args)
	}))

	// The module serves the page until the page is closed.
	select {}
}

// assess converts the page's arguments, runs the assessment and converts the
// result or the error for the page.
//
// The page must treat the returned object's fields as follows: error and kind
// are plain text, which it shows with textContent and never as markup; markdown
// and json are the downloads' bytes, which it offers as blobs and never
// inserts as HTML; only html is markup, the HTML reporter's fragment. A panic
// in the assessment is recovered and returned as an internal failure with a
// constant message, so the module stays usable for the next assessment.
func assess(assessor *web.Assessor, args []js.Value) (result any) {
	defer func() {
		if recover() != nil {
			result = failure(kindInternal, msgPanic)
		}
	}()

	if len(args) != 2 || args[0].Type() != js.TypeString || args[1].Type() != js.TypeObject {
		return failure(kindInternal, msgWrongArguments)
	}

	opts, ok := options(args[1])
	if !ok {
		return failure(kindUsage, msgBadOptions)
	}

	report, err := assessor.Assess(context.Background(), []byte(args[0].String()), opts)
	if err != nil {
		var webErr *web.Error
		if !errors.As(err, &webErr) {
			return failure(kindInternal, err.Error())
		}

		return failure(kindName(webErr.Kind), webErr.Error())
	}

	return map[string]any{
		"markdown": string(report.Markdown),
		"json":     string(report.JSON),
		"html":     string(report.HTML),
	}
}

// options converts the page's options object to web.Options. A missing
// property is the zero value, which web.Assessor.Assess checks; a mistyped
// property, a list that is no array or has more than maxEntries entries, or an
// entry of the wrong type makes ok false.
func options(v js.Value) (opts web.Options, ok bool) {
	estimate, ok := property(v, nameEstimate, js.TypeBoolean)
	if !ok {
		return web.Options{}, false
	}

	opts.Estimate = estimate.Truthy()

	if opts.SchemaVersion, ok = stringProperty(v, nameSchemaVersion); !ok {
		return web.Options{}, false
	}

	if opts.InventoryName, ok = stringProperty(v, nameInventoryName); !ok {
		return web.Options{}, false
	}

	targets, ok := array(v, nameTargets)
	if !ok {
		return web.Options{}, false
	}

	for _, target := range targets {
		if target.Type() != js.TypeString {
			return web.Options{}, false
		}

		opts.Targets = append(opts.Targets, target.String())
	}

	regions, ok := array(v, nameRegions)
	if !ok {
		return web.Options{}, false
	}

	for _, region := range regions {
		if region.Type() != js.TypeObject {
			return web.Options{}, false
		}

		target, targetOK := stringProperty(region, nameTarget)
		name, regionOK := stringProperty(region, nameRegion)

		if !targetOK || !regionOK {
			return web.Options{}, false
		}

		opts.Regions = append(opts.Regions, web.Region{Target: target, Region: name})
	}

	return opts, true
}

// array returns the entries of v's property name: none when it is missing,
// and ok false when it is no array (Array.isArray) or has more than
// maxEntries entries.
func array(v js.Value, name jsName) ([]js.Value, bool) {
	p := get(v, name)
	if p.IsUndefined() {
		return nil, true
	}

	if !call(get(js.Global(), nameArray), nameIsArray, p).Truthy() {
		return nil, false
	}

	n := p.Length()
	if n > maxEntries {
		return nil, false
	}

	entries := make([]js.Value, 0, n)
	for i := range n {
		entries = append(entries, p.Index(i))
	}

	return entries, true
}

// property returns v's property name, which is undefined when it is missing,
// and ok false when it is present with another type than typ.
func property(v js.Value, name jsName, typ js.Type) (js.Value, bool) {
	p := get(v, name)
	if p.IsUndefined() {
		return p, true
	}

	return p, p.Type() == typ
}

// stringProperty returns v's string property name, "" when it is missing, and
// ok false when it is not a string.
func stringProperty(v js.Value, name jsName) (string, bool) {
	p, ok := property(v, name, js.TypeString)
	if !ok || p.IsUndefined() {
		return "", ok
	}

	return p.String(), true
}

// get reads v's property name, one of the allow list's names.
func get(v js.Value, name jsName) js.Value {
	return v.Get(string(name))
}

// set sets v's property name, one of the allow list's names.
func set(v js.Value, name jsName, x any) {
	v.Set(string(name), x)
}

// call calls v's method name, one of the allow list's names.
func call(v js.Value, name jsName, args ...any) js.Value {
	return v.Call(string(name), args...)
}

// failure is the page's object for an error: kind and message are plain text.
func failure(kind, message string) map[string]any {
	return map[string]any{"error": message, "kind": kind}
}

// kindName names an error kind for the page.
func kindName(kind web.Kind) string {
	switch kind {
	case web.KindUsage:
		return kindUsage
	case web.KindInventory:
		return kindInventory
	default:
		return kindInternal
	}
}
