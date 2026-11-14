// Pins the git author of the agents (PRD #161 milestone 1, review item 5): vm-init's identity step lives in
// dev/vm-git-identity.sh <fallback user.name> <fallback user.email>, so it can be run on its own, and it reads
// the identity file make agents-token writes into the VM. The switch-over goes both ways: the bot's identity
// replaces the maintainer's, and with the identity file gone the fallback comes back, so a maintainer who rolls
// back commits as themselves again.
//
// vm-init itself cannot be run in make test: its first step refuses to run outside the Lima VM unless
// /etc/profile.d/shift2eu.sh exists, which a test cannot create without the VM's own provisioning. Its remaining
// claim, that it runs this script, is pinned in vminit_test.go.
package devenv_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// vmGitIdentityScript is the identity step, a script a test runs on its own.
const vmGitIdentityScript = "dev/vm-git-identity.sh"

// identityFixture is a throwaway HOME and global git config: the script writes there with git config --global,
// and a test reads back exactly what it wrote, never the machine's own git configuration.
type identityFixture struct {
	sb  *sandbox
	env map[string]string
}

// newIdentityFixture builds the fixture: HOME is the fake VM user's home, GIT_CONFIG_GLOBAL a file in the
// sandbox, and the system configuration is switched off.
func newIdentityFixture(t *testing.T) *identityFixture {
	t.Helper()

	sb := newSandbox(t)
	env := map[string]string{
		"PATH":                   sb.binDir + ":" + toolDir(t, "git") + ":/usr/local/bin:/usr/bin:/bin",
		"HOME":                   sb.vmHome,
		"GIT_CONFIG_GLOBAL":      filepath.Join(sb.root, "gitconfig"),
		"GIT_CONFIG_SYSTEM":      "/dev/null",
		"GIT_CONFIG_NOSYSTEM":    "1",
		"TMPDIR":                 filepath.Join(sb.root, "tmp"),
		"SHIFT2EU_TEST_ARGV_LOG": sb.argvLog,
		"SHIFT2EU_TEST_VM_HOME":  sb.vmHome,
	}

	return &identityFixture{sb: sb, env: env}
}

// run runs the identity step with the two fallbacks vm-init passes it, through the script's own interpreter,
// so the test runs it the way the VM does.
func (f *identityFixture) run(t *testing.T, name, email string) (string, string, error) {
	t.Helper()

	script := scriptPath(t, vmGitIdentityScript)

	cmd := exec.Command(shellOf(t, script), script, name, email)
	cmd.Env = envList(f.env)
	cmd.Dir = f.sb.hostHome

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return stdout.String(), stderr.String(), err
}

// shellOf returns the interpreter a script's shebang names, or sh when it has none.
func shellOf(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	first, _, _ := strings.Cut(string(body), "\n")
	if !strings.HasPrefix(first, "#!") {
		return "sh"
	}

	fields := strings.Fields(strings.TrimPrefix(first, "#!"))

	return fields[len(fields)-1]
}

// git runs git with the fixture's environment and returns its output.
func (f *identityFixture) git(t *testing.T, args ...string) (string, error) {
	t.Helper()

	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git is not installed: the test needs it")
	}

	cmd := exec.Command(git, args...)
	cmd.Env = envList(f.env)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", errors.New(err.Error() + "\nstderr: " + stderr.String())
	}

	return stdout.String(), nil
}

// set writes one key of the global git configuration.
func (f *identityFixture) set(t *testing.T, key, value string) {
	t.Helper()

	if _, err := f.git(t, "config", "--global", key, value); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

// get reads one key of the global git configuration, empty when it is not set.
func (f *identityFixture) get(t *testing.T, key string) string {
	t.Helper()

	out, err := f.git(t, "config", "--global", "--get", key)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(out)
}

// identity is git's global user.name and user.email.
func (f *identityFixture) identity(t *testing.T) (string, string) {
	t.Helper()

	return f.get(t, "user.name"), f.get(t, "user.email")
}

// config is the whole global git configuration, for the cases that must change nothing at all.
func (f *identityFixture) config(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(f.env["GIT_CONFIG_GLOBAL"])
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ""
		}

		t.Fatalf("read %s: %v", f.env["GIT_CONFIG_GLOBAL"], err)
	}

	return string(body)
}

// writeIdentityFile writes the identity file make agents-token would have written into the VM.
func (f *identityFixture) writeIdentityFile(t *testing.T, content string) {
	t.Helper()

	writeFileMode(t, f.sb.vmPath(agentsIdentityFile), []byte(content), 0o600)
}

