package lint_test

import "testing"

// The group composition-root-env (compositionRootTags in
// compositionroot_test.go) is forbidigo's group for the calls the composition
// root cmd/shift2eu may not make to the environment (issue #111): it reads no
// named variable, and since issue #152 it locates no home, config, cache or temp
// directory, which read $HOME, $XDG_CONFIG_HOME, $XDG_CACHE_HOME and $TMPDIR
// without naming them (the auditor's finding M-B: without that entry a root could
// read ~/.aws/credentials with lint green). It has no exception: the one it had,
// the HTTP API's token (ADR 0019), went with the API (ADR 0037), and no
// linters.exclusions.rules rule narrows on one of its entries' msgs, which
// TestCompositionRootExclusions and compositionRootRuleProblems check.
//
// TestCompositionRootForbidigoPatterns pins those names by group, so a name
// cannot leave the group unseen; this file pins them entry by entry, so a name
// cannot move from one entry's pattern into another's either - os.UserHomeDir out
// of the homeDir entry into the os.Getenv one, or a syscall read into the os one -
// and the pattern that must refuse each name is named. The group holds for every
// file under cmd/shift2eu/, _test.go files included.

// compositionRootEnvReadMsg is the msg of both entries that refuse one named
// variable, the os one and the syscall one; they differ in their pkg alone, which
// is why a pin names the pkg as well. compositionRootEnvHomeDirMsg is the msg of
// the entry that refuses the four lookups which name no variable.
const (
	compositionRootEnvReadMsg    = "composition-root-env: a composition root reads no environment variable; take the value as a flag"
	compositionRootEnvHomeDirMsg = "composition-root-env: a composition root locates no home, config, cache or temp directory, where a credentials file sits; take the value as a flag"
)

// compositionRootEnvEntries lists every entry of the group, named by its msg and
// the package its pkg matches, with the exact set of names its pattern must
// refuse: the os reads of a named variable, the four os lookups that locate a
// directory from an unnamed one, and the two syscall reads.
var compositionRootEnvEntries = []forbidGroupEntry{
	{
		msg: compositionRootEnvReadMsg,
		pkg: "os",
		refuses: []rootName{
			{"os", "os.Getenv"},
			{"os", "os.LookupEnv"},
			{"os", "os.ExpandEnv"},
		},
	},
	{
		msg: compositionRootEnvHomeDirMsg,
		pkg: "os",
		refuses: []rootName{
			{"os", "os.UserHomeDir"},
			{"os", "os.UserConfigDir"},
			{"os", "os.UserCacheDir"},
			{"os", "os.TempDir"},
		},
	},
	{
		msg: compositionRootEnvReadMsg,
		pkg: "syscall",
		refuses: []rootName{
			{"syscall", "syscall.Getenv"},
			{"syscall", "syscall.GetEnvironmentVariable"},
		},
	},
}

// TestCompositionRootEnvEntryPatterns checks the entries of the
// composition-root-env group one by one: the entry each pin names refuses every
// name the pin lists and no other listed name, no entry refuses a name
// envAllowed lists, and every entry of the group is pinned. Without it a name
// could be moved from one entry's pattern into another's - os.UserHomeDir out of
// the homeDir entry into the os.Getenv entry, os.TempDir into the syscall one -
// and lint would stay green for that pattern while the entry the name left
// refuses less than its msg says.
func TestCompositionRootEnvEntryPatterns(t *testing.T) {
	t.Parallel()

	checkForbidigoGroupEntries(t, "composition-root-env", compositionRootEnvEntries, envAllowed)
}
