package site_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// This file holds the other half of PRD #297's Layer 1, the check of the
// syscall/js glue: the constant names it reads, sets and calls must hold none of
// the APIs that send, load or reach another window or browsing context. The
// glue never computes a name, so its allow list of jsName constants is the whole
// set of JavaScript names it can touch; a messaging name or an HTML sink name in
// that list would hand a page's data out or write markup, whatever the page
// script's scan says.

// bannedJSNames are the names the glue's allow list may never hold: the
// messaging APIs that reach another window or browsing context, the message
// listener, and the HTML sinks and dynamic-code calls through which a value
// becomes code or markup. The CSP governs none of the first group, which is why
// Layer 1 refuses them by name (PRD #297, Layer 1).
var bannedJSNames = []string{
	// Messaging to another window or browsing context.
	"postMessage",
	"opener",
	"parent",
	"top",
	"BroadcastChannel",
	"MessageChannel",
	"name",
	"onmessage",
	"addEventListener",
	// The HTML sinks and the calls that run a string as code.
	"innerHTML",
	"outerHTML",
	"insertAdjacentHTML",
	"write",
	"writeln",
	"srcdoc",
	"setHTMLUnsafe",
	"createPolicy",
	"eval",
	"Function",
}

// jsNameConstants returns the string value of every constant of type jsName
// declared in src, in declaration order.
func jsNameConstants(t *testing.T, name string, src []byte) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}

	var names []string

	ast.Inspect(file, func(node ast.Node) bool {
		declaration, ok := node.(*ast.GenDecl)
		if !ok || declaration.Tok != token.CONST {
			return true
		}

		for _, spec := range declaration.Specs {
			value, isValue := spec.(*ast.ValueSpec)
			if !isValue {
				continue
			}

			typeName, isIdent := "", false

			if ident, ok := value.Type.(*ast.Ident); ok {
				typeName, isIdent = ident.Name, true
			}

			if !isIdent || typeName != "jsName" {
				continue
			}

			for _, expression := range value.Values {
				literal, ok := expression.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}

				unquoted, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatalf("unquote %s in %s: %v", literal.Value, name, err)
				}

				names = append(names, unquoted)
			}
		}

		return true
	})

	return names
}

// bannedNamesIn returns the entries of names that are in bannedJSNames, in the
// order names holds them.
func bannedNamesIn(names []string) []string {
	var banned []string

	for _, name := range names {
		if slices.Contains(bannedJSNames, name) {
			banned = append(banned, name)
		}
	}

	return banned
}

// TestGlueAllowListHoldsNoSendingName pins that the syscall/js glue's allow list
// of jsName constants holds none of bannedJSNames (PRD #297, Layer 1): the glue
// reads, sets and calls JavaScript only through get, set and call, which take a
// jsName, and it never computes one, so this list is every name it can touch.
func TestGlueAllowListHoldsNoSendingName(t *testing.T) {
	t.Parallel()

	glueDir := filepath.Join(theRepositoryRoot(t), "cmd", "shift2eu-web")

	entries, err := os.ReadDir(glueDir)
	if err != nil {
		t.Fatalf("read the glue's directory %s: %v", glueDir, err)
	}

	var names []string

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		src, err := os.ReadFile(filepath.Join(glueDir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}

		names = append(names, jsNameConstants(t, entry.Name(), src)...)
	}

	if len(names) == 0 {
		t.Fatal("the glue declares no jsName constant, so this check read nothing: it registers one function through a named allow list of JavaScript names")
	}

	if !slices.Contains(names, "shift2euAssess") {
		t.Errorf("the glue's jsName constants are %v, which do not hold shift2euAssess, the one function main registers: the check would be reading another list", names)
	}

	for _, banned := range bannedNamesIn(names) {
		t.Errorf("the glue's allow list holds %q: the glue reads, sets and calls only names from that list, so a messaging or HTML sink name there reaches another window or writes markup (PRD #297, Layer 1)", banned)
	}
}

// jsValueNamedMethods are js.Value's methods that take a property or method
// name. The glue calls them only inside its get, set and call helpers, and only
// with string(name), the name of an entry of its jsName allow list: it never
// computes a name and never passes a literal, so that list is every name it can
// touch (PRD #297, Layer 1).
var jsValueNamedMethods = []string{"Get", "Set", "Delete", "Call"}

