// Pins that vm-init runs the git identity step of PRD #161 milestone 1 through dev/vm-git-identity.sh, whose
// behaviour is tested for real in vmgitidentity_test.go (review item 5).
//
// vm-init itself cannot be run in make test: its first step refuses to run outside the Lima VM unless
// /etc/profile.d/shift2eu.sh exists, and a test cannot create that file without root and without the VM's own
// provisioning. So the file's call of the identity step is pinned here, and the behaviour it feeds is checked
// by the script's own tests.
package devenv_test

import (
	"strings"
	"testing"
)

func TestVMInitRunsTheGitIdentityScript(t *testing.T) {
	t.Parallel()

	body := readRepoFile(t, vmInitScript)

	// The identity step is a script of its own, so its switch-over cases can be tested without a VM: vm-init
	// must not carry a copy of that logic, or the tested script is not the one that runs.
	at := strings.Index(body, "vm-git-identity.sh")
	if at < 0 {
		t.Fatalf("%s does not run dev/vm-git-identity.sh: with an identity file in the VM the agents commit as the App's bot, and with it gone they commit as the identity vm-init was given (PRD #161 milestone 1, review item 5)", vmInitScript)
	}

	// The two fallbacks git user.name and user.email are the script's arguments: the line that runs it must
	// hand it both, or the rollback to the maintainer's identity has nothing to fall back to.
	line := body[strings.LastIndex(body[:at], "\n")+1:]
	if end := strings.Index(line, "\n"); end >= 0 {
		line = line[:end]
	}

	if got := strings.Count(line, "$"); got < 2 {
		t.Errorf("%s runs the identity step without two arguments: %q. It passes the fallback user.name and user.email, which the step writes when the VM has no identity file", vmInitScript, strings.TrimSpace(line))
	}
}
