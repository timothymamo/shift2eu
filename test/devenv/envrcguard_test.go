// Pins the guard issue #402 asks for: the host targets that load the VM credentials refuse to run when the
// maintainer's direnv setup reads a .envrc from somewhere other than the host clone's own .envrc, or when a
// .envrc or .env sits in the workspace the VM mounts or in a directory above it. docs/dev-environment.md, The
// host clone, says the VM's credentials (CLAUDE_CODE_OAUTH_TOKEN, DEEPSEEK_API_KEY) live only in the host
// clone's .envrc, never in a .envrc in or above the workspace: the workspace is the directory the VM mounts, so
// a .envrc there - or above it, which direnv finds with source_up - hands the credentials to every Mac shell the
// maintainer opens in it, and the VM's credentials are meant for the VM alone (issue #227, ADR 0031).
//
// Every row runs one of the eight host targets that load the credentials in a sandbox layout - the host clone make
// runs in, and the workspace whose checkout WORKSPACE_REPO names - and pins one of two things: a refusal (a
// non-zero exit, a message on stderr naming the file the maintainer must move, and no call of any tool the fakes
// record, so the guard ran before anything else), or a pass (the target reaches its next step, which is what the
// guard must not stop). The message of a refusal holds names and paths only: both files the tests write hold
// made-up credential values, and no output may carry either of them (CLAUDE.md rule 3, issue #402).
//
// The host targets that load the credentials are vm-env, the three that run it or its check (vm-up, vm-create,
// vm-init), the two that start or install the agents App token refresh (agents-token-refresh-start,
// agents-token-refresh-install) and the two foreground ones that mint the App's token in the maintainer's shell
// with the App's key in reach (agents-token, agents-token-refresh). The tests refuse rather than warn: vm-env copies the credentials into the VM,
// so a run with a .envrc in the wrong place is the moment to stop it. vm-env-check, the check the three run
// first, is refused on its own too, so a maintainer who runs it alone learns of the layout as well.
package devenv_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// envrcGuardTargets are the eight host targets issue #402 gives the guard to: the ones that load the VM's
// credentials, directly (vm-env) or through the checks and sub-makes around it, and the four that run the agents
// App's token script with the App's key in reach (the two that mint in the foreground and the two that start or
// install the background refresh).
var envrcGuardTargets = []string{
	"agents-token",
	"agents-token-refresh",
	"agents-token-refresh-install",
	"agents-token-refresh-start",
	"vm-create",
	"vm-env",
	"vm-init",
	"vm-up",
}

// The made-up credential values the tests plant in every file they write, each on the line of a credential
// docs/dev-environment.md keeps in the host clone's .envrc. Neither is a credential (CLAUDE.md rule 3), and a
// guard that read one of them must never print it (issue #402).
const (
	plantedOAuthValue    = "planted-oauth-value-402"
	plantedDeepseekValue = "planted-deepseek-value-402"
)

// plantedValues are the two made-up values, as a message may not hold them.
var plantedValues = []string{plantedOAuthValue, plantedDeepseekValue}

// plantedEnvrcLines are the two lines every .envrc and .env the tests write holds beside the lines the case is
// about: the credentials, written the way the maintainer's direnv .envrc holds them. Neither value may appear in
// what a refused run prints (issue #402).
func plantedEnvrcLines() string {
	return "CLAUDE_CODE_OAUTH_TOKEN=" + plantedOAuthValue + "\n" +
		"DEEPSEEK_API_KEY=" + plantedDeepseekValue + "\n"
}

// envrcGuardTools are the tools a refused run must not have reached: every tool a host target runs as the
// maintainer, and the account check's and SSH mode's tools, which the two token targets run first. The fakes of
// the sandbox record each call, so one call of any of them proves the guard did not run first (issue #402).
var envrcGuardTools = []string{"dscl", "dseditgroup", "ssh"}

// envrcNextStep is what one of the eight targets reaches once the guard lets it through: the first tool it runs, or
// the message of the credential check it prints when the sandbox sets no agents App variable, which is as far as
// a target the sandbox cannot take further gets. Reaching it is what "the guard passed" means here.
type envrcNextStep struct {
	tool    string
	message string
}

// envrcNextSteps is the next step of each of the eight targets. The four token targets print the same message
// once the guard lets them through: the sandbox sets no agents App variable, and dev/agents-app-token.sh's
// check_inputs dies on the missing SHIFT2EU_AGENTS_APP_ID before it mints anything or runs a tool.
var envrcNextSteps = map[string]envrcNextStep{
	"agents-token":                 {message: "SHIFT2EU_AGENTS_APP_ID"},
	"agents-token-refresh":         {message: "SHIFT2EU_AGENTS_APP_ID"},
	"agents-token-refresh-install": {message: "SHIFT2EU_AGENTS_APP_ID"},
	"agents-token-refresh-start":   {message: "SHIFT2EU_AGENTS_APP_ID"},
	"vm-create":                    {tool: "limactl"},
	"vm-env":                       {tool: "limactl"},
	"vm-init":                      {tool: "limactl"},
	"vm-up":                        {tool: "limactl"},
}

// envrcLayoutCase is one layout the guard is run in, for one or more of the eight targets: the .envrc files a
// maintainer's machine would have, and the files a refusal must name.
type envrcLayoutCase struct {
	name string
	// hostEnvrc is what the host clone's .envrc holds beside the planted lines, and empty leaves the clone
	// without one.
	hostEnvrc string
	// setup writes the files of the layout hostEnvrc cannot spell, such as a file outside the clone whose path
	// only the sandbox knows, and returns the paths a refusal must name.
	setup func(t *testing.T, l *hostLayout) []string
	// targets narrows the case to some of the eight targets, and empty runs it against all of them.
	targets []string
	// want is what a refusal's stderr must say beyond the paths, such as the docs section that holds the fix.
	want []string
	// notWant is what a refusal's stderr must not say: the names of links it walked or of a path it resolved,
	// which a guard that fails closed must not echo (issue #468).
	notWant []string
}

// runs is the targets the case runs against.
func (tc envrcLayoutCase) runs() []string {
	if len(tc.targets) > 0 {
		return tc.targets
	}

	return envrcGuardTargets
}

