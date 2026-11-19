//go:build js && wasm

package main

import (
	"context"
	"errors"
	"syscall/js"

	"github.com/timothymamo/shift2eu/cmd/shift2eu-web/internal/wire"
	"github.com/timothymamo/shift2eu/internal/web"
)

// assessFunction is the name of the global JavaScript function main
// registers: shift2euAssess(inventory, options) runs one assessment. inventory is the
// inventory document as a string; options is an object with targets (an array
// of target keys, or ["all"]), regions (an array of {target, region}),
// estimate (a boolean), schemaVersion (a string, "" for the default) and
// inventoryName (a string, "" for a pasted inventory). It returns an object
// with markdown, json and html strings, or with error (the message) and kind
// ("usage", "inventory" or "internal").
const assessFunction = "shift2euAssess"

func main() {
	assessor, err := web.New(wire.Deps())
	if err != nil {
		// A wiring mistake: the page finds no function and shows that the
		// module failed to start.
		panic(err)
	}

	js.Global().Set(assessFunction, js.FuncOf(func(_ js.Value, args []js.Value) any {
		return assess(assessor, args)
	}))

	// The module serves the page until the page is closed.
	select {}
}

// assess converts the page's arguments, runs the assessment and converts the
// result or the error for the page.
func assess(assessor *web.Assessor, args []js.Value) any {
	if len(args) != 2 || args[0].Type() != js.TypeString || args[1].Type() != js.TypeObject {
		return failure("internal", "the page called the module with the wrong arguments")
	}

	result, err := assessor.Assess(context.Background(), []byte(args[0].String()), options(args[1]))
	if err != nil {
		var webErr *web.Error
		if !errors.As(err, &webErr) {
			return failure("internal", err.Error())
		}

		return failure(kindName(webErr.Kind), webErr.Error())
	}

	return map[string]any{
		"markdown": string(result.Markdown),
		"json":     string(result.JSON),
		"html":     string(result.HTML),
	}
}

// options converts the page's options object to web.Options. A missing or
// mistyped property is the zero value, which web.Assessor.Assess checks.
func options(v js.Value) web.Options {
	opts := web.Options{
		Estimate:      property(v, "estimate", js.TypeBoolean).Truthy(),
		SchemaVersion: stringProperty(v, "schemaVersion"),
		InventoryName: stringProperty(v, "inventoryName"),
	}

	if targets := property(v, "targets", js.TypeObject); targets.Truthy() {
		for i := range targets.Length() {
			opts.Targets = append(opts.Targets, targets.Index(i).String())
		}
	}

	if regions := property(v, "regions", js.TypeObject); regions.Truthy() {
		for i := range regions.Length() {
			region := regions.Index(i)
			opts.Regions = append(opts.Regions, web.Region{
				Target: stringProperty(region, "target"),
				Region: stringProperty(region, "region"),
			})
		}
	}

	return opts
}

// property returns v's property name when it has type typ, and undefined
// otherwise.
func property(v js.Value, name string, typ js.Type) js.Value {
	if v.Type() != js.TypeObject {
		return js.Undefined()
	}

	p := v.Get(name)
	if p.Type() != typ {
		return js.Undefined()
	}

	return p
}

// stringProperty returns v's string property name, or "".
func stringProperty(v js.Value, name string) string {
	p := property(v, name, js.TypeString)
	if p.IsUndefined() {
		return ""
	}

	return p.String()
}

// failure is the page's object for an error.
func failure(kind, message string) map[string]any {
	return map[string]any{"error": message, "kind": kind}
}

// kindName names an error kind for the page.
func kindName(kind web.Kind) string {
	switch kind {
	case web.KindUsage:
		return "usage"
	case web.KindInventory:
		return "inventory"
	default:
		return "internal"
	}
}
