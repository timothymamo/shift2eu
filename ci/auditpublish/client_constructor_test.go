package main

// The source check of the artifacts subcommand's client (PRD #175 milestone 2,
// PRD #175 review, ADR 0040): main.go builds it through the constructor that is
// read-only whatever its config says, never through github.NewClient, whose
// ReadOnly field a caller may leave out. No behaviour test can tell the two
// apart, because the artifact check only reads.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"
)

// TestMainBuildsItsClientOnlyThroughNewArtifactsClient pins how main.go builds
// the artifacts subcommand's client: through github.NewArtifactsClient, which
// writes nothing even when its config's ReadOnly is false, and never through
// github.NewClient, whose ReadOnly a caller drops in one line that every
// behaviour test still passes (CheckArtifacts only sends a GET).
func TestMainBuildsItsClientOnlyThroughNewArtifactsClient(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	file, err := parser.ParseFile(token.NewFileSet(), "main.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	var general, readOnlyByConstruction int

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "github" {
			return true
		}

		switch sel.Sel.Name {
		case "NewClient":
			general++
		case "NewArtifactsClient":
			readOnlyByConstruction++
		}

		return true
	})

	if general != 0 {
		t.Errorf("main.go calls github.NewClient %d time(s), want none: the artifacts client is read-only by construction, github.NewArtifactsClient", general)
	}

	if readOnlyByConstruction == 0 {
		t.Error("main.go calls github.NewArtifactsClient nowhere, want the artifacts subcommand's client built through it")
	}
}
