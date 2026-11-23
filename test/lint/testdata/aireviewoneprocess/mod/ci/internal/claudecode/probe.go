// The shared Claude Code launcher's stand-in for TestAIReviewerOneProcessProbe
// (issue #277, ADR 0040's model-job amendment). The launcher the AI reviewer asks
// through starts the Claude Code CLI once, with a context, writes the system
// prompt to a file of a temporary directory it makes and removes, and reads no
// file: its callers hand it the CLI's path, the token, the model and the tools.
//
// Every line below that ends in a want comment is a call the group
// ai-reviewer-one-process must refuse here as in the reviewer's library; the
// calls it keeps carry no want comment, so a group whose scope leaves the
// launcher out, or one that reaches exec.CommandContext or os.WriteFile, fails
// the probe. Nothing here runs: the probe lints the file.
package claudecode

import (
	"context"
	"os"
	"os/exec"
)

// reads runs a process with no context and reads a file: the group refuses both.
func reads(path string) {
	_ = exec.Command("claude", "-p") // want "ai-reviewer-one-process: "

	_, _ = os.ReadFile(path)           // want "ai-reviewer-one-process: "
	_, _ = os.Open(path)               // want "ai-reviewer-one-process: "
	_, _ = os.OpenFile(path, 0, 0o600) // want "ai-reviewer-one-process: "
	_, _ = os.ReadDir(path)            // want "ai-reviewer-one-process: "
}

// writes runs the one process with a context, writes the system prompt to a
// temporary file and removes the directory it made: the group keeps all three.
func writes(ctx context.Context, cli, path string, prompt []byte) {
	_ = exec.CommandContext(ctx, cli, "-p")
	_ = os.WriteFile(path, prompt, 0o600)

	dir, err := os.MkdirTemp("", "claudecode-cwd-")
	if err != nil {
		return
	}

	_ = os.RemoveAll(dir)
}