// TestHostTargetsRefuseAHostCloneEnvrcThatRunsAnotherFile pins the first half of issue #402 in its strict form: the
// host clone's .envrc may hold the maintainer's credentials, but it may not hand them to another file, and the
// guard does not parse shell, so it refuses every spelling it cannot read exactly. `source_env`,
// `source_env_if_exists`, `source` and `.` run a file as shell, which the guard does not scan, so they are refused
// whatever they name, a file inside the clone included. `source_up` and `source_up_if_exists` find a .envrc in a
// directory above the clone, which no review of this repository reaches, and are refused too. `dotenv` and
// `dotenv_if_exists` load a file direnv parses as KEY=VALUE rather than runs, so a path inside the clone passes
// only while the argument - one enclosing pair of matching quotes stripped - holds nothing but letters, digits and
// . _ / + @ = - and resolves inside the clone: a quote, a backslash, a brace, a glob, $, a backtick, a tilde or a
// space counts as outside. The line is read as bash reads it: ;, &, |, ( and ) separate directives, so
// `dotenv;source_env ../x` holds two, and <, > and a backtick end a word as well, so a directive glued to a
// redirection (`source_up>/dev/null`) or sitting inside backticks is a directive of its own. `source_url <url>
// <hash>` downloads a script and sources it, another file the guard never reads, so it is refused whatever it
// names. Every one of the eight targets must refuse such a clone, and the refusal must name the clone's
// .envrc, the line the maintainer has to change, and docs/dev-environment.md, The host clone, where every line
// belongs.
func TestHostTargetsRefuseAHostCloneEnvrcThatRunsAnotherFile(t *testing.T) {
	t.Parallel()

	cases := []envrcLayoutCase{
		{
			name:      "the host clone's .envrc says source_up",
			hostEnvrc: "source_up\n",
			want:      []string{"The host clone"},
		},
		{
			name:      "the host clone's .envrc says source_up_if_exists",
			hostEnvrc: "source_up_if_exists\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			name:      "the host clone's .envrc says source_up_if_exists with a file name",
			hostEnvrc: "source_up_if_exists .envrc.private\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			name:      "the host clone's .envrc says source_env ..",
			hostEnvrc: "source_env ..\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			name:      "the host clone's .envrc says source_env ../secrets.env",
			hostEnvrc: "source_env ../secrets.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			name:      "the host clone's .envrc says dotenv ../x.env",
			hostEnvrc: "dotenv ../x.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			name:      "the host clone's .envrc says dotenv_if_exists ../secrets.env",
			hostEnvrc: "dotenv_if_exists ../secrets.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			name:      "the host clone's .envrc says source_env_if_exists ../secrets.env",
			hostEnvrc: "source_env_if_exists ../secrets.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			// A source_env inside the clone runs a file the guard never reads, as one outside it does: either
			// file may read a parent in lines the guard cannot see, so the directive is refused whatever it names
			// (issue #402).
			name:      "the host clone's .envrc sources a file inside the clone with source_env",
			hostEnvrc: "source_env ./host-secrets.env\n",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeFileMode(t, filepath.Join(l.root, "host-secrets.env"), []byte(plantedEnvrcLines()), 0o600)

				return nil
			},
			want: []string{"line 1", "The host clone"},
		},
		{
			// source is bash's name for source_env, so a file inside the clone is refused with it too.
			name:      "the host clone's .envrc says source with a path inside the clone",
			hostEnvrc: "source ./host-secrets.env\n",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeFileMode(t, filepath.Join(l.root, "host-secrets.env"), []byte(plantedEnvrcLines()), 0o600)

				return nil
			},
			want: []string{"line 1", "The host clone"},
		},
		{
			// . is bash's source as well: it runs the file it names, and a file of the clone is no file the
			// guard has read.
			name:      "the host clone's .envrc says . with a path inside the clone",
			hostEnvrc: ". ./.env.local\n",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeFileMode(t, filepath.Join(l.root, ".env.local"), []byte(plantedEnvrcLines()), 0o600)

				return nil
			},
			want: []string{"line 1", "The host clone"},
		},
		{
			// The clone's own absolute path is the same file as the relative one, and source_env runs it either
			// way: the spelling of the path changes nothing about the refusal.
			name: "the host clone's .envrc sources an absolute path inside the clone",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				inside := filepath.Join(l.root, "host-secrets.env")
				writeFileMode(t, inside, []byte(plantedEnvrcLines()), 0o600)
				writeHostEnvrc(t, l, "source_env "+inside+"\n")

				return []string{filepath.Join(l.root, ".envrc")}
			},
			want: []string{"line 1", "The host clone"},
		},
		{
			// The one-hop case: the guard scans the clone's .envrc alone, so whatever a file it sources holds -
			// here the source_up that reads a .envrc above the clone - reaches direnv unread. The directive is
			// refused before that hop can happen (issue #402).
			name: "the host clone's .envrc sources a file inside the clone that says source_up",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeFileMode(t, filepath.Join(l.root, ".envrc.local"),
					[]byte("source_up\n"+plantedEnvrcLines()), 0o600)
				writeHostEnvrc(t, l, "source_env ./.envrc.local\n")

				return []string{filepath.Join(l.root, ".envrc")}
			},
			want: []string{"line 1", "The host clone"},
		},
		{
			// ; separates two commands in bash, and direnv runs the line as bash does: the source_env after it
			// is a directive of its own, and the line is refused for it.
			name:      "the host clone's .envrc says dotenv;source_env ../outside.env",
			hostEnvrc: "dotenv;source_env ../outside.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// && separates two commands as ; does: the guard must read the source behind it.
			name:      "the host clone's .envrc says dotenv&&source ../outside.env",
			hostEnvrc: "dotenv&&source ../outside.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// Two . directives on one line: the second names a file above the clone, and the guard refuses the
			// line for it.
			name:      "the host clone's .envrc says .;. ../outside.env",
			hostEnvrc: ".;. ../outside.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// bash ends a word at < and > as it does at a space: the redirection after the directive is no part
			// of it, and the directive is refused for what it runs, as it is with a space (issue #402).
			name:      "the host clone's .envrc says source_up>/dev/null",
			hostEnvrc: "source_up>/dev/null\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// The redirection the directive is glued to hides nothing: source_env runs ../outside.env, and the
			// guard refuses the directive whatever it names (issue #402).
			name:      "the host clone's .envrc says source_env>/dev/null ../outside.env",
			hostEnvrc: "source_env>/dev/null ../outside.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// . is bash's source, and a redirection glued to it ends the word as a space does (issue #402).
			name:      "the host clone's .envrc says .>/dev/null ../outside.env",
			hostEnvrc: ".>/dev/null ../outside.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// < ends a word too, and reading from /dev/null says nothing about the file source runs (issue #402).
			name:      "the host clone's .envrc says source</dev/null ../outside.env",
			hostEnvrc: "source</dev/null ../outside.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// bash hands dotenv ../outside.env: the redirection is not an argument, so the guard must read the
			// word after it and refuse the path (issue #402).
			name:      "the host clone's .envrc says dotenv>/dev/null ../outside.env",
			hostEnvrc: "dotenv>/dev/null ../outside.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// The same for dotenv_if_exists, which loads the file it names when it exists (issue #402).
			name:      "the host clone's .envrc says dotenv_if_exists>/dev/null ../outside.env",
			hostEnvrc: "dotenv_if_exists>/dev/null ../outside.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// A backtick runs the command it encloses and splices its output into the line, so the directive
			// inside the pair runs as surely as one of its own and must be read as one (issue #402).
			name:      "the host clone's .envrc says x=`source_up`",
			hostEnvrc: "x=`source_up`\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// The pair on its own: bash ends the word at the opening backtick, so source_up is a command of its
			// own wherever the word it opens sits (issue #402).
			name:      "the host clone's .envrc says `source_up` alone",
			hostEnvrc: "`source_up`\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// The argument holds a space, so bash hands dotenv the first word alone, which names no file at all:
			// the guard cannot tell what dotenv would load and refuses the line as outside the clone.
			name: "the host clone's .envrc dotenvs a quoted path holding a space",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				if err := os.MkdirAll(filepath.Join(l.root, "a b"), 0o700); err != nil {
					t.Fatalf("create %s: %v", filepath.Join(l.root, "a b"), err)
				}

				writeHostEnvrc(t, l, "dotenv \"./a b/../../outside.env\"\n")

				return []string{filepath.Join(l.root, ".envrc")}
			},
			targets: []string{"vm-env"},
			want:    []string{"line 1", "The host clone"},
		},
		{
			// A backslash escapes the first dot, so the path bash hands dotenv is not the one a guard reading
			// the characters of the line would resolve: it counts as outside.
			name:      "the host clone's .envrc dotenvs a path with an escaped dot",
			hostEnvrc: "dotenv \\../outside.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// Quotes around the word are bash's other spelling of the same command: it removes them and runs
			// dotenv ../outside.env. The guard parses no shell, so a directive directly followed by a quote
			// counts as outside (issue #402).
			name:      "the host clone's .envrc quotes the word dotenv",
			hostEnvrc: "\"dotenv\" ../outside.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// bash joins the quoted word and the rest of the path into ../outside.env: one quote inside the
			// argument is not a pair the guard strips, so the line counts as outside.
			name:      "the host clone's .envrc dotenvs a single-quoted .. followed by a path",
			hostEnvrc: "dotenv '..'/outside.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// The same with double quotes: the pair does not enclose the whole argument.
			name:      "the host clone's .envrc dotenvs a double-quoted .. followed by a path",
			hostEnvrc: "dotenv \"..\"/outside.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// A brace expands to two words in bash, one of which walks up out of the clone: a character the
			// guard cannot read is outside.
			name:      "the host clone's .envrc dotenvs a brace expansion",
			hostEnvrc: "dotenv {..,a}/outside.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// The glob matches .. and loads the file above the clone: another character the guard cannot read.
			name:      "the host clone's .envrc dotenvs a glob",
			hostEnvrc: "dotenv .[.]/outside.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// A path holding $ is expanded by the shell direnv runs the .envrc with, so the guard cannot
			// resolve it and must treat it as outside the clone.
			name:      "the host clone's .envrc says source_env with a $HOME path",
			hostEnvrc: "source_env \"$HOME/secrets.env\"\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			// direnv runs the .envrc in bash, where source and . are the same directive as source_env: a line
			// that reads a parent with them is as far outside the clone as source_env .. (issue #402).
			name:      "the host clone's .envrc says source ../secrets.env",
			hostEnvrc: "source ../secrets.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			name:      "the host clone's .envrc says . ../secrets.env",
			hostEnvrc: ". ../secrets.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			// direnv's source_url downloads the script the URL names and sources it, so it runs a file the guard
			// has not read as source_env does, and is refused whatever it names - here an invented URL and hash
			// (issue #402).
			name:      "the host clone's .envrc says source_url with an invented URL and hash",
			hostEnvrc: "source_url https://example.com/x.sh sha256-AAAA\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 1", "The host clone"},
		},
		{
			// Not the first word of the line: a source behind a ; is a command of its own to bash, and direnv
			// runs the whole line.
			name:      "the host clone's .envrc says export A=1; source ../secrets.env",
			hostEnvrc: "export A=1; source ../secrets.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			// bash searches PATH for a word with no slash before the current directory, so `source creds.env`
			// reads whichever creds.env PATH finds first, not the clone's: the guard cannot know which file
			// that is, and must refuse it. The fix is writing ./creds.env (issue #402).
			name: "the host clone's .envrc says source with a name that holds no slash",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeFileMode(t, filepath.Join(l.root, "creds.env"), []byte(plantedEnvrcLines()), 0o600)
				writeHostEnvrc(t, l, "source creds.env\n")

				return []string{filepath.Join(l.root, ".envrc")}
			},
			targets: []string{"vm-env"},
			want:    []string{"line 1", "The host clone"},
		},
		{
			// . is bash's source: the same PATH search, and the same refusal for a word with no slash.
			name: "the host clone's .envrc says . with a name that holds no slash",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeFileMode(t, filepath.Join(l.root, "creds.env"), []byte(plantedEnvrcLines()), 0o600)
				writeHostEnvrc(t, l, ". creds.env\n")

				return []string{filepath.Join(l.root, ".envrc")}
			},
			targets: []string{"vm-env"},
			want:    []string{"line 1", "The host clone"},
		},
		{
			// A backslash continues the line, so bash hands source_env the ../secrets.env of the second line:
			// the directive's argument is the two lines together, not the lone backslash of the first.
			name:      "the host clone's .envrc continues source_env with a backslash",
			hostEnvrc: "source_env \\\n  ../secrets.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			// dotenv reads the file it names, and a symlink loads like the file it points at: the guard must
			// resolve a path inside the clone before it calls it inside.
			name: "the host clone's .envrc dotenvs a symlink that points outside the clone",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				target := filepath.Join(l.sb.root, "outside", "secrets.env")
				writeFileMode(t, target, []byte(plantedEnvrcLines()), 0o600)

				link := filepath.Join(l.root, "filelink")
				if err := os.Symlink(target, link); err != nil {
					t.Fatalf("symlink %s: %v", link, err)
				}

				writeHostEnvrc(t, l, "dotenv filelink\n")

				return []string{filepath.Join(l.root, ".envrc")}
			},
			targets: []string{"vm-env"},
			want:    []string{"The host clone"},
		},
		{
			// A bare dotenv reads the clone's own .env, but a .env that is a symlink hands the shell the file
			// it points at: a clone whose .env points outside is as far outside as any other, so the guard
			// resolves the clone's .env and refuses it (issue #402).
			name: "the host clone's .envrc says dotenv alone and the clone's .env is a symlink outside it",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				target := filepath.Join(l.sb.root, "outside", ".env")
				writeFileMode(t, target, []byte(plantedEnvrcLines()), 0o600)

				link := filepath.Join(l.root, ".env")
				if err := os.Symlink(target, link); err != nil {
					t.Fatalf("symlink %s: %v", link, err)
				}

				writeHostEnvrc(t, l, "dotenv\n")

				return []string{filepath.Join(l.root, ".envrc")}
			},
			targets: []string{"vm-env"},
			want:    []string{"line 1", "The host clone"},
		},
		{
			// The clone's own .envrc is what the guard reads, and a symlink there makes the credentials live
			// in a file outside the clone: the refusal names the clone's .envrc, the file to replace with its
			// target's lines, and reads none of them.
			name: "the host clone's .envrc is a symlink to a file outside the clone",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				target := filepath.Join(l.sb.root, "outside", "envrc.outside")
				writeFileMode(t, target, []byte("export X=1\n"+plantedEnvrcLines()), 0o600)

				link := filepath.Join(l.root, ".envrc")
				if err := os.Symlink(target, link); err != nil {
					t.Fatalf("symlink %s: %v", link, err)
				}

				return []string{link}
			},
			targets: []string{"vm-env"},
			want:    []string{"The host clone"},
		},
		{
			// A credential on the directive's own line: the guard reads that line, and its message must name
			// the directive and paths only, never the value beside them (CLAUDE.md rule 3).
			name:      "the host clone's .envrc holds a credential after a directive on the same line",
			hostEnvrc: "dotenv ../x.env; export CLAUDE_CODE_OAUTH_TOKEN=" + plantedOAuthValue + "\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			// The directive sits inside a double-quoted value: bash only passes it to direnv as an argument of
			// export, so loading it is arguable - refusing the line is not, and either way no value may reach
			// the terminal.
			name:      "the host clone's .envrc holds a directive inside a quoted value",
			hostEnvrc: "export DEEPSEEK_API_KEY=\"" + plantedDeepseekValue + " dotenv /planted\"\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			// A sibling whose name begins with the clone's is no directory of the clone: a check that compares
			// the paths as strings must compare whole components.
			name: "the host clone's .envrc sources a sibling whose name begins with the clone's",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				sibling := filepath.Join(filepath.Dir(l.root), filepath.Base(l.root)+"-old")
				writeFileMode(t, filepath.Join(sibling, "x.envrc"), []byte(plantedEnvrcLines()), 0o600)
				writeHostEnvrc(t, l, "source_env ../"+filepath.Base(sibling)+"/x.envrc\n")

				return []string{filepath.Join(l.root, ".envrc")}
			},
			targets: []string{"vm-env"},
			want:    []string{"The host clone"},
		},
		{
			// A refusal names the line the maintainer has to change, so the two directives on lines 2 and 4
			// are named by their numbers as well as by the file.
			name:      "the host clone's .envrc reads a parent on two lines",
			hostEnvrc: "# the maintainer's own settings\ndotenv ../a.env\n\ndotenv ../a.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"line 2", "line 4"},
		},
		{
			// vm-env-check is the check the three that copy the credentials run first, and the guard gives it
			// the same refusal, so running it alone tells the maintainer of the layout too.
			name:      "the host clone's .envrc says source_up, run through vm-env-check alone",
			hostEnvrc: "source_up\n",
			targets:   []string{"vm-env-check"},
			want:      []string{"The host clone"},
		},
		{
			// An absolute path outside the clone is as far outside it as `..` is: a file of the workspace's
			// parent, which only the sandbox knows the path of.
			name: "the host clone's .envrc names an absolute path outside the clone",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				outside := filepath.Join(l.sb.root, "outside", "secrets.env")
				writeFileMode(t, outside, []byte(plantedEnvrcLines()), 0o600)
				writeHostEnvrc(t, l, "source_env "+outside+"\n")

				return []string{filepath.Join(l.root, ".envrc")}
			},
			targets: []string{"vm-env"},
			want:    []string{"The host clone"},
		},
		{
			// Issue #468, the macOS path PR #463 pinned for the host guard: a link's target may end in a
			// newline, which command substitution strips, so a guard that follows the target it read walks to
			// the plain directory of that name while the kernel walks the name that ends in the newline. Here
			// the kernel's way leads through "envrc\n", a link to a credentials file outside the clone, and
			// the resolver's way to "envrc", a harmless file inside it: the guard must refuse the clone's
			// .envrc, which direnv loads from outside the clone, and not read the file inside.
			name: "the host clone's .envrc is a symlink whose target ends in a newline",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				outside := filepath.Join(l.sb.root, "outside", "credentials.envrc")
				writeFileMode(t, outside, []byte("source_up\n"+plantedEnvrcLines()), 0o600)

				// The name the kernel follows: it ends in the newline left out of everything a guard echoes.
				writeLink(t, filepath.Join(l.root, "envrc\n"), outside)
				// What the resolver reads instead: a file of the clone that reads nothing.
				writeFileMode(t, filepath.Join(l.root, "envrc"), []byte(plantedEnvrcLines()), 0o600)

				return []string{writeLink(t, filepath.Join(l.root, ".envrc"), "envrc\n")}
			},
			targets: []string{"vm-env"},
			want:    []string{"The host clone"},
		},
		{
			// The same layout with a readlink that itself drops the trailing newline of a target, as the macOS
			// make 3.81 run of issue #447 was suspected to have. A resolver that keeps a target's newline (issue
			// #463 reads it with a _ after it) still trusts what readlink prints unless it checks the target
			// against the kernel: this readlink reproduces the fail-open on Linux, where the guard runs the host
			// target instead of refusing, and after the fix it must fail closed.
			name: "the host clone's .envrc is a symlink whose target ends in a newline, and readlink drops it",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				useFakeReadlinkThatDropsATrailingNewline(t, l)

				outside := filepath.Join(l.sb.root, "outside", "credentials.envrc")
				writeFileMode(t, outside, []byte("source_up\n"+plantedEnvrcLines()), 0o600)

				writeLink(t, filepath.Join(l.root, "envrc\n"), outside)
				writeFileMode(t, filepath.Join(l.root, "envrc"), []byte(plantedEnvrcLines()), 0o600)

				return []string{writeLink(t, filepath.Join(l.root, ".envrc"), "envrc\n")}
			},
			targets: []string{"vm-env"},
			want:    []string{"The host clone"},
		},
		{
			// Issue #468: the same newline on the path a dotenv directive's argument is resolved along. direnv
			// reads the file the kernel names - the credentials file outside the clone - while the resolver
			// reads the harmless file named without the newline inside it.
			name: "the host clone's .envrc dotenvs a symlink whose target ends in a newline",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				outside := filepath.Join(l.sb.root, "outside", "creds.env")
				writeFileMode(t, outside, []byte(plantedEnvrcLines()), 0o600)

				writeLink(t, filepath.Join(l.root, "creds\n"), outside)
				writeFileMode(t, filepath.Join(l.root, "creds"), []byte(plantedEnvrcLines()), 0o600)
				writeLink(t, filepath.Join(l.root, "credslink"), "creds\n")
				writeHostEnvrc(t, l, "dotenv credslink\n")

				return []string{filepath.Join(l.root, ".envrc")}
			},
			targets: []string{"vm-env"},
			want:    []string{"The host clone"},
		},
		{
			// Issue #468: the clone's .envrc is a dangling symlink whose absolute target lies outside the
			// clone. The file direnv would go to is no file of the clone's, so the guard must refuse rather
			// than compare the link's name.
			name: "the host clone's .envrc is a dangling symlink whose absolute target is outside the clone",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				gone := filepath.Join(l.sb.root, "outside", "gone.envrc")
				if _, err := os.Stat(gone); !os.IsNotExist(err) {
					t.Fatalf("%s exists, so the link this case plants is no dangling one: %v", gone, err)
				}

				return []string{writeLink(t, filepath.Join(l.root, ".envrc"), gone)}
			},
			targets: []string{"vm-env"},
			want:    []string{"The host clone"},
		},
		{
			// The same with a relative target, which the resolver must read against the link's own directory:
			// here it climbs out of the clone into a directory beside its parent.
			name: "the host clone's .envrc is a dangling symlink whose relative target is outside the clone",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				gone := filepath.Join(filepath.Dir(l.root), "beside-the-clone", "gone.envrc")
				if _, err := os.Stat(gone); !os.IsNotExist(err) {
					t.Fatalf("%s exists, so the link this case plants is no dangling one: %v", gone, err)
				}

				return []string{writeLink(t, filepath.Join(l.root, ".envrc"), "../beside-the-clone/gone.envrc")}
			},
			targets: []string{"vm-env"},
			want:    []string{"The host clone"},
		},
		{
			// Issue #468, fail closed: the clone's .envrc runs into a loop of symlinks, a -> b and b -> a,
			// which no resolution ends. The guard must refuse instead of comparing a path it never resolved,
			// and it must not echo the names it walked there.
			name: "the host clone's .envrc is a loop of symlinks",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeLink(t, filepath.Join(l.root, "loop-a"), "loop-b")
				writeLink(t, filepath.Join(l.root, "loop-b"), "loop-a")
				writeLink(t, filepath.Join(l.root, ".envrc"), "loop-a")

				return nil
			},
			targets: []string{"vm-env"},
			want:    []string{"The host clone"},
			notWant: []string{"loop-b"},
		},
		{
			// Issue #468: a chain of more symlinks than the 32 the host guard's resolver follows (macOS's
			// MAXSYMLINKS) ends in a file inside the clone, so the cap is the only reason to refuse it. The two
			// guards share one resolver, so both must fail closed here; a guard with a cap of its own decides
			// the same layout the other way, and it must not echo the hops it walked.
			name: "the host clone's .envrc is a chain of more than 32 symlinks",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeFileMode(t, filepath.Join(l.root, "chain-end"), []byte(plantedEnvrcLines()), 0o600)

				// More than 32 links including the clone's .envrc: each hop names the next, the last the file.
				const links = 33

				for hop := links; hop >= 1; hop-- {
					target := "chain-end"
					if hop < links {
						target = fmt.Sprintf("chain-%d", hop+1)
					}

					writeLink(t, filepath.Join(l.root, fmt.Sprintf("chain-%d", hop)), target)
				}

				writeLink(t, filepath.Join(l.root, ".envrc"), "chain-1")

				return nil
			},
			targets: []string{"vm-env"},
			want:    []string{"The host clone"},
			notWant: []string{"chain-32"},
		},
		{
			// Issue #468: the link's target climbs into a credentials file outside the clone through ..
			// components, which only the resolved path shows. The guard must compare the workspace as it
			// resolves, not as it is spelled.
			name: "the host clone's .envrc is a symlink whose target climbs out of the clone with ..",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				outside := filepath.Join(l.sb.root, "outside", "creds.envrc")
				writeFileMode(t, outside, []byte(plantedEnvrcLines()), 0o600)

				return []string{writeLink(t, filepath.Join(l.root, ".envrc"), "not-there/../../../outside/creds.envrc")}
			},
			targets: []string{"vm-env"},
			want:    []string{"The host clone"},
		},
		{
			// Issue #468: a link to / is a target like any other, so the resolver must follow it and walk the
			// rest of the path from there again - here the absolute path of a credentials file outside the clone.
			name: "the host clone's .envrc is a symlink through a link to the root directory",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				outside := filepath.Join(l.sb.root, "outside", "creds.envrc")
				writeFileMode(t, outside, []byte(plantedEnvrcLines()), 0o600)

				toRoot := writeLink(t, filepath.Join(l.sb.root, "to-root"), "/")

				return []string{writeLink(t, filepath.Join(l.root, ".envrc"), toRoot+strings.TrimPrefix(outside, "/"))}
			},
			targets: []string{"vm-env"},
			want:    []string{"The host clone"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, target := range tc.runs() {
				t.Run(target, func(t *testing.T) {
					t.Parallel()

					run := makeInEnvrcLayout(t, target, tc)
					run.assertRefused(t, target, tc)
				})
			}
		})
	}
}

