// Pins the Makefile's part in the agents App's credential (PRD #161 milestone 1): VM_ENV names no
// SHIFT2EU_AGENTS_APP_* variable, make vm-env refuses one a maintainer adds rather than copying it into the
// VM, and make agents-token / make agents-token-refresh run the mint and the refresh.
package devenv_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// agentsAppPrefix is the prefix of the three inputs dev/agents-app-token.sh reads from the maintainer's
// direnv .envrc: the App ID, the installation ID and the path of the App's key. None may enter the VM.
const agentsAppPrefix = "SHIFT2EU_AGENTS_APP_"

func TestMakefileVMENVNamesNoAgentsAppVariable(t *testing.T) {
	t.Parallel()

	body := readRepoFile(t, makefilePath)

	value, ok := makeVariable(body, "VM_ENV")
	if !ok {
		t.Fatalf("%s has no 'VM_ENV ?=' assignment", makefilePath)
	}

	names := strings.Split(value, ",")
	if len(names) == 0 || strings.TrimSpace(value) == "" {
		t.Fatalf("VM_ENV's default is empty: vm-env copies the variables it lists from your shell into the VM")
	}

	for _, name := range names {
		name = strings.TrimSpace(name)
		if strings.HasPrefix(name, agentsAppPrefix) {
			t.Errorf("VM_ENV's default names %s: the App's key and its path stay on the Mac, and only the installation token enters the VM (PRD #161 milestone 1)", name)
		}
	}
}

// TestMakeVMEnvRefusesAnAgentsAppVariable pins the second half: a maintainer who adds an App variable to
// VM_ENV, or passes one on the command line, gets a refusal and not a copy of the key's path into the VM.
func TestMakeVMEnvRefusesAnAgentsAppVariable(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)

	env := sb.baseEnv()
	env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = "/home/tester/invented/agents-app.pem"

	stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=SHIFT2EU_AGENTS_APP_KEY_FILE"}, env)
	if err == nil {
		t.Fatalf("make vm-env exited 0 with VM_ENV=SHIFT2EU_AGENTS_APP_KEY_FILE: vm-env must refuse an App variable, so neither the key nor its path enters the VM\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if log := sb.argvLogText(t); log != "" {
		t.Errorf("make vm-env called limactl before it refused an App variable, so something may have entered the VM:\n%s", log)
	}

	if !strings.Contains(stdout+stderr, "SHIFT2EU_AGENTS_APP_KEY_FILE") {
		t.Errorf("the refusal does not name SHIFT2EU_AGENTS_APP_KEY_FILE, so a maintainer cannot tell what vm-env refused:\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if _, err := os.Stat(sb.vmPath(".config/dot-agent-deck/daemon.env")); err == nil {
		t.Errorf("make vm-env wrote the VM's ~/.config/dot-agent-deck/daemon.env although it refused an App variable")
	}
}

func TestMakefileHasTheAgentsTokenTargets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		target string
		want   string
	}{
		{target: "agents-token", want: "agents-app-token.sh mint"},
		{target: "agents-token-refresh", want: "agents-app-token.sh refresh"},
	}

	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			t.Parallel()

			stdout, stderr, err := runMake(t, []string{"-n", tc.target}, makeEnv(t, nil))
			if err != nil {
				t.Fatalf("make -n %s: %v\nstderr: %s", tc.target, err, stderr)
			}

			if !strings.Contains(stdout, tc.want) {
				t.Errorf("make -n %s does not run %q:\n%s", tc.target, tc.want, stdout)
			}
		})
	}
}

// runMake runs make in the repository root with args and env alone, never the test process's own
// environment.
func runMake(t *testing.T, args []string, env map[string]string) (string, string, error) {
	t.Helper()

	if _, err := exec.LookPath("make"); err != nil {
		t.Fatalf("make is not installed: the repository's commands, and this test, need it")
	}

	root, err := filepath.Abs(moduleRoot)
	if err != nil {
		t.Fatalf("resolve the repository root: %v", err)
	}

	cmd := exec.Command("make", append([]string{"-C", root, "--no-print-directory"}, args...)...)
	cmd.Env = envList(env)
	cmd.Dir = root

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	return stdout.String(), stderr.String(), err
}

// makeEnv is the environment make runs with in a test: a PATH of the machine's own and a temporary HOME, so
// the Makefile's $(shell git config ...) reads nothing of the maintainer's.
func makeEnv(t *testing.T, extra map[string]string) map[string]string {
	t.Helper()

	env := map[string]string{
		"PATH": "/usr/local/bin:/usr/bin:/bin",
		"HOME": t.TempDir(),
	}

	for name, value := range extra {
		env[name] = value
	}

	return env
}

// makeVariable returns the value a substitution assignment in the Makefile gives name.
func makeVariable(body, name string) (string, bool) {
	match := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*[?:]?=\s*(.*)$`).FindStringSubmatch(body)
	if match == nil {
		return "", false
	}

	return strings.TrimSpace(match[1]), true
}