// jsValueAnonymousMethods are js.Value's methods that take no name: New and
// Invoke act as the value they are called on, so the glue may call them only on
// a value it obtained through get, whose name came from the allow list.
var jsValueAnonymousMethods = []string{"New", "Invoke"}

// glueHelpers are the functions the named methods are confined to: each takes
// the js.Value and the jsName, and converts the name for the JavaScript call
// itself.
var glueHelpers = []string{"get", "set", "call"}

// method is one method call of a source: the name called and the expression it
// is called on.
type method struct {
	receiver ast.Expr
	name     string
}

// methodCall returns the method call's name and receiver, and ok false for a
// call that is no method call.
func methodCall(call *ast.CallExpr) (method, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return method{}, false
	}

	return method{receiver: selector.X, name: selector.Sel.Name}, true
}

// receiverText names the expression a method is called on, for a failure
// message.
func receiverText(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return receiverText(e.X) + "." + e.Sel.Name
	case *ast.CallExpr:
		return "a call's result"
	default:
		return "a value"
	}
}

// stringName reports whether call passes string(name) as its name argument,
// with name a parameter of fn: the allow list's entry, converted for
// JavaScript, and never a computed or literal name.
func stringName(call *ast.CallExpr, fn *ast.FuncDecl) bool {
	if len(call.Args) == 0 {
		return false
	}

	conversion, ok := call.Args[0].(*ast.CallExpr)
	if !ok || len(conversion.Args) != 1 {
		return false
	}

	builtin, ok := conversion.Fun.(*ast.Ident)
	if !ok || builtin.Name != "string" {
		return false
	}

	arg, ok := conversion.Args[0].(*ast.Ident)
	if !ok {
		return false
	}

	return parameterNamed(fn, arg.Name)
}

// parameterNamed reports whether fn has a parameter with the name.
func parameterNamed(fn *ast.FuncDecl, name string) bool {
	for _, field := range fn.Type.Params.List {
		for _, ident := range field.Names {
			if ident.Name == name {
				return true
			}
		}
	}

	return false
}

// bindingsFromGet returns the names of the variables fn binds to a value get
// returned: the glue may call New and Invoke on those.
func bindingsFromGet(body *ast.BlockStmt) map[string]bool {
	bindings := make(map[string]bool)

	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			if n.Tok != token.DEFINE && n.Tok != token.ASSIGN {
				return true
			}

			for i, rhs := range n.Rhs {
				if !isGetCall(rhs) || i >= len(n.Lhs) {
					continue
				}

				if lhs, ok := n.Lhs[i].(*ast.Ident); ok && lhs.Name != "_" {
					bindings[lhs.Name] = true
				}
			}
		case *ast.ValueSpec:
			for i, value := range n.Values {
				if !isGetCall(value) || i >= len(n.Names) {
					continue
				}

				bindings[n.Names[i].Name] = true
			}
		}

		return true
	})

	return bindings
}

// isGetCall reports whether expr is a call of the glue's get helper.
func isGetCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}

	fun, ok := call.Fun.(*ast.Ident)

	return ok && fun.Name == "get"
}

// valueFromGet reports whether expr is a value get returned: a direct call of
// get, or a variable bound from one.
func valueFromGet(expr ast.Expr, bindings map[string]bool) bool {
	if isGetCall(expr) {
		return true
	}

	ident, ok := expr.(*ast.Ident)

	return ok && bindings[ident.Name]
}

// importedPackageNames returns the names a file imports its packages under, so
// a selector on one of them reads as a call of that package's function and not
// as a method of a JavaScript value (web.New is no js.Value.New).
func importedPackageNames(file *ast.File) map[string]bool {
	names := make(map[string]bool)

	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}

		name := path
		if slash := strings.LastIndex(path, "/"); slash >= 0 {
			name = path[slash+1:]
		}

		if spec.Name != nil {
			name = spec.Name.Name
		}

		if name != "_" && name != "." {
			names[name] = true
		}
	}

	return names
}