// TestHostTargetsRefuseAnEnvrcFileInOrAboveTheWorkspace pins the second half of issue #402: no .envrc and no .env
// may sit in the workspace the VM mounts - the parent directory of WORKSPACE_REPO - or in a directory above it,
// where direnv finds it and loads the credentials in every shell the maintainer opens in the workspace. A file
// there is refused by its path alone, and the refusal names it, so the maintainer knows which file to move into
// the host clone's .envrc. The host clone's own .envrc is clean here, which pins that the check reaches beyond
// the clone it runs in.
func TestHostTargetsRefuseAnEnvrcFileInOrAboveTheWorkspace(t *testing.T) {
	t.Parallel()

	cases := []envrcLayoutCase{
		{
			name: "the workspace holds a .envrc",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeHostEnvrc(t, l, "")

				return []string{writeAboveWorkspace(t, l, 1, ".envrc")}
			},
		},
		{
			name: "the workspace holds a .env",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeHostEnvrc(t, l, "")

				return []string{writeAboveWorkspace(t, l, 1, ".env")}
			},
		},
		{
			name: "a directory above the workspace holds a .envrc",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeHostEnvrc(t, l, "")

				return []string{writeAboveWorkspace(t, l, 2, ".envrc")}
			},
		},
		{
			name: "a directory above the workspace holds a .env",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeHostEnvrc(t, l, "")

				return []string{writeAboveWorkspace(t, l, 2, ".env")}
			},
		},
		{
			// A symlink loads like the file it points at, so the guard must refuse it by its own path, which
			// is the one in the workspace the maintainer has to remove.
			name: "the workspace holds a .envrc symlink",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeHostEnvrc(t, l, "")

				target := filepath.Join(l.sb.root, "outside", "secrets.env")
				writeFileMode(t, target, []byte(plantedEnvrcLines()), 0o600)

				link := filepath.Join(l.workspace(), ".envrc")
				if err := os.Symlink(target, link); err != nil {
					t.Fatalf("symlink %s: %v", link, err)
				}

				return []string{link}
			},
		},
		{
			// WORKSPACE_REPO is a path, and direnv reads the .envrcs of the path as written, while the VM mounts
			// the directory it leads to: the guard must walk both, or a file above one spelling only is missed.
			// Here the symlink keeps the .envrc of the directory holding it in the chain of the spelling alone.
			name: "a .envrc above the workspace only in the spelling WORKSPACE_REPO writes",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeHostEnvrc(t, l, "")

				linkparent := filepath.Join(l.sb.root, "linkparent")
				if err := os.MkdirAll(linkparent, 0o700); err != nil {
					t.Fatalf("create %s: %v", linkparent, err)
				}

				link := filepath.Join(linkparent, "wslink")
				if err := os.Symlink(l.workspace(), link); err != nil {
					t.Fatalf("symlink %s: %v", link, err)
				}

				l.env["WORKSPACE_REPO"] = filepath.Join(link, filepath.Base(l.workspaceRepo()))

				above := filepath.Join(linkparent, ".envrc")
				writeFileMode(t, above, []byte(plantedEnvrcLines()), 0o600)

				return []string{above}
			},
			targets: []string{"vm-env-check"},
		},
		{
			// The other spelling: the symlink leads below the workspace of the path as written, so a .envrc in
			// the directory above where it leads matches no directory of WORKSPACE_REPO's own chain.
			name: "a .envrc above the workspace the spelling WORKSPACE_REPO leads to",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeHostEnvrc(t, l, "")

				// The physical workspace, the directory the symlink leads to, and its parent - which is no
				// directory of the spelling's own chain (<sandbox>/link, <sandbox>, ...).
				physical := filepath.Join(l.sb.workspaceRoot(), "sub")
				writeClone(t, filepath.Join(physical, filepath.Base(l.workspaceRepo())))

				link := filepath.Join(l.sb.root, "link")
				if err := os.Symlink(physical, link); err != nil {
					t.Fatalf("symlink %s: %v", link, err)
				}

				l.env["WORKSPACE_REPO"] = filepath.Join(link, filepath.Base(l.workspaceRepo()))

				above := filepath.Join(l.sb.workspaceRoot(), ".envrc")
				writeFileMode(t, above, []byte(plantedEnvrcLines()), 0o600)

				return []string{above}
			},
			targets: []string{"vm-env-check"},
		},
		{
			// The same refusal for the check the three credential targets run first, run on its own.
			name: "the check refuses the workspace's .envrc when run through vm-env-check alone",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeHostEnvrc(t, l, "")

				return []string{writeAboveWorkspace(t, l, 1, ".envrc")}
			},
			targets: []string{"vm-env-check"},
		},
		{
			// Issue #468, the macOS path PR #463 pinned for the host guard: the workspace is reached through a
			// link whose target ends in a newline, and the credentials .envrc sits in the directory that target
			// names - the directory direnv reads the .envrcs above the workspace from. The layout must be refused
			// on every host the guard runs on, whichever of the two guards resolves the workspace first: here the
			// host guard's resolver refuses the newline in the resolved workspace (no character a WORKSPACE_REPO
			// may hold), so the .envrc guard never walks it, and a guard that let it through would leave direnv
			// reading that .envrc in every shell opened in the workspace.
			name: "a .envrc in the directory a link in the workspace's path ends in a newline for",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeHostEnvrc(t, l, "")

				physical := filepath.Join(l.sb.root, "alias\n")
				writeClone(t, filepath.Join(physical, filepath.Base(l.workspaceRepo())))
				writeLink(t, filepath.Join(l.sb.root, "tn"), "alias\n")

				l.env["WORKSPACE_REPO"] = filepath.Join(l.sb.root, "tn", filepath.Base(l.workspaceRepo()))

				writeAboveWorkspaceIn(t, physical, 1, ".envrc")

				return nil
			},
			targets: []string{"vm-env-check"},
		},
		{
			// Issue #468: the workspace is spelled in another case on a file system that ignores case (macOS's
			// APFS by default), which is the case PR #463 pinned for the host guard, and the .envrc direnv loads
			// sits in the workspace itself. The kernel names that .envrc to every shell opened in the workspace,
			// so the guard must refuse. The file system is asked whether it is case-insensitive rather than
			// GOOS: on a case-sensitive one (Linux) the spelling names another path, so the row is skipped
			// there and runs only on CI's macOS make 3.81 job, whose /bin/sh is bash 3.2.
			name: "the workspace is spelled in another case, on a file system that ignores case",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeHostEnvrc(t, l, "")

				if !caseInsensitiveDir(t, l.sb.root) {
					t.Skipf("the file system holding %s is case-sensitive, so a spelling in another case names another path, one without a .envrc: the row pins what a case-insensitive file system, macOS's APFS by default, does with the workspace (issue #468)", l.sb.root)
				}

				// The .envrc direnv loads in the workspace, written where the kernel's path leads, spelled in the
				// case the file system keeps.
				workspace := l.workspace()
				writeFileMode(t, filepath.Join(workspace, ".envrc"), []byte(plantedEnvrcLines()), 0o600)

				// WORKSPACE_REPO spelled in the other case: the kernel names the same checkout, while a guard
				// comparing spellings reads a path the clone's REPO_ROOT never equals.
				l.env["WORKSPACE_REPO"] = caseSwapped(l.workspaceRepo())

				return nil
			},
			targets: []string{"vm-env-check"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, target := range tc.runs() {
				t.Run(target, func(t *testing.T) {
					t.Parallel()

					run := makeInEnvrcLayout(t, target, tc)
					run.assertRefused(t, target, tc)
				})
			}
		})
	}
}

