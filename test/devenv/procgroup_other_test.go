//go:build !unix

package devenv_test

import "os/exec"

// inOwnGroup does nothing where there are no process groups: the scripts under test run under sh on the Mac
// and in the Linux VM only, and the package is merely compiled for the other release targets by make lint.
func inOwnGroup(*exec.Cmd) {}

// killGroup does nothing where there are no process groups; see inOwnGroup.
func killGroup(int) {}
