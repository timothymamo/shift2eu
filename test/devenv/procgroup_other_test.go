//go:build !unix

package devenv_test

import (
	"os/exec"
	"testing"
)

// processGroup does nothing where there are no process groups: the scripts under test run under sh on the Mac
// and in the Linux VM only, and the package is merely compiled for the other release targets by make lint.
type processGroup struct{}

// newProcessGroup, join and end do nothing where there are no process groups; see processGroup.
func newProcessGroup(*testing.T) *processGroup { return &processGroup{} }

func (*processGroup) join(*exec.Cmd) {}

func (*processGroup) end() {}