// TestHostTargetsPassACleanEnvrcLayout pins what the guard must not stop: the maintainer's layout when the host
// clone's .envrc runs no other file - it holds its lines itself - or loads one the way direnv parses rather than
// runs it: `dotenv` and `dotenv_if_exists` with a plain path inside the clone, written as a name with no slash, a
// relative path, an absolute path or one enclosing pair of matching quotes, and a bare `dotenv`, which reads the
// clone's own .env. No .envrc and no .env may sit in the workspace or above it either. Each of the eight targets
// must reach its next step, so the guard does not refuse the layout the docs ask for.
func TestHostTargetsPassACleanEnvrcLayout(t *testing.T) {
	t.Parallel()

	cases := []envrcLayoutCase{
		{
			name: "the host clone has no .envrc",
		},
		{
			// dotenv loads a file direnv parses as KEY=VALUE and never runs, so a relative path inside the
			// clone is the layout the docs ask for, while source_env with the same path is refused for running
			// it (issue #402).
			name:      "the host clone's .envrc dotenvs a path inside the clone",
			hostEnvrc: "dotenv ./.env.local\n",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeFileMode(t, filepath.Join(l.root, ".env.local"), []byte(plantedEnvrcLines()), 0o600)

				return nil
			},
		},
		{
			// One enclosing pair of matching quotes is how bash writes the same path, and dotenv receives the
			// path without them: nothing the guard refuses is left (issue #402).
			name:      "the host clone's .envrc dotenvs a double-quoted path inside the clone",
			hostEnvrc: "dotenv \"./.env.local\"\n",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeFileMode(t, filepath.Join(l.root, ".env.local"), []byte(plantedEnvrcLines()), 0o600)

				return nil
			},
		},
		{
			// The same pair with single quotes.
			name:      "the host clone's .envrc dotenvs a single-quoted path inside the clone",
			hostEnvrc: "dotenv './.env.local'\n",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeFileMode(t, filepath.Join(l.root, ".env.local"), []byte(plantedEnvrcLines()), 0o600)

				return nil
			},
		},
		{
			// A bare dotenv names no file, which is dotenv's own default: it reads the clone's .env, and that
			// file is the clone's own (issue #402).
			name:      "the host clone's .envrc says dotenv alone",
			hostEnvrc: "dotenv\n",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeFileMode(t, filepath.Join(l.root, ".env"), []byte(plantedEnvrcLines()), 0o600)

				return nil
			},
		},
		{
			// direnv resolves a name with no slash against the .envrc's own directory, not against PATH, so
			// `dotenv .env.local` reads the clone's file: the PATH search that makes `source creds.env`
			// unreadable is bash's alone, and this layout is the one the docs ask for (issue #402).
			name:      "the host clone's .envrc dotenvs a name that holds no slash",
			hostEnvrc: "dotenv .env.local\n",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeFileMode(t, filepath.Join(l.root, ".env.local"), []byte(plantedEnvrcLines()), 0o600)

				return nil
			},
		},
		{
			// A commented-out directive loads nothing, so it must not refuse the layout the docs ask for.
			name:      "the host clone's .envrc only mentions source_up in comments",
			hostEnvrc: "# source_up\n#   dotenv ../secrets.env\n",
		},
		{
			// The clone's own absolute path is the same file as the relative one: dotenv loads it, and the
			// guard passes it.
			name: "the host clone's .envrc dotenvs an absolute path inside the clone",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				inside := filepath.Join(l.root, ".env.local")
				writeFileMode(t, inside, []byte(plantedEnvrcLines()), 0o600)
				writeHostEnvrc(t, l, "dotenv "+inside+"\n")

				return nil
			},
		},
		{
			// Issue #468's positive control, the mirror of the host guard's row of issue #447: the workspace is
			// reached through a symlink the maintainer's own directories hold, and no .envrc and no .env sits in
			// it or above it. Resolving the path must land on the workspace the link leads to, which holds
			// nothing direnv would load, so the guard must let the credential targets through: a resolver that
			// refused every workspace behind a link would stop a layout docs/dev-environment.md asks for.
			name: "the workspace is reached through a symlink outside the host clone",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				physical := filepath.Join(l.sb.root, "physical-ws")
				writeClone(t, filepath.Join(physical, filepath.Base(l.workspaceRepo())))
				writeLink(t, filepath.Join(l.sb.root, "ws-link"), physical)

				l.env["WORKSPACE_REPO"] = filepath.Join(l.sb.root, "ws-link", filepath.Base(l.workspaceRepo()))

				return nil
			},
			targets: []string{"vm-env", "vm-up"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, target := range tc.runs() {
				t.Run(target, func(t *testing.T) {
					t.Parallel()

					run := makeInEnvrcLayout(t, target, tc)
					run.assertPassed(t, target)
				})
			}
		})
	}
}