// glueNameFindings returns every way src breaks the glue's naming rule, and how
// many allowed calls each helper holds, so a check that read no call is caught.
func glueNameFindings(t *testing.T, name string, src []byte) ([]string, map[string]int) {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}

	packages := importedPackageNames(file)

	var findings []string

	uses := make(map[string]int)

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		helper := slices.Contains(glueHelpers, fn.Name.Name)
		fromGet := bindingsFromGet(fn.Body)

		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}

			called, ok := methodCall(call)
			if !ok {
				return true
			}

			// A selector on an imported package is that package's function,
			// not a method of a JavaScript value.
			if receiver, isIdent := called.receiver.(*ast.Ident); isIdent && packages[receiver.Name] {
				return true
			}

			switch {
			case slices.Contains(jsValueNamedMethods, called.name):
				switch {
				case !helper:
					findings = append(findings, fmt.Sprintf("%s: %s.%s is called outside the get, set and call helpers, which are the only places that pass an allow-listed name",
						name, receiverText(called.receiver), called.name))
				case !stringName(call, fn):
					findings = append(findings, fmt.Sprintf("%s: %s.%s does not take string(name), a parameter of %s, as its name: the glue passes only an entry of its jsName allow list, never a computed or literal name",
						name, receiverText(called.receiver), called.name, fn.Name.Name))
				default:
					uses[fn.Name.Name]++
				}
			case slices.Contains(jsValueAnonymousMethods, called.name):
				if !valueFromGet(called.receiver, fromGet) {
					findings = append(findings, fmt.Sprintf("%s: %s.%s is called on a value get did not return: New and Invoke act as the value they are called on, so they may reach only a value read through an allow-listed name",
						name, receiverText(called.receiver), called.name))
				}
			}

			return true
		})
	}

	return findings, uses
}

// TestGlueCallsOnlyAllowListedNames pins the rest of the glue's Layer 1 rule
// (PRD #297): js.Value's Get, Set, Delete and Call take a name, and the glue
// calls them only inside get, set and call, with string(name) for a name from its
// jsName allow list, so no name is computed and no literal name bypasses the
// list. New and Invoke take no name and act as the value they are called on, so
// the glue calls them only on a value get returned.
func TestGlueCallsOnlyAllowListedNames(t *testing.T) {
	t.Parallel()

	glueDir := filepath.Join(theRepositoryRoot(t), "cmd", "shift2eu-web")

	entries, err := os.ReadDir(glueDir)
	if err != nil {
		t.Fatalf("read the glue's directory %s: %v", glueDir, err)
	}

	uses := make(map[string]int)
	read := 0

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		src, err := os.ReadFile(filepath.Join(glueDir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}

		read++

		findings, fileUses := glueNameFindings(t, entry.Name(), src)

		for _, finding := range findings {
			t.Errorf("%s\nPRD #297, Layer 1: the glue calls js.Value's Get, Set, Delete and Call only with string(name) from its jsName allow list, inside get, set and call, and New and Invoke only on a value get returned", finding)
		}

		for helper, count := range fileUses {
			uses[helper] += count
		}
	}

	if read == 0 {
		t.Fatal("the glue's directory holds no non-test Go file, so this check read nothing")
	}

	for _, helper := range glueHelpers {
		if uses[helper] == 0 {
			t.Errorf("the glue's %s helper makes no call this check reads (%v), so the check would pass whatever %s does", helper, uses, helper)
		}
	}
}

