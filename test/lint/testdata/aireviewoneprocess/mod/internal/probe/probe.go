// A package outside ci/aireview/internal/review, for TestAIReviewerOneProcessProbe
// (PRD #161 milestone 4, ADR 0033): the group ai-reviewer-one-process holds for the
// library's non-test files and nowhere else, so a file read here is refused by no
// entry of it and carries no want comment. The probe fails if the group widens to
// this package.
package probe

import "os"

// read is the file read the group must not reach outside the library.
func read(path string) ([]byte, error) {
	return os.ReadFile(path)
}