// envrcGuardRefusalTexts are the guard's two refusals, the phrases that say direnv reads a credential file
// outside the host clone: a target the guard does not cover must print neither of them, and no recipe line of the
// Makefile holds either (the guard is make code that runs while make reads the file, not a recipe).
var envrcGuardRefusalTexts = []string{
	"is in the workspace the VM mounts or in a directory above it",
	"direnv would read the credentials from a .envrc or .env other than the host clone's own",
}

// TestHostTargetsOutsideTheGuardIgnoreAnEnvrcAboveTheWorkspace pins the other side of issue #402: the guard is the
// credential targets' alone. A host target that loads no credential - vm-boot-install, whose LaunchDaemon starts
// the VM and holds none - must still run with a .envrc in the workspace: refusing it too would stop the maintainer
// doing the one thing the docs ask for while the file is still there, and a guard that refuses every host target
// could only be switched off by deleting the guard. make -n prints the recipe and runs nothing, which is all this
// case needs: whether the guard refused shows on the streams, not in what ran.
func TestHostTargetsOutsideTheGuardIgnoreAnEnvrcAboveTheWorkspace(t *testing.T) {
	t.Parallel()

	const target = "vm-boot-install"

	l := newHostLayout(t)
	l.workspaceCheckout(t)
	writeHostEnvrc(t, l, "")

	above := writeAboveWorkspace(t, l, 1, ".envrc")

	stdout, stderr, err := runMakeIn(t, l.root, []string{"-n", target}, maps.Clone(l.env))
	if err != nil {
		t.Fatalf("make -n %s exited non-zero with %s in the workspace, a file the guard refuses for the targets that load a credential and must leave alone for the ones that load none (issue #402): %v\nstdout: %s\nstderr: %s",
			target, above, err, stdout, stderr)
	}

	if !strings.Contains(stdout, target+":") {
		t.Errorf("make -n %s printed none of %s's recipe lines, so this case pins nothing about the guard letting it through (issue #402)\nstdout: %s\nstderr: %s",
			target, target, stdout, stderr)
	}

	for _, text := range envrcGuardRefusalTexts {
		if strings.Contains(stdout, text) || strings.Contains(stderr, text) {
			t.Errorf("make -n %s printed the guard's refusal %q although %s loads no credential and the guard must not cover it: only the targets that load the VM's credentials or the agents App's key are refused (issue #402)\nstdout: %s\nstderr: %s",
				target, text, target, stdout, stderr)
		}
	}

	if namesAnywhere(stdout+stderr, pathForms(t, above)) {
		t.Errorf("make -n %s names %s, the .envrc above the workspace, although it loads no credential and the guard must not cover it (issue #402)\nstdout: %s\nstderr: %s",
			target, above, stdout, stderr)
	}

	run := envrcRun{layout: l, stdout: stdout, stderr: stderr, err: err}
	run.assertNoPlantedValue(t, "make -n "+target)
}

