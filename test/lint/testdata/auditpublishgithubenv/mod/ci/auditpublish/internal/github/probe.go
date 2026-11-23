// Package github is a probe module for TestAuditPublishGitHubEnvForbidigoProbe.
// It stands where the publish program's GitHub side does, so the group
// audit-publish-github-env holds for it, and every line that ends in a want
// comment holds a call one of the group's entries refuses. There is a line for
// every name the group refuses: the environment reads and the four lookups of a
// home, config, cache or temp directory, os.StartProcess, the whole environment,
// and the two os/exec calls with no context and no absolute path. The comment
// names the group's tag, which starts the msg of every entry in it.
package github

import (
	"os"
	"os/exec"
)

// environment reads and hands over the environment, locates a directory from it,
// and starts a process through os, which the group's first and second entries
// refuse.
func environment() {
	_ = os.Getenv("GH_TOKEN")                                     // want "audit-publish-github-env: "
	_, _ = os.LookupEnv("GH_TOKEN")                               // want "audit-publish-github-env: "
	_ = os.ExpandEnv("$GH_TOKEN")                                 // want "audit-publish-github-env: "
	_ = os.Expand("$GH_TOKEN", func(string) string { return "" }) // want "audit-publish-github-env: "
	_ = os.Environ()                                              // want "audit-publish-github-env: "
	_, _ = os.UserHomeDir()                                       // want "audit-publish-github-env: "
	_, _ = os.UserConfigDir()                                     // want "audit-publish-github-env: "
	_, _ = os.UserCacheDir()                                      // want "audit-publish-github-env: "
	_ = os.TempDir()                                              // want "audit-publish-github-env: "
	_, _ = os.StartProcess("/bin/true", nil, nil)                 // want "audit-publish-github-env: "
}

// programs starts a program with no context and looks one up through PATH, which
// the group's last entry refuses: the library starts gh and gitleaks by absolute
// path with exec.CommandContext alone.
func programs() {
	_ = exec.Command("gh")     // want "audit-publish-github-env: "
	_, _ = exec.LookPath("gh") // want "audit-publish-github-env: "
}
