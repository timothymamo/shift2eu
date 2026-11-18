//go:build unix

package devenv_test

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// processGroup is a process group the scripts a test starts run in, so the test can end every process they
// start, the children they leave behind included.
//
// Its leader is a holder process of the test's own, not a script: the group's id is the holder's pid, and only
// end reaps the holder. A pid is not reused while its process exists, a zombie included, so until end has
// reaped the holder the group's id names this group and no other. end signals the group before it reaps the
// holder, so the signal can reach no process outside the group, even when the scripts and every child of
// theirs are gone already (issue #267). A group led by a script would lose that guarantee the moment Wait
// reaped the script while nothing else was left in the group.
type processGroup struct {
	holder  *exec.Cmd
	release *os.File // the write end of the holder's stdin: the holder ends with the test binary at the latest
}

// newProcessGroup starts the holder of a new process group.
func newProcessGroup(t *testing.T) *processGroup {
	t.Helper()

	stdin, release, err := os.Pipe()
	if err != nil {
		t.Fatalf("create the stdin of a process group's holder: %v", err)
	}

	// cat waits for input on a pipe whose write end the test alone holds, so it runs until end kills it, and
	// ends by itself, without killing anyone, should the test binary die first.
	holder := exec.Command("cat")
	holder.Stdin = stdin
	holder.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	err = holder.Start()

	if closeErr := stdin.Close(); closeErr != nil {
		t.Errorf("close the stdin of a process group's holder: %v", closeErr)
	}

	if err != nil {
		_ = release.Close()

		t.Fatalf("start the holder of a process group: %v", err)
	}

	return &processGroup{holder: holder, release: release}
}

// join makes cmd start in the group.
func (g *processGroup) join(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: g.holder.Process.Pid}
}

// end kills every process in the group, the holder included, and then reaps the holder. It ignores the
// kill error, as ESRCH means only zombies are left in the group, and the holder's exit status: SIGKILL
// or whatever else ended it.
func (g *processGroup) end() {
	_ = syscall.Kill(-g.holder.Process.Pid, syscall.SIGKILL)
	_ = g.holder.Wait()
	_ = g.release.Close()
}
