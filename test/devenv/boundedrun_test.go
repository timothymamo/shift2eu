package devenv_test

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// errCommandTimedOut is what runBounded's error wraps when the command outlived its bound and was killed.
var errCommandTimedOut = errors.New("command timed out")

// boundedWaitDelay bounds how long runBounded's Wait waits for the command's stdout and stderr to close once the
// command itself exited, or was killed: a grandchild left running with them open would otherwise block Wait,
// and the subtest, until it ends (issue #512).
const boundedWaitDelay = 5 * time.Second

// runBounded runs cmd, which must not have been started, with its stdout and stderr captured, and kills it once
// it has run for timeout: on unix the whole process group it starts, so a recipe's shell and whatever it started
// go too (boundedgroup_unix_test.go), elsewhere the process alone. It returns what the command wrote and nil on
// exit 0, cmd.Run's error (an *exec.ExitError on a non-zero exit) otherwise, and on the timeout an error wrapping
// errCommandTimedOut that names the bound, the command line, its directory and what it wrote until then, so a
// blocked command fails its test with all a reader needs, instead of blocking it until go test's package timeout
// names nothing (issue #512). A command that exited on its own before the bound fired, while a grandchild kept
// its stdout or stderr open, is never reported as a timeout: the kill still ends that grandchild, and the
// command's own status is returned. stdin stays as the caller left it: nil, /dev/null.
func runBounded(cmd *exec.Cmd, timeout time.Duration) (stdout, stderr string, err error) {
	var outBuf, errBuf bytes.Buffer

	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.WaitDelay = boundedWaitDelay

	group, err := startBounded(cmd)
	if err != nil {
		return "", "", err
	}

	defer group.release()

	// The buffers are read only after done is closed, which orders that read after Wait finished writing them.
	var status error

	done := make(chan struct{})

	go func() {
		status = cmd.Wait()

		close(done)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-done:
		return outBuf.String(), errBuf.String(), status
	case <-timer.C:
		// Wait may have returned just as the bound fired, and select picks among ready channels at random.
		select {
		case <-done:
			return outBuf.String(), errBuf.String(), status
		default:
		}

		// The kill ends the command if it still runs, and whatever it started that holds its stdout or stderr
		// open, so Wait returns.
		group.kill()
		<-done

		// A command that had exited on its own before the kill, while a grandchild kept Wait waiting for its
		// pipes, ran within its bound: its own status is the run's result, never a timeout (issue #512).
		if endedOnItsOwn(cmd.ProcessState) {
			return outBuf.String(), errBuf.String(), status
		}

		// The wait status (the kill's) is only text here: wrapped, a timeout would errors.As-match the
		// *exec.ExitError a non-zero exit returns.
		waitStatus := fmt.Sprint(status)

		return outBuf.String(), errBuf.String(), fmt.Errorf("%w after %s and killed: %s in %s (wait: %s)\nstdout so far:\n%s\nstderr so far:\n%s",
			errCommandTimedOut, timeout, cmd.String(), cmd.Dir, waitStatus, outBuf.String(), errBuf.String())
	}
}
