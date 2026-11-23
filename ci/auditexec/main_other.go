//go:build !unix

package main

import (
	"fmt"
	"os"
)

// main refuses to run: the wrapper replaces itself with a command through
// syscall.Exec, which only a unix system has, and the audit's runner is Linux
// (ADR 0040). The file exists so the package builds, and is linted, on every
// release target.
func main() {
	fmt.Fprintln(os.Stderr, "audit-exec: refused: runs on a unix system only")
	os.Exit(exitRefused)
}
