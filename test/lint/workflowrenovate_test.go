// Pins finding A8 of the auditor's report on issue #277: the customManager that
// reads an annotated version pin names the workflows it scans one by one, so a
// `# renovate: datasource=` pin in any other workflow is a pin nothing bumps. A
// workflow joins that list when it gains such a pin - the audit's workflow of PRD
// #175's milestone 2 will hold the sandbox's own release pins - and until it does,
// the pin goes stale without a sound (CLAUDE.md rule 7).
package lint_test

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheAuditWorkflowIsScanned pins that the generic manager scans the audit's
// workflow too: .github/workflows/scheduled-security-audit.yml is PR 5b of PRD
// #175's milestone 2 and holds the sandbox's own release pins (age, bubblewrap,
// socat and the checks' tools), so a pin there that the manager's file patterns
// do not reach is a pin nothing bumps. The fixtures under testdata are no part of
// this: Renovate reads the repository, not a test's documents.
func TestTheAuditWorkflowIsScanned(t *testing.T) {
	t.Parallel()

	const auditWorkflowFile = ".github/workflows/scheduled-security-audit.yml"

	managers := renovateCustomManagers(t, moduleRoot(t))
	generic, _ := aiReviewGenericManagerIndex(t, managers)

	if !renovateManagerScans(t, managers[generic], auditWorkflowFile) {
		t.Errorf("%s: the file patterns of the manager described %q are %v, which do not scan %s: the audit workflow's annotated pins would never be bumped, and nothing says so (CLAUDE.md rule 7, PRD #175 milestone 2)",
			renovateConfigPath, managers[generic].description, managers[generic].filePatterns, auditWorkflowFile)
	}
}

// renovateAnnotation starts the comment Renovate's generic manager reads a version
// pin from, and the one this test finds in the workflows.
const renovateAnnotation = "# renovate: datasource="

// workflowsDir is the directory of the repository's workflows, a slash-separated
// path from the root.
const workflowsDir = ".github/workflows"

// TestEveryAnnotatedWorkflowPinIsScanned pins that every workflow holding an
// annotated pin is scanned by the generic manager: the manager's file patterns,
// read from .github/renovate.json5 as Renovate reads them, must match that
// workflow's path.
func TestEveryAnnotatedWorkflowPinIsScanned(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	managers := renovateCustomManagers(t, root)
	generic, _ := aiReviewGenericManagerIndex(t, managers)

	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(workflowsDir)))
	if err != nil {
		t.Fatalf("read %s: %v; the workflows are where the annotated pins live (CLAUDE.md rule 7)", workflowsDir, err)
	}

	annotated := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		rel := path.Join(workflowsDir, entry.Name())

		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}

		if !strings.Contains(string(body), renovateAnnotation) {
			continue
		}

		annotated++

		if !renovateManagerScans(t, managers[generic], rel) {
			t.Errorf("%s: %s holds a %q pin, and the file patterns of the manager described %q are %v, which do not scan it: Renovate would never bump that pin, and nothing says so (CLAUDE.md rule 7, issue #277)",
				renovateConfigPath, rel, renovateAnnotation, managers[generic].description, managers[generic].filePatterns)
		}
	}

	if annotated == 0 {
		t.Fatalf("no file under %s holds %q, so this test proves nothing: the workflows' annotated pins are what it is about (CLAUDE.md rule 7)", workflowsDir, renovateAnnotation)
	}
}
