//go:build !unix

package devenv_test

import (
	"os"
	"os/exec"
)

// boundedGroup is the command alone where there are no process groups: the package's commands run on the Mac
// and in the Linux VM only, and it is merely compiled for the other release targets by make lint.
type boundedGroup struct {
	cmd *exec.Cmd
}

// startBounded starts cmd; see the unix boundedGroup.
func startBounded(cmd *exec.Cmd) (*boundedGroup, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &boundedGroup{cmd: cmd}, nil
}

// kill kills the command's process, ignoring the error of one that ended already.
func (g *boundedGroup) kill() { _ = g.cmd.Process.Kill() }

// release has nothing to end here.
func (*boundedGroup) release() {}

// endedOnItsOwn cannot tell a kill from an exit here, where a killed process exits too: it reports none, so a
// run the bound cut short stays a timeout.
func endedOnItsOwn(*os.ProcessState) bool { return false }