func TestVMGitIdentitySetsTheAgentsAuthor(t *testing.T) {
	t.Parallel()

	const (
		botName       = "shift2eu-agents[bot]"
		botEmail      = "987654321+shift2eu-agents[bot]@users.noreply.github.com"
		fallbackName  = "Shift2eu Maintainer"
		fallbackEmail = "maintainer@example.com"
		humanName     = "Ada Lovelace"
		humanEmail    = "ada@example.com"
	)

	human := map[string]string{"user.name": humanName, "user.email": humanEmail}
	bot := map[string]string{"user.name": botName, "user.email": botEmail}

	cases := []struct {
		name         string
		fallbackName string
		fallbackMail string
		existing     map[string]string
		identityFile *string // nil: no identity file in the VM
		wantFailure  bool
		wantMessages []string
		wantName     string // the identity afterwards; empty together with wantFailure
		wantEmail    string
	}{
		{
			name:         "no identity file and no identity at all: the fallback is set",
			fallbackName: fallbackName,
			fallbackMail: fallbackEmail,
			wantName:     fallbackName,
			wantEmail:    fallbackEmail,
		},
		{
			name:         "no identity file and the maintainer's identity: it is kept",
			fallbackName: fallbackName,
			fallbackMail: fallbackEmail,
			existing:     human,
			wantMessages: []string{"kept"},
			wantName:     humanName,
			wantEmail:    humanEmail,
		},
		{
			name:         "the identity file replaces the maintainer's identity",
			fallbackName: fallbackName,
			fallbackMail: fallbackEmail,
			existing:     human,
			identityFile: identityText(botName + "\n" + botEmail + "\n"),
			wantName:     botName,
			wantEmail:    botEmail,
		},
		{
			name:         "the identity file removed: the fallback comes back over the bot's identity",
			fallbackName: fallbackName,
			fallbackMail: fallbackEmail,
			existing:     bot,
			wantName:     fallbackName,
			wantEmail:    fallbackEmail,
		},
		{
			name:         "no identity file and no fallback user.name",
			fallbackName: "",
			fallbackMail: fallbackEmail,
			wantFailure:  true,
			wantMessages: []string{"GIT_USER_NAME"},
		},
		{
			name:         "no identity file and no fallback user.email",
			fallbackName: fallbackName,
			fallbackMail: "",
			wantFailure:  true,
			wantMessages: []string{"GIT_USER_EMAIL"},
		},
		{
			name:         "the identity file's name is not an App's bot",
			fallbackName: fallbackName,
			fallbackMail: fallbackEmail,
			existing:     human,
			identityFile: identityText("shift2eu-agents\n" + botEmail + "\n"),
			wantFailure:  true,
		},
		{
			name:         "the identity file's email is not a bot's noreply address",
			fallbackName: fallbackName,
			fallbackMail: fallbackEmail,
			existing:     human,
			identityFile: identityText(botName + "\n123+shift2eu-agents[bot]@example.com\n"),
			wantFailure:  true,
		},
		{
			name:         "the identity file holds one line",
			fallbackName: fallbackName,
			fallbackMail: fallbackEmail,
			existing:     human,
			identityFile: identityText(botName + "\n"),
			wantFailure:  true,
		},
		{
			name:         "the identity file is empty",
			fallbackName: fallbackName,
			fallbackMail: fallbackEmail,
			existing:     human,
			identityFile: identityText(""),
			wantFailure:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newIdentityFixture(t)

			for key, value := range tc.existing {
				f.set(t, key, value)
			}

			before := f.config(t)

			if tc.identityFile != nil {
				f.writeIdentityFile(t, *tc.identityFile)
			}

			stdout, stderr, err := f.run(t, tc.fallbackName, tc.fallbackMail)

			if tc.wantFailure {
				if err == nil {
					t.Fatalf("the identity step exited 0 with %s; it must refuse it and exit non-zero\nstdout: %s", tc.name, stdout)
				}

				for _, want := range tc.wantMessages {
					if !strings.Contains(stdout+stderr, want) {
						t.Errorf("the refusal does not name %s, so a maintainer cannot tell what to fix: %s", want, stdout+stderr)
					}
				}

				if after := f.config(t); after != before {
					t.Errorf("the identity step changed the git configuration although it refused %s:\nbefore:\n%s\nafter:\n%s", tc.name, before, after)
				}

				return
			}

			if err != nil {
				t.Fatalf("the identity step exited non-zero: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
			}

			for _, want := range tc.wantMessages {
				if !strings.Contains(stdout+stderr, want) {
					t.Errorf("the identity step does not say %q, so a maintainer cannot see that the VM kept its own identity: %s", want, stdout+stderr)
				}
			}

			name, email := f.identity(t)

			if name != tc.wantName {
				t.Errorf("git user.name is %q, want %q: %s", name, tc.wantName, tc.name)
			}

			if email != tc.wantEmail {
				t.Errorf("git user.email is %q, want %q: %s", email, tc.wantEmail, tc.name)
			}
		})
	}
}

// identityText returns a pointer to s, for a case that has an identity file.
func identityText(s string) *string {
	return &s
}
