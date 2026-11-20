// Package cli stands for the CLI's production code, which neither
// composition-root group reaches: those two hold for files under cmd/shift2eu/
// only, which their linters.exclusions.rules say and
// TestCompositionRootExclusions checks. What the CLI may not do is its own
// group's business (issue #152): cli-no-env-or-process refuses every way to
// read, write or dump the environment, to locate a home, config, cache or temp
// directory, and to start a process, so a composition root that delegates the
// read to the CLI cannot read the variable a root may not. Every line below
// that ends in a want comment is one of those calls, and the want names the
// group's tag.
//
// The calls the CLI really makes keep no want comment: it opens the inventory a
// user names, reads it, and creates the report the user asked for (--inventory,
// --output). os stays allowed in the CLI's import rule for exactly that reason,
// and the group names the calls on the environment, not the package.
//
// The imports the CLI may not make at all (syscall, unsafe, os/exec) are held by
// the CLI's import rule, cli-gets-adapters-from-cmd, which
// TestCLIImportsProbe lints; this file imports os alone, as the CLI does today.
package cli

import "os"

// Environment reads and dumps the environment and locates the directories the
// environment names, which the CLI's group refuses. The writes — os.Setenv,
// os.Unsetenv, os.Clearenv and os.Chdir — are no call of this group: the group
// tests-no-process-state refuses them in every file of the module but the two
// harnesses under test/ (issue #153), so one group owns them and no line is
// reported twice.
func Environment() {
	_ = os.Environ()                              // want "cli-no-env-or-process: "
	_ = os.Getenv("SHIFT2EU_TOKEN")               // want "cli-no-env-or-process: "
	_, _ = os.LookupEnv("SHIFT2EU_TOKEN")         // want "cli-no-env-or-process: "
	_ = os.ExpandEnv("$SHIFT2EU_TOKEN")           // want "cli-no-env-or-process: "
	_, _ = os.StartProcess("/bin/true", nil, nil) // want "cli-no-env-or-process: "
	_, _ = os.UserHomeDir()                       // want "cli-no-env-or-process: "
	_, _ = os.UserConfigDir()                     // want "cli-no-env-or-process: "
	_, _ = os.UserCacheDir()                      // want "cli-no-env-or-process: "
	_ = os.TempDir()                              // want "cli-no-env-or-process: "
	// A method value is no call, and the group must refuse it too: taken
	// alone, it hands the read to whoever the CLI passes it to.
	_ = os.Getenv // want "cli-no-env-or-process: "
}

// Files reads the inventory a user names and writes the report the user asked
// for, which is what the CLI is for: none of these lines carries a want, so a
// group that refuses the package as a whole, or a pattern that lost its $ and
// reaches os.Open, fails the probe.
func Files(inventory, output string) {
	_, _ = os.Open(inventory)
	_, _ = os.OpenFile(inventory, os.O_RDONLY, 0o600)
	_, _ = os.ReadFile(inventory)
	_, _ = os.Create(output)
	_, _ = os.CreateTemp("", ".shift2eu-report-*.tmp")
	_ = os.WriteFile(output, nil, 0o600)
	_ = os.MkdirAll("example", 0o750)
	_, _ = os.Stat(inventory)
	os.Exit(0)
	_ = os.Getpid()
}
