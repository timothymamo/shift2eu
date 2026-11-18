//go:build unix

package devenv_test

import (
	"os/exec"
	"syscall"
)

// inOwnGroup makes cmd start in a process group of its own, whose id is its pid, so killGroup can end every
// process it starts, the children it leaves behind included.
func inOwnGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup ends every process left in the group of the leader pid, which inOwnGroup created. It ignores
// ESRCH: the group is empty already. A group id stays reserved while any process is in the group, so the
// signal reaches another process only after the pid was freed and reused as a group id, which a test's
// lifetime is far too short for.
func killGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
