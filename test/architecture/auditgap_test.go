package architecture_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	// auditPublishPackage is the audit's publish core, the package whose one
	// gap constant the check reads (PRD #175 milestone 2, ADR 0040).
	auditPublishPackage = "ci/auditpublish/internal/publish"

	// auditWorkflow is the audit's workflow, which carries the schedule
	// trigger from milestone 3 on.
	auditWorkflow = ".github/workflows/scheduled-security-audit.yml"

	// gapThresholdName is the one constant the report's gap line is stated in.
	gapThresholdName = "GapThresholdDays"

	// gapThresholdDays is the value the constant must have while the workflow
	// has no schedule trigger: the longest month, 31 days, plus 7 days of
	// slack, for the monthly cron the ADR records.
	gapThresholdDays = 38
)

// scheduleTrigger matches a YAML schedule: key indented under the workflow's
// on: mapping.
var scheduleTrigger = regexp.MustCompile(`(?m)^ {2}schedule:\s*$`)

// gapDeclaration is one declaration of the constant the check reads.
type gapDeclaration struct {
	file string
	line int
	// kind is the declaration's keyword, const or var.
	kind string
	// literal is the integer literal the declaration assigns, empty when the
	// value is no integer literal.
	literal string
}

// TestGapThresholdIsPinnedOnce reads the audit's publish core and fails unless
// GapThresholdDays is declared exactly once, as an integer literal, and equals
// the threshold the ADR records (PRD #175 milestone 2, ADR 0040). The report's
// gap line and the staleness check of the release-tag and dispatch skills are
// stated in that one number, so a second declaration, a computed value or
// another value would let the two drift apart unseen.
func TestGapThresholdIsPinnedOnce(t *testing.T) {
	t.Parallel()

	declarations, files := gapDeclarations(t)

	if files == 0 {
		t.Fatalf("no Go file of %s was read, so this check would hold nothing; add the package and the constant with it", auditPublishPackage)
	}

	if len(declarations) != 1 {
		t.Fatalf("%s is declared %d times in %s, want exactly once, as an integer literal %d:\n%s",
			gapThresholdName, len(declarations), auditPublishPackage, gapThresholdDays, formatDeclarations(declarations))
	}

	declaration := declarations[0]

	if declaration.kind != "const" {
		t.Errorf("%s:%d: %s is declared with %s, want const: the threshold is one pinned constant",
			declaration.file, declaration.line, gapThresholdName, declaration.kind)
	}

	if declaration.literal != strconv.Itoa(gapThresholdDays) {
		t.Errorf("%s:%d: %s is declared as %q, want the integer literal %d: a computed value or another number would drift from the skills' staleness check and the ADR",
			declaration.file, declaration.line, gapThresholdName, declaration.literal, gapThresholdDays)
	}
}

// TestAuditWorkflowHasNoScheduleTrigger pins the other half of the constant's
// check: while the audit's workflow has no schedule trigger, the threshold is
// the literal above; milestone 3, which adds the trigger, replaces it with the
// cron-derived check the PRD states. The workflow does not exist yet, which is
// the "no trigger" case: the PRD's fifth pull request adds it.
func TestAuditWorkflowHasNoScheduleTrigger(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), filepath.FromSlash(auditWorkflow)))

	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist):
		return
	default:
		t.Fatalf("read %s: %v", auditWorkflow, err)
	}

	if line := scheduleTrigger.FindIndex(data); line != nil {
		t.Errorf("%s holds a schedule: trigger at byte %d, want none while %s is the literal %d: milestone 3 adds the trigger and changes this check to the cron's longest gap plus 7 days",
			auditWorkflow, line[0], gapThresholdName, gapThresholdDays)
	}
}

// gapDeclarations returns every declaration of GapThresholdDays in the publish
// core's Go files, and how many files were read: a declaration with no value,
// or a value that is no integer literal, is one the check reports with an empty
// literal.
func gapDeclarations(t *testing.T) ([]gapDeclaration, int) {
	t.Helper()

	dir := filepath.Join(repositoryRoot(t), filepath.FromSlash(auditPublishPackage))

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", auditPublishPackage, err)
	}

	var (
		declarations []gapDeclaration
		read         int
	)

	fset := token.NewFileSet()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}

		name := filepath.Join(dir, entry.Name())

		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		read++

		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST && gen.Tok != token.VAR {
				continue
			}

			for _, spec := range gen.Specs {
				values, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}

				for i, ident := range values.Names {
					if ident.Name != gapThresholdName {
						continue
					}

					literal := ""

					if i < len(values.Values) {
						if lit, ok := values.Values[i].(*ast.BasicLit); ok && lit.Kind == token.INT {
							literal = lit.Value
						} else {
							literal = "an expression"
						}
					}

					declarations = append(declarations, gapDeclaration{
						file:    auditPublishPackage + "/" + entry.Name(),
						line:    fset.Position(ident.Pos()).Line,
						kind:    gen.Tok.String(),
						literal: literal,
					})
				}
			}
		}
	}

	return declarations, read
}

// formatDeclarations returns one line per declaration, for a failure message.
func formatDeclarations(declarations []gapDeclaration) string {
	if len(declarations) == 0 {
		return "  (none)"
	}

	lines := make([]string, 0, len(declarations))

	for _, d := range declarations {
		lines = append(lines, "  "+d.file+":"+strconv.Itoa(d.line)+": "+d.kind+" "+gapThresholdName+" "+d.literal)
	}

	return strings.Join(lines, "\n")
}