// envrcRun is one run of one host target in a layout the guard is run in: what make printed and returned, the
// sandbox whose fakes recorded what ran, and the files a refusal must name.
type envrcRun struct {
	layout *hostLayout
	// files are the .envrc or .env files the layout holds where the guard must look, and the ones a refusal must
	// name: the host clone's own .envrc and every file the case wrote in the workspace or above it.
	files  []string
	stdout string
	stderr string
	err    error
}

// makeInEnvrcLayout builds a host-clone layout for the case, writes the case's .envrc files with the planted
// credentials beside their lines, and runs target in the host clone. WORKSPACE_REPO names the checkout inside the
// sandbox's workspace, as the host clone's direnv .envrc sets it, and the environment holds invented values for
// the two credentials make vm-env copies, so vm-env and vm-create reach their next step instead of refusing a
// missing credential.
func makeInEnvrcLayout(t *testing.T, target string, tc envrcLayoutCase) envrcRun {
	t.Helper()

	l := newHostLayout(t)

	// The workspace holds the repository's own checkout, which WORKSPACE_REPO names.
	l.workspaceCheckout(t)

	// The host clone holds the script the two foreground token targets run, as the maintainer's does, so their
	// next step is the script's own refusal of the App variables the sandbox does not set.
	writeFileMode(t, filepath.Join(l.root, "dev", "agents-app-token.sh"),
		[]byte(readRepoFile(t, "dev/agents-app-token.sh")), 0o700)

	var files []string

	if tc.hostEnvrc != "" {
		files = append(files, writeHostEnvrc(t, l, tc.hostEnvrc))
	}

	if tc.setup != nil {
		files = append(files, tc.setup(t, l)...)
	}

	env := maps.Clone(l.env)

	for _, name := range vmEnvCredentials {
		env[name] = fakeEnvValue(name)
	}

	stdout, stderr, err := runMakeIn(t, l.root, []string{target}, env)

	return envrcRun{layout: l, files: files, stdout: stdout, stderr: stderr, err: err}
}

