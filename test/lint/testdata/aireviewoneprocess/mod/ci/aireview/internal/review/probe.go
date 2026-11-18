// The AI reviewer's library stand-in for TestAIReviewerOneProcessProbe (PRD #161
// milestone 4, ADR 0033). The library starts the Claude Code CLI once, with a
// context, writes the system prompt to a file of a temporary directory it makes
// and removes, and reads no file: the pull request and the instructions reach it
// as the strings ci/aireview hands it, so nothing of the checkout or the runner's
// home is a file it opens.
//
// Every line below that ends in a want comment is a call the group
// ai-reviewer-one-process must refuse; the calls it keeps carry no want comment,
// so a group that stops firing, or one that reaches exec.CommandContext or
// os.WriteFile, fails the probe. Nothing here runs: the probe lints the file.
package review

import (
	"context"
	"os"
	"os/exec"
)

// reads runs a process with no context and reads a file: the group refuses both,
// the process because nothing can then time it out or cancel it, the read because
// the library reads no file at all.
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

	dir, err := os.MkdirTemp("", "shift2eu-aireview-")
	if err != nil {
		return
	}

	_ = os.RemoveAll(dir)
}
