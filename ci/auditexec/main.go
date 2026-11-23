//go:build unix

package main

import (
	"os"
	"syscall"
)

// main replaces the wrapper with the command its arguments name, through
// syscall.Exec, the one place the wrapper leaves Go, after changing into its
// fixed working directory with os.Chdir (ADR 0040).
func main() {
	os.Exit(run(os.Args[1:], os.Chdir, syscall.Exec, os.Stderr))
}