// assertRefused fails unless the run refused the layout: a non-zero exit, stderr naming every file the guard must
// point the maintainer at and saying what the case's want asks for beyond the paths, no call of a tool a host
// target reaches - the guard ran before anything else - no path the case's notWant forbids, and no planted
// credential value in either stream.
func (r envrcRun) assertRefused(t *testing.T, target string, tc envrcLayoutCase) {
	t.Helper()

	out := "make " + target

	if r.err == nil {
		t.Errorf("%s in %s exited 0: it must refuse a layout whose .envrc is not only the host clone's own, because the VM's credentials would be loaded outside the clone and handed to every shell opened in the workspace (issue #402)\nstdout: %s\nstderr: %s",
			out, r.layout.root, r.stdout, r.stderr)
	}

	for _, file := range r.files {
		if !namesAnywhere(r.stderr, pathForms(t, file)) {
			t.Errorf("%s's refusal does not name %s on stderr, so a maintainer cannot tell which file to move into the host clone's .envrc (issue #402)\nstdout: %s\nstderr: %s",
				out, file, r.stdout, r.stderr)
		}
	}

	for _, text := range tc.want {
		if !strings.Contains(r.stderr, text) {
			t.Errorf("%s's refusal does not say %q on stderr, so a maintainer does not learn where the credentials belong: docs/dev-environment.md, The host clone (issue #402)\nstdout: %s\nstderr: %s",
				out, text, r.stdout, r.stderr)
		}
	}

	for _, text := range tc.notWant {
		if strings.Contains(r.stderr, text) {
			t.Errorf("%s's refusal says %q on stderr, a path the guard's walk read and could not resolve: a guard that fails closed names the file it could not resolve, never what it walked or resolved, which may be a credential typed where a path goes (CLAUDE.md rule 3, issue #468)\nstdout: %s\nstderr: %s",
				out, text, r.stdout, r.stderr)
		}
	}

	log := r.layout.sb.argvLogText(t)

	for _, tool := range append(slices.Clone(hostTargetTools), envrcGuardTools...) {
		if calls := invocations(log, tool); len(calls) > 0 {
			t.Errorf("%s ran %s %d time(s), starting with %v, although it must refuse the layout first: the guard runs before any limactl, launchctl, sudo or other command of the target, or a run with a .envrc in the wrong place still reaches the machine with the credentials in reach (issue #402)\nstderr: %s",
				out, tool, len(calls), leadingArgs(calls[0], 2), r.stderr)
		}
	}

	r.assertNoPlantedValue(t, out)
}

