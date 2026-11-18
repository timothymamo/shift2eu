// The library's own tests for TestAIReviewerOneProcessProbe (PRD #161 milestone 4,
// ADR 0033): the group ai-reviewer-one-process holds for the library's non-test
// files only, so a test there reads its fixtures and runs the go command the way a
// test does. Neither call carries a want comment, so the probe fails if the group
// reaches this file. Nothing here runs: the probe lints the file.
package review

import (
	"os"
	"os/exec"
	"testing"
)

func TestProbe(t *testing.T) {
	if _, err := os.ReadFile("probe.go"); err != nil {
		t.Fatal(err)
	}

	_ = exec.Command("go", "version")
}