// TestGlueNameCheckFires pins that the check above fires, and what it accepts:
// a named method outside the helpers, a literal or computed name inside one, and
// New or Invoke on a value get did not return must each be reported, while the
// helpers' own form and New on a value get returned must pass. Without this, a
// check that walked no call would pass silently.
func TestGlueNameCheckFires(t *testing.T) {
	t.Parallel()

	const helpers = `package main

import "syscall/js"

func get(v js.Value, name jsName) js.Value {
	return v.Get(string(name))
}

func set(v js.Value, p jsName, x any) {
	v.Set(string(p), x)
}

func call(v js.Value, name jsName, args ...any) js.Value {
	return v.Call(string(name), args...)
}
`

	findings, uses := glueNameFindings(t, "fixture.go", []byte(helpers))

	if len(findings) != 0 {
		t.Errorf("the helpers' own form was reported %v, want no finding: it is the form the glue uses", findings)
	}

	for _, helper := range glueHelpers {
		if uses[helper] != 1 {
			t.Errorf("the check read %d allowed calls in %s, want one: it reads the helper's one call", uses[helper], helper)
		}
	}

	tests := []struct {
		name string
		// src is a glue that adds one construct to the helpers above.
		src string
		// want is the part of the finding the check must make.
		want string
	}{
		{
			name: "a Get outside the helpers",
			src:  helpers + "\nfunc read(v js.Value, other string) {\n\tv.Get(other)\n}\n",
			want: "outside the get, set and call helpers",
		},
		{
			name: "a literal name inside a helper",
			src:  `package main` + "\n\nfunc get(v any, name jsName) any {\n\treturn v.Get(\"shift2euAssess\")\n}\n",
			want: "does not take string(name)",
		},
		{
			name: "a computed name inside a helper",
			src:  `package main` + "\n\nfunc call(v any, name jsName, args ...any) any {\n\treturn v.Call(string(name) + \"Suffix\", args...)\n}\n",
			want: "does not take string(name)",
		},
		{
			name: "the allow list's name passed without conversion",
			src:  `package main` + "\n\nfunc call(v any, name jsName, args ...any) any {\n\treturn v.Call(name, args...)\n}\n",
			want: "does not take string(name)",
		},
		{
			name: "a New on a value get did not return",
			src:  `package main` + "\n\nfunc start() {\n\tjs.Global().New(\"Worker\")\n}\n",
			want: "value get did not return",
		},
		{
			name: "an Invoke on a parameter",
			src:  `package main` + "\n\nfunc invokeAll(v any) {\n\tv.Invoke(\"run\")\n}\n",
			want: "value get did not return",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, _ := glueNameFindings(t, "fixture.go", []byte(tc.src))

			if len(got) == 0 {
				t.Fatalf("the check reported nothing for %s, want a finding holding %q", tc.name, tc.want)
			}

			if !slices.ContainsFunc(got, func(finding string) bool { return strings.Contains(finding, tc.want) }) {
				t.Errorf("the check reported %v for %s, want a finding holding %q", got, tc.name, tc.want)
			}
		})
	}

	t.Run("a New and an Invoke on a value get returned", func(t *testing.T) {
		t.Parallel()

		const src = `package main

func start(v any, name jsName) {
	value := get(v, name)
	value.New("Worker")
	get(v, name).Invoke("run")
}
`

		if got, _ := glueNameFindings(t, "fixture.go", []byte(src)); len(got) != 0 {
			t.Errorf("the check reported %v for a New and an Invoke on a value get returned, want no finding", got)
		}
	})

	t.Run("a package's New", func(t *testing.T) {
		t.Parallel()

		const src = `package main

import "github.com/timothymamo/shift2eu/internal/web"

func start(deps web.Deps) (*web.Assessor, error) {
	return web.New(deps)
}
`

		if got, _ := glueNameFindings(t, "fixture.go", []byte(src)); len(got) != 0 {
			t.Errorf("the check reported %v for web.New, a package's function, want no finding: only js.Value's New is a method", got)
		}
	})
}

// TestGlueAllowListCheckFires pins that the check above fires: a fixture glue
// that adds one banned name to its allow list must be reported. Without this, a
// check that read no constant or compared nothing would pass silently.
func TestGlueAllowListCheckFires(t *testing.T) {
	t.Parallel()

	const fixture = `package main

type jsName string

const (
	nameAssess jsName = "shift2euAssess"
	nameTargets jsName = "targets"
	nameBad jsName = "postMessage"
)
`

	names := jsNameConstants(t, "fixture.go", []byte(fixture))

	if want := []string{"shift2euAssess", "targets", "postMessage"}; !slices.Equal(names, want) {
		t.Fatalf("jsNameConstants read %v from the fixture, want %v", names, want)
	}

	if banned := bannedNamesIn(names); !slices.Equal(banned, []string{"postMessage"}) {
		t.Errorf("bannedNamesIn(%v) = %v, want [postMessage]", names, banned)
	}
}