// assertPassed fails unless the guard let the layout through: the target reaches its next step - the first tool it
// runs, or the message of its own credential check - no message names a .envrc of the layout, and no planted
// credential value appears in either stream.
func (r envrcRun) assertPassed(t *testing.T, target string) {
	t.Helper()

	out := "make " + target

	skipIfAnEnvrcIsAboveTheSandbox(t, r.layout)

	step, ok := envrcNextSteps[target]
	if !ok {
		t.Fatalf("envrcNextSteps holds no next step for make %s, so this test pins nothing about it", target)
	}

	if step.tool != "" {
		if r.err != nil {
			t.Errorf("%s from the host clone exited non-zero although the layout holds nothing the guard refuses, so the guard stopped a layout docs/dev-environment.md asks for (issue #402): %v\nstdout: %s\nstderr: %s",
				out, r.err, r.stdout, r.stderr)
		}

		if calls := invocations(r.layout.sb.argvLogText(t), step.tool); len(calls) == 0 {
			t.Errorf("%s ran no %s: it must get past the guard and reach its first %s, or the guard stopped a layout the docs ask for (issue #402)\nstdout: %s\nstderr: %s",
				out, step.tool, step.tool, r.stdout, r.stderr)
		}
	} else if !strings.Contains(r.stderr, step.message) {
		t.Errorf("%s's stderr does not hold %q, the message of its own credential check, which it prints once the guard lets it through: the guard must not stop a layout docs/dev-environment.md asks for (issue #402)\nstdout: %s\nstderr: %s",
			out, step.message, r.stdout, r.stderr)
	}

	for _, file := range r.files {
		if named := namesAnywhere(r.stderr, pathForms(t, file)); named {
			t.Errorf("%s's stderr names %s although the layout holds nothing the guard refuses: the refusal belongs to a .envrc read from outside the host clone (issue #402)\nstdout: %s\nstderr: %s",
				out, file, r.stdout, r.stderr)
		}
	}

	r.assertNoPlantedValue(t, out)
}

// assertNoPlantedValue fails when either stream holds one of the two made-up credential values the layout's
// .envrc files hold: the guard reads those files, and a message that echoed a line of one would put a credential
// on the maintainer's terminal and into the log (CLAUDE.md rule 3, issue #402).
func (r envrcRun) assertNoPlantedValue(t *testing.T, out string) {
	t.Helper()

	for _, value := range plantedValues {
		for _, stream := range []struct{ name, text string }{{"stdout", r.stdout}, {"stderr", r.stderr}} {
			if strings.Contains(stream.text, value) {
				t.Errorf("%s printed %q on %s, a value out of a .envrc it read: the guard's messages hold names and paths only, never a value (CLAUDE.md rule 3, issue #402)\n%s:\n%s",
					out, value, stream.name, stream.name, stream.text)
			}
		}
	}
}

// writeHostEnvrc writes the host clone's .envrc with content beside the planted credential lines, and returns its
// path: the file the guard reads first, and the one a refusal of a clone that reads a parent must name.
func writeHostEnvrc(t *testing.T, l *hostLayout, content string) string {
	t.Helper()

	path := filepath.Join(l.root, ".envrc")

	writeFileMode(t, path, []byte(content+plantedEnvrcLines()), 0o600)

	return path
}

// writeAboveWorkspace writes a .envrc- or .env-shaped file holding the planted credential lines in the directory
// levels above the workspace that holds WORKSPACE_REPO: level 1 is the workspace itself, the parent directory of
// WORKSPACE_REPO, and level 2 the directory above it. It returns the file's path.
func writeAboveWorkspace(t *testing.T, l *hostLayout, levels int, name string) string {
	t.Helper()

	return writeAboveWorkspaceIn(t, l.workspace(), levels, name)
}

// writeAboveWorkspaceIn writes the same file in the directory levels above dir - level 1 is dir itself - for a case
// whose workspace only the case knows the resolved path of, and returns the file's path.
func writeAboveWorkspaceIn(t *testing.T, dir string, levels int, name string) string {
	t.Helper()

	for level := 1; level < levels; level++ {
		dir = filepath.Dir(dir)
	}

	path := filepath.Join(dir, name)

	writeFileMode(t, path, []byte(plantedEnvrcLines()), 0o600)

	return path
}

// writeLink plants the symbolic link at path whose target is exactly target, a trailing newline included, as a
// maintainer's link may carry one, and returns path.
func writeLink(t *testing.T, path, target string) string {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}

	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("symlink %s to %q: %v", path, target, err)
	}

	return path
}

// fakeReadlinkDroppingATrailingNewline is a readlink that prints a link's target without the newline it ends in,
// which is the readlink the macOS make 3.81 run of issue #447 was suspected to have: command substitution drops a
// target's trailing newline as well, so a guard that keeps one (issue #463 reads it with a _ after it) has to check
// what readlink printed against the kernel, or it walks to another path. It runs the machine's own readlink and
// prints its answer through a command substitution, which drops every trailing newline the answer ends in.
const fakeReadlinkDroppingATrailingNewline = `#!/bin/sh
out=$(%s "$@") || exit 1
printf '%%s\n' "$out"
`

// useFakeReadlinkThatDropsATrailingNewline puts that readlink first on the sandbox's PATH, so a run reads link
// targets the way the macOS run of issue #447 did on Linux.
func useFakeReadlinkThatDropsATrailingNewline(t *testing.T, l *hostLayout) {
	t.Helper()

	writeFileMode(t, filepath.Join(l.sb.binDir, "readlink"),
		[]byte(fmt.Sprintf(fakeReadlinkDroppingATrailingNewline, shQuote(realTool(t, "readlink")))), 0o700)
}

// pathForms returns the spellings a message may use for path: the one the test wrote, and the one with the path's
// symlinks resolved, which is what make's $(CURDIR) and git's REPO_ROOT print on macOS, where /var is
// /private/var. A refusal of a path is right in either spelling.
func pathForms(t *testing.T, path string) []string {
	t.Helper()

	forms := []string{path}

	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != path {
		forms = append(forms, resolved)
	}

	return forms
}

// namesAnywhere reports whether text holds any of the forms.
func namesAnywhere(text string, forms []string) bool {
	for _, form := range forms {
		if strings.Contains(text, form) {
			return true
		}
	}

	return false
}

// leadingArgs returns the first n arguments of a recorded call, so a failure names the call without printing a
// whole recipe's shell text (make vm-env hands limactl all of it).
func leadingArgs(args []string, n int) []string {
	if len(args) <= n {
		return args
	}

	return append(slices.Clone(args[:n]), "...")
}

// skipIfAnEnvrcIsAboveTheSandbox skips a case the guard must pass when the machine the tests run on keeps a .envrc
// or a .env in a directory above the sandbox, up to /: the guard must refuse such a layout, so the case would pin
// the machine's own files instead of the layout the test built, and fail for a reason that is none of the guard's.
// It reads no file, only whether one is there.
func skipIfAnEnvrcIsAboveTheSandbox(t *testing.T, l *hostLayout) {
	t.Helper()

	for dir := filepath.Dir(l.sb.root); ; {
		for _, name := range []string{".envrc", ".env"} {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				t.Skipf("this machine keeps %s above the sandbox, which the guard must refuse, so this case cannot pin the layout it built",
					path)
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}

		dir = parent
	}
}
