package architecture_test

import (
	"strings"
	"testing"
)

// bwrapProfile is the AppArmor profile the audit workflow's model job loads.
const bwrapProfile = ".github/security-audit/bwrap.apparmor"

// bwrapProfileRules are the profile's lines that are no comment and not blank,
// exactly: the ABI, the global tunables, and one profile for /usr/bin/bwrap
// alone that grants userns and confines nothing else.
var bwrapProfileRules = []string{
	"abi <abi/4.0>,",
	"include <tunables/global>",
	"profile shift2eu-audit-bwrap /usr/bin/bwrap flags=(unconfined) {",
	"  userns,",
	"}",
}

// TestBwrapAppArmorProfile pins the AppArmor profile of ADR 0040's note on the
// runner's user-namespace restriction line for line: the model job loads it with
// apparmor_parser -r while sudo still works, so a widened profile (another
// program, another rule, a local include that the runner image could fill)
// would apply to the sandbox the audit's model runs in. The comments are free.
func TestBwrapAppArmorProfile(t *testing.T) {
	t.Parallel()

	var got []string

	for line := range strings.SplitSeq(string(readRepositoryFile(t, bwrapProfile)), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		got = append(got, line)
	}

	if strings.Join(got, "\n") != strings.Join(bwrapProfileRules, "\n") {
		t.Errorf("%s holds the rules %q, want exactly %q: the profile grants userns to /usr/bin/bwrap alone (ADR 0040)", bwrapProfile, got, bwrapProfileRules)
	}
}
