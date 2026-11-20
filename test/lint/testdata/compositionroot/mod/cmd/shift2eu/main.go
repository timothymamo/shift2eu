//go:build !windows

// Command shift2eu is the composition root, for
// TestCompositionRootForbidigoProbe. Every line that ends in a want comment
// holds a call that one of the two composition-root groups refuses, and the
// comment names the group's tag, which starts the msg of every entry in it:
// composition-root-no-exec for a process, a raw system call and the whole
// environment, and composition-root-env for one named variable. There is a line
// for every function the design forbids, and the same calls in internal/cli must pass, as the groups hold for the
// composition roots only. The file is for the host's platform: the windows
// entry points of syscall are in probe_windows.go, which the probe's windows run
// lints.
package main

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

func main() {}

// start starts a process or replaces this one with it, which would run a
// program the operator of a report did not ask for.
func start() {
	_, _ = os.StartProcess("/bin/true", nil, nil)              // want "composition-root-no-exec: "
	_ = syscall.Exec("/bin/true", nil, nil)                    // want "composition-root-no-exec: "
	_, _ = syscall.ForkExec("/bin/true", nil, nil)             // want "composition-root-no-exec: "
	_, _, _ = syscall.StartProcess("/bin/true", nil, nil)      // want "composition-root-no-exec: "
	_ = exec.Command("/bin/true")                              // want "composition-root-no-exec: "
	_ = exec.CommandContext(context.Background(), "/bin/true") // want "composition-root-no-exec: "
	_, _ = exec.LookPath("true")                               // want "composition-root-no-exec: "
	_ = os.Environ()                                           // want "composition-root-no-exec: "
	_ = syscall.Environ()                                      // want "composition-root-no-exec: "
}

// syscalls makes the kernel call the process entry points are built on, and
// names the number of the exec call: a composition root that may not start a
// process may not do it this way either.
func syscalls() {
	_, _, _ = syscall.Syscall(0, 0, 0, 0)              // want "composition-root-no-exec: "
	_, _, _ = syscall.Syscall6(0, 0, 0, 0, 0, 0, 0)    // want "composition-root-no-exec: "
	_, _, _ = syscall.RawSyscall(0, 0, 0, 0)           // want "composition-root-no-exec: "
	_, _, _ = syscall.RawSyscall6(0, 0, 0, 0, 0, 0, 0) // want "composition-root-no-exec: "
	_ = syscall.SYS_EXECVE                             // want "composition-root-no-exec: "
}

// environment reads one named variable, or locates a directory from an unnamed
// one, and the composition-root-env group refuses both, with no exception.
func environment() {
	_ = os.Getenv("SHIFT2EU_TOKEN")         // want "composition-root-env: "
	_, _ = os.LookupEnv("SHIFT2EU_TOKEN")   // want "composition-root-env: "
	_, _ = syscall.Getenv("SHIFT2EU_TOKEN") // want "composition-root-env: "
	_ = os.ExpandEnv("$SHIFT2EU_TOKEN")     // want "composition-root-env: "

	_, _ = os.UserHomeDir()   // want "composition-root-env: "
	_, _ = os.UserConfigDir() // want "composition-root-env: "
	_, _ = os.UserCacheDir()  // want "composition-root-env: "
	_ = os.TempDir()          // want "composition-root-env: "
}
