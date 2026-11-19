package site_test

import (
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
