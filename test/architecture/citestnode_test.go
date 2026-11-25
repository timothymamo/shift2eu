// Pins ci.yml's Test job to the Node TestPiEnvDenyExtensionLoads has to run on
// (issue #548, ADR 0044).
//
// The load check in pienvdenyload_test.go cannot see the one failure that costs
// it its whole point: on a runner without the Node that strips TypeScript types
// it skips, and unless SHIFT2EU_REQUIRE_NODE=1 tells it to fail instead, CI
// passes with the extension never loaded. So the Test job must install the Node
// dev/lima.yaml pins, check that the node on the PATH is that one, and set
// SHIFT2EU_REQUIRE_NODE=1 on the step that runs the tests. Those four steps are
// declared once in test/architecture/internal/check (nodeSteps, issue #554),
// which holds release.yml's Test job to them through its closed schema
// (releaseworkflow.go) and ci.yml's through check.CITestJobNode; the edits that
// break them are pinned there, in citestnode_test.go and releaseworkflow_test.go.
package architecture_test

import (
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// TestCITestJobPinsTheNodeTheExtensionLoadRuns pins that the repository's
// ci.yml sets up dev/lima.yaml's Node in its Test job, checks that it is the
// one on the PATH, and runs the tests with SHIFT2EU_REQUIRE_NODE=1, in that
// order (issues #548, #554).
func TestCITestJobPinsTheNodeTheExtensionLoadRuns(t *testing.T) {
	t.Parallel()

	for _, message := range check.CITestJobNode(readRepositoryFile(t, ".github/workflows/ci.yml")) {
		t.Errorf("%s: the Test job must set up dev/lima.yaml's Node for TestPiEnvDenyExtensionLoads and run the tests with SHIFT2EU_REQUIRE_NODE=1 (issue #548)", message)
	}
}
