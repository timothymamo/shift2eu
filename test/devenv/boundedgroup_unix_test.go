//go:build unix

package devenv_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// boundedGroup is the process group runBounded starts a command in, so a timeout can kill the command and
// every process it started. As processGroup's (procgroup_unix_test.go), its leader is a holder of runBounded's
// own, a cat on a pipe only runBounded writes to, not the command: until release reaps the holder its pid, and
// so the group's id, names this group and no other, so kill's group signal reaches no process outside it even
// when the command has been reaped already (CLAUDE.md, rule 10). It needs no *testing.T, unlike processGroup, and ends
// the holder by closing its stdin rather than by a signal: a process the command left behind on purpose keeps
// running unless the command timed out.
type boundedGroup struct {
	cmd    *exec.Cmd
	holder *exec.Cmd
	stdin  *os.File // the write end of the holder's stdin
}

// startBounded starts the holder of a new process group, and cmd in that group. cmd's SysProcAttr is runBounded's
// to set: a cmd that already has one is refused rather than silently moved to another group.
func startBounded(cmd *exec.Cmd) (*boundedGroup, error) {
	if cmd.SysProcAttr != nil {
		return nil, errors.New("runBounded sets the command's SysProcAttr itself, and it is set already: " + cmd.String())
	}

	stdin, write, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create the stdin of a process group's holder: %w", err)
	}

	holder := exec.Command("cat")
	holder.Stdin = stdin
	holder.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	err = holder.Start()

	_ = stdin.Close()

	if err != nil {
		_ = write.Close()

		return nil, fmt.Errorf("start the holder of a process group: %w", err)
	}

	group := &boundedGroup{holder: holder, stdin: write}

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: holder.Process.Pid}

	if err := cmd.Start(); err != nil {
		group.kill()
		group.release()

		return nil, err
	}

	group.cmd = cmd

	return group, nil
}

// kill sends SIGKILL to every process in the group, the holder included, and then to the command's own process
// through Go's handle, which reaches a command that moved itself out of the group (setsid). On Linux that handle
// is a pidfd, signalled with pidfd_send_signal, so it cannot reach another process (CLAUDE.md, rule 10).
// Elsewhere (darwin) Go keeps the bare pid and a done flag it sets only after wait4 has reaped the command, so a
// command that exits on its own exactly at the bound leaves an instant in which kill(2) goes to a freed pid: it
// harms another process only if that pid is reused within the instant, which is not ruled out but practically
// never happens. It ignores the errors: ESRCH means only zombies are left in the group, and os.ErrProcessDone
// that the command was reaped.
func (g *boundedGroup) kill() {
	_ = syscall.Kill(-g.holder.Process.Pid, syscall.SIGKILL)

	if g.cmd != nil {
		_ = g.cmd.Process.Kill()
	}
}

// release ends the holder, by closing its stdin unless kill ended it already, and reaps it. It ignores the
// holder's exit status: EOF or SIGKILL.
func (g *boundedGroup) release() {
	_ = g.stdin.Close()
	_ = g.holder.Wait()
}

// endedOnItsOwn reports whether the command, reaped with state, exited rather than died of a signal: kill's
// SIGKILL cannot be caught, so an exit is the command's own, from before the kill.
func endedOnItsOwn(state *os.ProcessState) bool {
	return state != nil && state.Exited()
}
