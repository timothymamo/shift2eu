//go:build unix

package main

import (
	"os"
	"syscall"
)

// main replaces the wrapper with the command its arguments name, through
// syscall.Exec, the one place the wrapper leaves Go (ADR 0040).
func main() {
	os.Exit(run(os.Args[1:], syscall.Exec, os.Stderr))
}
