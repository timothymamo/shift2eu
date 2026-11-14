// Pins the git author dev/vm-init.sh gives the agents (PRD #161 milestone 1): when the agents' identity file
// is in the VM, the agents commit as the App's bot, through user.name and user.email; without it, vm-init
// keeps the identity the maintainer passed in, as today.
//
// The script cannot be run in make test: its first step refuses to run outside the Lima VM unless
// /etc/profile.d/shift2eu.sh exists, and a test cannot create that file without root and without the VM's
// own provisioning. So the file's identity-file seam is pinned here, and the behaviour it feeds is checked
// by hand in the VM as the switch-over runbook's step 3 says.
package devenv_test

import (
	"strings"
	"testing"
)

func TestVMInitGivesTheAgentsTheBotIdentity(t *testing.T) {
	t.Parallel()

	body := readRepoFile(t, vmInitScript)

	// The identity file's name is the seam the design fixes: ~/.config/shift2eu/agents-app-identity, two
	// lines, git user.name then user.email. A vm-init that does not read it leaves every agent commit
	// authored by the maintainer, which the switch-over cannot tell apart from a hand-made commit.
	at := strings.Index(body, "agents-app-identity")
	if at < 0 {
		t.Fatalf("%s does not read %s: with an identity file in the VM its two lines are the agents' git user.name and user.email (PRD #161 milestone 1, the agents' identity)", vmInitScript, agentsIdentityFile)
	}

	rest := body[at:]
	for _, key := range []string{"user.name", "user.email"} {
		if !strings.Contains(rest, key) {
			t.Errorf("%s mentions %s but never sets git %s after it: both lines of the identity file are the agents' author", vmInitScript, agentsIdentityFile, key)
		}
	}
}
