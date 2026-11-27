# 0044. Refuse printing the environment in pi with a project extension, and in Claude Code with deny rules

- Status: Accepted
- Date: 2026-11-24
- Amended: 2026-11-26, the tester starts through `dev/pi-tester.sh` with only its own credential, and does
  not start when the refusal's files are not in place (issue #662; see "Amendment: the tester starts with
  only its model credential, and fails closed")
- Amended: 2026-11-27, the tester starts from a root-owned copy of the wrapper and the refusal installed at
  VM provisioning, and does not use the unit folder's copies for its checks or `--extension` (issue #692; see "Amendment: the tester starts
  from a root-owned installed copy")
- Amended: 2026-11-27, the tester's pi trusts no project and loads no discovered extension: only the
  installed refusal, the deck's own extension and the skills, by explicit paths (issue #702; see
  "Amendment: the tester loads no project extension or setting")

## Context

The agents' API keys and tokens are in their environment (CLAUDE.md rule 3). On 2026-11-24 the tester
(pi on DeepSeek, [ADR 0003](0003-pi-with-deepseek-for-the-tester-role.md)) ran `env | grep` while looking
for its scratch directory and printed `DEEPSEEK_API_KEY` in its pane; the key had to be rotated
(issue #540). Only words held the rule:

- pi runs with `--approve` and has no permission prompts; `.pi/settings.json` only loaded the skills;
- `.claude/settings.json`'s `permissions.deny` had no rule for `env`, `printenv` or `set`;
- nothing told the tester where its scratch directory is, so it went looking.

pi 1.5.2 (the version `dev/lima.yaml` pins) has no setting that refuses a command, but it auto-discovers
project-local extensions in `.pi/extensions/`, and an extension's `tool_call` handler sees every tool call
before it runs and can return `{ block: true, reason }`; a handler that throws blocks the call too. pi loads
project extensions only once the project is trusted, and `--approve` trusts it for the process, so the
tester's command line already loads them.

## Decision

Hold rule 3 with a second layer in both agents, beside the words in CLAUDE.md:

- **pi:** `.pi/extensions/deny-env-printing.ts` hooks `tool_call`. It refuses a bash command that matches
  any pattern of `.pi/deny-env-printing.json` (`{"reason": ..., "patterns": [...]}`, read relative to the
  extension file), with the list's reason, and a read, grep, find, ls, edit or write call whose path
  resolves to `/proc` or `~/.config/dot-agent-deck`, or below either, or whose path, glob or pattern
  names `daemon.env` or the deck's directory (`.config/dot-agent-deck`, or a spelling bash or a glob
  reads the same: `.config//dot-agent-deck`, `.config/./dot-agent-deck`, `.config/dot-agent*`). The path
  is normalised as pi's `resolveToCwd` and `normalizePath` normalise it before a tool reads it, or the
  guard would resolve a value pi reads differently (`@/proc/self/environ`): one leading `@` (pi's
  shorthand for a path) is dropped, a `file://` URL becomes its path (`fileURLToPath` from `node:url`; a
  URL that does not convert throws, and the handler refuses the call rather than resolving the raw value
  as a relative path), the Unicode spaces pi maps (`U+00A0`, `U+2000`-`U+200A`, `U+202F`, `U+205F`,
  `U+3000`) become plain spaces, then `~` is the home directory and the result is resolved against the
  working directory, `..` included. grep and find, the two file tools that walk a directory tree (pi runs
  grep as `rg --hidden`), are refused also when their resolved path is an ancestor of either directory
  (`~`, `~/.config`, `/`, and every spelling that resolves to one: `//`, `/.`, `/..`, enough `../`): a
  grep rooted at `~` for `API_KEY` would print `daemon.env`'s lines, one rooted at `/` walks `/proc`. The
  root is compared as itself, since appending `/` to `/` would ask for a `//` prefix no resolved path
  has. A grep or find with no path runs in the working directory (pi's default), so the guard checks an
  absent path as `.`: from a working directory of `~` it is refused. A read, ls or edit of such an
  ancestor prints no key and stays allowed. A bash command over 32 KB (32768 characters) is refused
  unread, before the patterns, with a reason that points at the write tool (see the bound below); when
  the list failed to load, the reason of every bash refusal, a long command's too, is the load failure. It
  tests `event.toolName` itself, imports only `node:` built-ins and pi's package for types alone (`import
  type`), so the guard does not depend at run time on the module it guards.
  - **What fails closed, and what does not.** pi reports an extension that fails to load and carries on
    without it: a load failure fails open. So the extension's top level does nothing that can throw (the
    list's path is computed inside the loader's `try`), and the factory never throws. After load it fails
    closed: when the list cannot be found, read or parsed, or a pattern does not compile, it refuses every
    bash command and says why, and a handler that throws blocks the call it was checking (pi turns the
    throw into a blocked call; the handler also catches it and refuses). A `session_start` handler shows
    "env-printing refusal loaded" (or that the list failed) when the pane has a UI (`ctx.hasUI`), and
    nothing otherwise.
  - **What the patterns refuse.** Every pattern for a command word shares one prefix: a separator
    (start, newline, `;`, `&`, `|`, `(`, `)`, a backtick, `{`, a quote; `)` ends a case pattern or a
    subshell: `case x in x) env;; esac`), then up to six elements of the prefix (see the bound below):
    redirects in front
    of the command word (`2>&1 env`, `</dev/null env`, `> out env`, `2>x export`: bash runs the printer
    all the same), assignments (`FOO=1 env`), shell keywords (`then`, `do`, `else`, `elif`, `if`,
    `while`, `until`, `!`) and wrappers that run their argument (`sudo`, `command`, `builtin`, `exec`,
    `nohup`, `time`, `xargs`, `timeout`, `nice`, `ionice`, `stdbuf`, `doas`, `busybox`, `eval`, `env`,
    `bash`/`sh`/`zsh`/`dash`/`ksh`/`fish`, `setsid`, `strace`, `ltrace`, `watch`, `script`, `unbuffer`,
    `su`, `runuser`, `coproc`, each with up to four options, an option's one plain value, numeric or
    duration words and assignments: `timeout -s KILL 5 env`, `sudo -u x env`, `xargs -a f env`,
    `bash -c 'env'`, `sudo -u x nice -n 1 timeout 5 env`; and `flock` and `chroot`, which take up to four
    options and one plain word before the command: `flock x env`, `chroot / env`),
    then an optional quote or backslash (a backslash-escaped command word, `\env`, `\printenv`, runs the
    same command). After that prefix they refuse: `env` followed only by options (also `-u`/`-C`/`--unset`/
    `--chdir` with their value: `env -u FOO`, `env -C /tmp`) and assignments, then the end, a separator or
    a redirect, numbered or not (`env`, `env -0`, `env FOO=bar | grep`, `env > out`, `env 2>&1`,
    `env 1>out.txt`); `printenv`; bare `set`; bare `export` and `export -p`;
    `declare`/`typeset` bare or with `-p`/`-x`, where bare `set`, `export`, `declare` and `typeset` also
    count when a redirect, numbered or not, follows (`set 2>&1 | grep KEY`, `typeset 2>&1`); `compgen -v`/`-e`/`-A`; `ps` whose first
    argument is a BSD option word with `e` (`ps e`, `ps eww`, `ps auxeww`); `make` with an option holding
    `p` or `--print-data-base` (`make -p`, `make -pn`). Anywhere in a command they refuse a `/proc` path,
    absolute or relative (`ls /proc/1`, `cat /proc/self/env*`, `cat ../../proc/self/environ`), a
    `pro...` path, relative or globbed, whose second segment after it is `environ`
    (`pro[^/\s]*/+[^/\s]*/+environ`: `cat proc/self/environ` after `cd /`, `cat /pro?/self/environ`; not
    `.../provider/environ_test.go` or `.../projects/shift2eu/docs/dev-environment.md`), `daemon.env`, the
    deck's key directory in every spelling bash reads the same (`\.config[/.]*/dot-agent`: `grep -r .
    ~/.config/dot-agent-deck`, `~/.config//dot-agent-deck/x`, `~/.config/./dot-agent-deck/x`,
    `ls ~/.config/dot-agent*`; the repository's `.dot-agent-deck.toml` and `.dot-agent-deck/`, and
    `~/.local/bin/dot-agent-deck`, are not it), the expansion of a key by name (`$..._KEY`,
    `${..._TOKEN}`, `$..._SECRET`, `$..._PASSWORD`, and `${!` indirection), the interpreter one-liners that
    read the environment themselves (`ENVIRON`, `os.environ`, `os.getenv`, `process.env`, `%ENV`, `$ENV{`,
    `getenv(`, Ruby's bare `ENV` word, `ruby -e 'p ENV'`, Python's bare `environ` word), `gh`'s own token
    printers (`gh auth token`, and `gh auth status` with `-t` or `--show-token`), the credential helpers,
    which print the token they fill in whatever feeds them (`git ... credential fill`, `gh auth
    git-credential`; `git config --global credential.helper` and `git push` stay allowed, and git runs
    the helper `dev/lima.yaml` configures itself, not through an agent's bash), and `jq`'s environment
    (its `env` builtin after `jq`, `jq -n env`, and `$ENV` anywhere, `jq -n '$ENV'`).
  - **The bound on every repeat.** pi matches with JavaScript's backtracking engine. An unbounded group
    repeat around a part that is itself optional (a wrapper's option with its optional plain value) costs
    exponentially many paths on a command that almost matches: before the bound, `"sudo -u "` 22 times and
    a `-` took 4.4 s in Node and 40 times never finished, a hung tool call the agent cannot tell from a
    broken one. Go's RE2 is linear on the same pattern, so the Go tables cannot show the cost; every group
    repeat is therefore bounded, at most `{0,8}`: the shared prefix's elements `{0,6}`, a wrapper's options
    and `flock`/`chroot`'s options `{0,4}`, `env`'s own options and assignments `{0,8}`, `make`'s words
    before `-p` `{0,8}?`. `TestPiEnvDenyListPatternsCannotBacktrackWithoutBound` guards the structure. In
    Node every pattern took under 21 ms on the adversarial inputs (`"sudo -u "` 40 and 200 times and a
    `-`, `"nice -n "` and `"timeout -s "` 40 times, a `git commit -m` holding `time -x` 40 times, 5000
    characters of `-a `, `> `, `2>` or `if `, `sudo a=b ` 2500 times). Bounded is not linear: the
    prefix patterns start an attempt at every separator, and each attempt walks on from there, so a long
    one-line command full of separators costs time quadratic in its length, and so do the `gh`, `git` and
    `jq` patterns, which scan the rest of the command; 100 KB of minified JSON inside an `echo '...'`
    took 35 s across all patterns, a call the agent waits out. A bash command over 32 KB is therefore
    refused unread, before any pattern, and the agent writes such content with the write tool and passes
    the file.
  - **What they leave alone.** `env` followed by a command word runs that command and prints nothing:
    `env GOTOOLCHAIN=local make check`, `env -u FOO make test`, `env -C dir make`,
    `/usr/bin/env bash dev/x.sh` are allowed in pi, unless the command word is itself a printer
    (`env A=1 printenv`, `env X=1 env`), which the shared prefix catches. So are `set -e`, `export FOO=bar`,
    `go env GOPATH`, `ps -o lstart=,args= -p <pid>` (rule 10), `ps -ef`, `ps aux`, `make -C dir check` and
    `make -j4 check`, a redirect in front of a command that prints nothing (rule 10's backgrounded run,
    `</dev/null make check`, `>log 2>&1 make check`), a redirect to or from a file named like a printer
    (`echo x > env`, `cat < set`), and `timeout 600 make check`, `nice -n 10 make check`.
  - The patterns are written in the syntax Go's RE2 and JavaScript share (no lookaround, no backreference,
    no group extension or inline flag, no Unicode property, no `\Q`, `\z`, `\A` or POSIX class), so the Go
    test that holds them to the refused and allowed tables (`test/architecture/pienvdeny_test.go`) tests
    what pi runs; `TestPiEnvDenyListPatternsUseTheNarrowestSyntax` guards that syntax.
- **Claude Code:** `.claude/settings.json`'s `permissions.deny` refuses `env` and `env *`, `/usr/bin/env*`,
  `printenv` and `printenv *`, bare `set`, `export`, `declare` and `typeset` and each followed by a
  redirect, plain, numbered or of both streams (`set >*`, `set [0-9]>*`, `set &>*`, and the same three for
  `export`, `declare` and `typeset`: `set 2>&1`, `export > out`, `declare &>x`), `export -p*`, `declare`
  and `typeset` with `-p*`/`-x*`, `compgen -v*`/`-e*`/`-A*`/`*-A*`, `*/proc/*/environ*`, `make -p*` and
  `make --print-data-base*`, `ps e*`, `*daemon.env*`, the deck's whole key directory
  (`*.config/dot-agent-deck*`), and a command holding a `$` before `API_KEY` or
  `TOKEN` (`*$*API_KEY*`, `*$*TOKEN*`), `gh auth token*`, `gh auth status*-t*` and
  `gh auth status*--show-token*` (bare `gh auth status` names the account, not the token, and stays
  allowed; no skill, make recipe, dev script or workflow types a refused form), the credential helpers
  wherever they stand (`*git credential fill*`, `*gh auth git-credential*`; the only uses in the tree,
  `dev/lima.yaml` and `dev/vm-gh-wrapper.sh`, configure the helper for git at provisioning and are no
  command a role types); and reading `/proc` whole (`Read(//proc)`, `Read(//proc/**)`,
  `Read(//proc/**/environ)`) and the deck's directory (`Read(~/.config/dot-agent-deck/**)`). `//` is
  Claude Code's absolute path: a single leading `/` is relative to the project, so the earlier
  `Read(/proc/*/environ)` matched nothing and is gone. No rule is written for `make` with `-p` after
  another option (`make -C dir -p`) or for BSD `ps` forms other than `ps e*` (`ps auxe`): a prefix rule
  wide enough for them (`make *-p*`, `ps *e*`) would also refuse a target or argument that holds `-p`
  and rule 10's `ps -o lstart=,args= -p <pid>`; pi's patterns hold those forms. A prefix rule cannot tell
  `env VAR=value cmd` from `env | grep`, so the Claude Code roles refuse `env` with arguments altogether;
  no skill, make recipe the agents type or dev script an agent runs uses `env VAR=value cmd` (recipes and
  scripts that use `env -i` or `env -u` run inside make or a script, where no permission rule applies).
  The project settings load for every role, the workers too: `--settings .claude/worker-settings.json`
  adds a layer, it does not replace the project's. `test/architecture/claudesettings_test.go` pins the rules.
- **The tester's prompt** names its scratch directory, `.scratch/` in its worktree (git-ignored), and
  repeats rule 3 in one line (`test/docs/testerprompt_test.go`). An ignored `.scratch/` does not stop
  `git worktree remove`, which refuses only untracked or modified files; it is removed with the worktree.

This is a new ADR rather than an amendment of ADR 0003: ADR 0003 decides which agent and model run the
tester and stays as it is; this one adds a mechanism, a project extension pi executes, and the matching
Claude Code rules, which ADR 0003 does not cover.

## Alternatives considered

- **Words only** (CLAUDE.md and the prompt). What failed on 2026-11-24.
- **A global extension in the VM** (`~/.pi/agent/extensions/`, provisioned by `dev/lima.yaml`). It would
  load without project trust, but it lives outside git, is not reviewed with the change that needs it and
  is not tested by `make check`; the project extension is, and `--approve` loads it already.
- **Keep the keys out of the tester's environment** (a key helper or a wrapper that passes the key only to
  pi's provider). Out of scope (issue #540: how the deck passes keys); it would remove the value rather than
  the command, and stays worth doing. Taken on 2026-11-26 for everything but the tester's own key (the
  amendment below).
- **Parse the command line in the extension.** A shell parser would tell more forms apart, but it is code
  the Go tests could not run; pattern data both sides read is testable here.

## Consequences

- A tester or Claude Code role that runs `env`, `printenv` or bare `set` is refused before the command
  runs, and told why and where its scratch directory is.
- It is a second layer, not a boundary. Patterns cannot parse a shell: a script file the agent writes and
  runs, a variable that holds the command word, an interpreter reached in a form the list does not name,
  or a tool that is not bash still reaches the environment; the Lima VM stays the boundary (ADR 0003).
  Known forms the patterns do not refuse: a printer run by `find -exec env` (or `-execdir`), and a
  wrapper the list does not name; a Go test (or any program) written with the edit tool that logs
  `os.Environ()` and is run with `go test -v`, because the file tools do not scan what they write;
  `script -q -c env /dev/null`; BSD `ps` with `e` after another option word (`ps -e e`, `ps -o args e`);
  `deno eval` or `bun -e` reading `Deno.env` or `Bun.env` (neither is installed); a no-break space inside
  a word, which JavaScript's `\s` reads as a space and bash does not; and `readonly -p` and `local -p`.
  The hook checks only bash and the named file tools: another tool (pi's `powershell`, the `codemode`
  extension, an MCP tool) would bypass it. None is active today (pi's defaults are read, bash, edit and
  write plus grep, find and ls; the tester passes no `--tools`, and no `pwsh` is installed); enabling one
  is a reason to revisit this decision. Also: the deck's directory reached from its parent
  (`cd ~/.config && cat dot-agent-deck/x`) or through a symlink to an ancestor; a command word split by
  quotes (`e''nv`); a wrapper whose command follows a positional argument or a combined option
  (`su x -c env`, `flock /tmp/l -c env`, `script -qc env /dev/null`); a wrapper the list does not name,
  such as the shell named by a variable (`$SHELL -c env`, `${SHELL} -c env`); and, because every repeat
  is bounded, a prefix of more elements than the bound (more than six wrappers, redirects, assignments
  or keywords in front of the printer, more than four options on one wrapper, more than eight options
  or assignments after `env`).
- Whether Claude Code's matcher refuses a redirect in front of the command word (`2>&1 env`) cannot be
  checked offline: its rules are prefix rules on the command it parses, and no rule names that form. pi's
  patterns refuse it.
- A load failure of the extension (a syntax error, a module that cannot be resolved) fails open: pi logs
  it and runs the tester without the refusal. Only the paths after load fail closed (since issue #662 the
  launch wrapper refuses to start pi when the refusal's files are missing, empty or malformed; see the
  amendment below). The `session_start`
  notice shows a TUI pane whether the refusal loaded. Since issue #548, `TestPiEnvDenyExtensionLoads`
  loads the extension with Node's own type stripping, a stricter stand-in for pi's jiti loader: a top
  level that throws fails both, and the check also fails when the default export is no function. It skips
  where `node` is missing or cannot strip types, except where `SHIFT2EU_REQUIRE_NODE=1` (the Test jobs of
  `ci.yml` and `release.yml`, which install the Node release `dev/lima.yaml` pins, `nodeVersion`), which
  fail instead. TypeScript syntax that needs a transform (an `enum`, a `namespace`, parameter properties)
  fails this check although pi would load it, so the extension keeps to erasable syntax.
- A pattern Go accepts but JavaScript does not would refuse nothing in pi and pass every Go test; a
  reviewer holds the patterns to the common syntax.
- Known false positives in pi, each refused although it prints nothing: `declare -x FOO=bar` (it sets and
  exports); a quoted argument that starts with a printer word, or a separator and a printer word inside a
  quoted argument (`git commit -m "env"`, a multi-line `--body` with a line that is only `set`); and a
  command that only names `daemon.env`, `process.env`, `os.environ`, `ENVIRON` or a `/proc/...` path, such
  as a `grep` for them in the tree. Write such a command differently: a message through a file
  (`git commit -F`, `--body-file`), a search through the file tools, which refuse only `/proc` paths,
  `daemon.env` and the deck's key directory. A quote is a separator to the patterns (it is what refuses `bash -c 'env'`), so
  `git commit -m "x; env"` and `git commit -m "env"` are refused too; `git commit -m "fix env bug"` is not.
  `make` with a quoted variable value that holds `-p` (`make test GOFLAGS='-run TestX -p 1'`) is refused
  as `make -p`: the pattern's argument class allows quotes, so it sees `-p` inside the value. That is rare
  here, since the Makefile has no flags variable the agents pass. The bare word `environ` is refused
  wherever it stands (`git grep environ`, `cat x/environ.go`), and so is the bare word `ENV`
  (`grep ENV Dockerfile`, `echo ENV`, a Dockerfile's `ENV` line in a heredoc or a quoted argument):
  `\bENV\b` is what refuses `ruby -e 'p ENV'`; `VM_ENV` and `ENVIRONMENT` are not refused by it. `$ENV`
  is refused anywhere, and so is a command that names `jq` and then a word `env` before the next
  separator (`jq -r .name f.json env.json` is; `jq -r .env f` is not). A `git` command that holds
  `credential fill` in an argument (`git commit -m "drop credential fill"`) is refused as the helper, and
  a command that names `gh auth git-credential` in an argument
  (`gh pr create --body "... gh auth git-credential ..."`) as that helper.
- Known limit of the Claude Code rules: `*git credential fill*` does not match a `git` with an option
  between the word and the subcommand (`git -c x credential fill`); pi's patterns refuse it.
- Rollout: the extension and the tester's `.scratch/` line reach only tester panes started from a dispatch
  worktree created after the merge; a pane started before it runs without them until it is restarted, and
  a branch cut before the merge does not ignore `.scratch/` (its `.gitignore` predates the line).
- pi now executes project code (`.pi/extensions/`) under `--approve`. A change to `.pi/` is reviewed like
  any other code, and the auditor reads it.

## Amendment: the tester starts with only its model credential, and fails closed

Issue #662 named the two gaps the decision above left: every agent in the VM got every variable of
`daemon.env`, so a printing form the patterns miss (see Consequences) still shows the tester the other
agents' keys; and a load failure of the extension fails open. The maintainer decided on 2026-11-26 to close
both now (options 1 and 3 of the issue). The decision above stands; this adds a layer in front of it.

- **`dev/pi-tester.sh` starts the tester.** `.dot-agent-deck.toml`'s tester `command` is
  `dev/pi-tester.sh --provider deepseek --model deepseek-flash --thinking high --approve`, with
  `agent = "pi"` (the deck identifies the agent from the command's start and cannot see behind a launcher).
  The path is relative to the pane's working directory, the unit's folder, where the deck starts it.
- **It fails closed.** Before pi starts, the wrapper checks, relative to that directory,
  `.pi/extensions/deny-env-printing.ts` (a non-empty regular file) and `.pi/deny-env-printing.json` (a JSON
  object whose `patterns` is a non-empty array, checked with `jq`, which `dev/lima.yaml` installs). Anything
  else exits 1 with a message naming the file, and pi is not started. Then node (from `PATH`, the one
  `dev/lima.yaml` pins, which strips TypeScript types on import by default, under the allow list below)
  makes the load pi makes: it imports the extension and requires a function as its default export, and
  compiles every pattern of the list with `new RegExp`, as `TestPiEnvDenyExtensionLoads`'s load script
  does. A load that throws, a pattern that is no regular expression, and a `PATH` without node each refuse
  the same way. Only node's exit status is read; its output is discarded. pi 1.6.1 has no option that makes
  a failed extension load fatal, so the wrapper is what does it.
- **It marks the tester's process tree.** The wrapper sets `SHIFT2EU_PI_TESTER=1` in pi's environment, and
  the VM's login profile (`/etc/profile.d/shift2eu.sh`, written by `dev/lima.yaml`) leaves `daemon.env`
  unsourced when that marker is set and non-empty, so a `bash -l` from the tester's bash tool does not put
  the other agents' keys back into its pane. It takes effect at the maintainer's next VM rebuild.
- **It hands pi an allow list.** pi runs under `env -i` with only the variables the script names, each
  copied when set and each with its reason in the script: `DEEPSEEK_API_KEY`, the basics (`HOME`, `PATH`,
  `USER`, `LOGNAME`, `SHELL`, `TERM`, `COLORTERM`, the locale, `TMPDIR`, the `XDG_*` directories) and the
  deck's pane variables its pi extension and `work-done` need (`DOT_AGENT_DECK_PANE_ID`,
  `DOT_AGENT_DECK_PANE_CAPABILITY`, the socket, the deck executable's path and the like). No GitHub token,
  no other agent's key, no `OTEL_*` (its headers carry a telemetry backend's key), and no name the list does
  not know. `test/devenv/pitester_test.go` pins the list's effect, the marker, the fail-closed start, the
  profile's guard and the wiring.
- **It checks `PATH` first.** Before anything that looks a command up, the wrapper refuses a `PATH` with an
  empty entry (a leading, doubled or trailing `:`) or an entry that is not absolute (`.`, `bin`): either is
  the working directory or under it, the unit's folder, so `jq`, node or pi could be found there. It exits 1
  with a message naming `PATH` (never its value), and nothing starts. The VM's `PATH` has no such entry.
- **The refusal covers the App's token folder and the marker.** The extension's file tools refuse
  `~/.config/shift2eu`, the folder of the agents App's token file `agents-app-token` (ADR 0031), as they
  refuse the deck's key directory: by resolved path (`~/`, absolute, `file://`) and anything under it, as an
  ancestor of a `grep` or `find` (`~`, `~/.config`), and by its spellings in a `path`. A `grep` or `find`
  *pattern* or `glob` may name the folder or the files: a pattern is text the walk looks for and a glob
  only filters the names the walk reports, neither is a place it goes, and where the walk goes is its
  `path`, which stays checked, with its ancestors (round 3's review, finding S1). The `glob` is no longer
  checked for the App's folder or files (issue #691): a lexical glob check added cost and no boundary, since
  pi's grep and find do not follow symlinks, so a walk whose path is neither the folder, under it nor an
  ancestor of it never reaches the token, whatever its glob (symlinks aside: a path that is itself a symlink
  to the folder or an ancestor is a limit documented below, in the amendment's consequences). The deck's
  key-file checks (`daemon.env`, `.config/dot-agent-deck`) on a `glob` and a `pattern` are kept unchanged:
  they err strict, and issue #691 covered the App's token alone; a follow-up may relax them the same way.
  The repository's `dev/agents-app-token.sh`, which mints the token and holds none, stays
  readable. The list refuses any bash command naming the folder
  (`\.config[/.]*/shift2eu\b`: the file, a glob, a `cd`, an `ls`), naming the token or identity file other
  than as the script (`agents-app-(token|identity)($|[^.a-z])`: `find ~ -name agents-app-token`, a globbed
  or quoted folder spelling, while `sed` or `shellcheck` of `dev/agents-app-token.sh` stays allowed), and
  any command naming the marker `SHIFT2EU_PI_TESTER` (an `unset`, an empty assignment, `env -u`), since
  clearing it before a `bash -l` would source `daemon.env` again. It also refuses starting a command with a
  cleared or fresh environment, which drops the marker without naming it: `env -i`,
  `env --ignore-environment`, `env -`, `exec` with an option holding `c`, `sudo -i` (also `-iu`,
  `--login`) and `su -l`, `su -`, `su --login` (round 3's audit, finding SF-1). `exec -c` is refused
  behind the same prefix as the printers (a quote, `then`/`do`/`else`/`elif`/`!`, a wrapper such as
  `command`, `builtin`, `eval` or `time`) and after an option with a value (`exec -a name -c`), while
  `exec ./bin/shift2eu`, `find -exec` and `docker exec` stay allowed; a bare `-` of `env` or `su` counts
  before an operator or a newline as before a space (`su -;true`, round 4's review and audit).
  `test/architecture/pienvdenytoolcall_test.go` runs the extension's `tool_call` handler in node against
  those calls.
- **The wrapper is project code.** `dev/pi-tester.sh` is reviewed and audited as such, like `.pi/`: a
  change to it changes what reaches the tester.

Alternatives considered for the amendment:

- **A deny list** (unset the known keys). It keeps out only the names it knows; a key added to `VM_ENV`
  later would reach the tester. The allow list keeps out every name it does not know.
- **Narrowing the VM's egress** (option 2 of the issue: the tester reaches only DeepSeek and the module
  proxy). Out of scope now by the maintainer's decision of 2026-11-26; it stays a possible later stage,
  and would make a printed or exfiltrated key harder to use from the VM. It, or running the tester as a
  separate uid, is also what would keep the files and other processes' environments below from the tester,
  which the allow list cannot.

Consequences of the amendment:

- The wrapper keeps the other agents' keys out of pi's environment and, with the marker, out of the login
  shells it starts: printing pi's own environment shows only the DeepSeek key and the deck's pane
  variables.
- It does not separate the tester from the other agents. The tester still runs as the VM's one user, so
  `daemon.env`, the agents App's token file (`~/.config/shift2eu/agents-app-token`) and other processes'
  `/proc/<pid>/environ` stay readable to it, guarded only by the extension: its file tools refuse the
  App's token folder as they refuse the deck's, and its patterns refuse a command naming that folder or
  clearing the marker. A program the tester writes and runs can bypass both, and can clear the marker
  itself, since nothing but those patterns protects it. Separating them needs a separate uid or the egress
  stage.
- The patterns are lexical, and a single bash command can still get past them: by quoting or globbing a
  folder name the patterns do not foresee (`~/.conf''ig/shi*`), or through a symlink, since the file tools'
  checks resolve `~`, `..` and the working directory but not symlinks (no `realpath`, round 3's audit,
  finding N-2; left as is, a second layer). A login shell with a fresh environment drops the marker without
  naming it (`exec -c bash -l`, `sudo -i`, `su -l`, `ssh localhost`): the common spellings are refused,
  others are not: `ssh localhost`, `sudo -u <the same user> bash -l`, a GNU long-option abbreviation
  (`env --ignore`, `su --log`), an option after the operand (`su x -l`) or after an option with a value
  (`env -P dir -i`, `env --unset=X -i`). The boundary is the separate uid or the egress stage, not these
  patterns.
- The refusal costs some work. A bash command whose text holds the marker `SHIFT2EU_PI_TESTER`,
  `agents-app-token` or `agents-app-identity` (not followed by `.` or a letter), `env -i`, `env -`,
  `sudo -i`, `su -l`, `su -` or `exec -c` is refused, even as text: a bash `grep` for it, or a
  `git commit -m` whose message names it; the tester uses the grep tool and `git commit -F` instead. The grep and find tools may search the repository for `agents-app` or the folder,
  with a pattern or a glob naming them (issue #691), but not with a path under, or above, the folder. The folder pattern's `\b` also
  refuses a name such as `~/.config/shift2eu-other`, which is harmless.
- The tester still reaches GitHub: the VM's `gh` (ADR 0031) reads the agents App's token from its file
  under `HOME`, not from the environment, so `gh` and `git push` keep working for it.
- A variable the tester's tools turn out to need and the list lacks is absent until the script names it;
  the fix is one line and a reason in `dev/pi-tester.sh`. The deck's own telemetry of pi (its
  `DOT_AGENT_DECK_PI_TELEMETRY` and `OTEL_*`) does not reach the tester's pane.
- The change reaches only tester panes started from a unit folder that has the script, after the
  maintainer approved the changed orchestration in the deck (`dot-agent-deck trust`).

## Amendment: the tester starts from a root-owned installed copy

The amendment above ran the wrapper, and checked the refusal's two files, from the unit's folder. That
folder is writable by the agents, so a branch could weaken `dev/pi-tester.sh`, the extension or the list
before the tester starts on it (the AI reviewer's point 2 on PR #679, accepted by the maintainer on
2026-11-27, issue #692). The decisions above stand; this moves what the tester starts with out of the agents'
reach.

- **The VM installs a fixed copy.** `dev/lima.yaml`'s provisioning embeds the three files verbatim, as
  here-documents (`PITESTER`, `PIDENYEXT`, `PIDENYLIST`) whose text `test/devenv` holds identical to the
  repository's, and installs them with `install -o root -g root`, outside the workspace mount and every
  path an agent writes:
  - `/usr/local/lib/shift2eu/pi-tester/pi-tester.sh`, mode 0755, from `dev/pi-tester.sh`;
  - `/usr/local/lib/shift2eu/pi-tester/extensions/deny-env-printing.ts`, mode 0644, from
    `.pi/extensions/deny-env-printing.ts`;
  - `/usr/local/lib/shift2eu/pi-tester/deny-env-printing.json`, mode 0644, from `.pi/deny-env-printing.json`.
  The extension finds its list at `../deny-env-printing.json` beside its own file, so the installed layout
  keeps it working unchanged.
- **The deck runs the installed wrapper.** `.dot-agent-deck.toml`'s tester `command` is
  `/usr/local/lib/shift2eu/pi-tester/pi-tester.sh --provider deepseek --model deepseek-flash --thinking high --approve`,
  `agent = "pi"` unchanged.
- **The wrapper reads only its installed files.** It finds the extension and the list beside its own path
  (`$0`, which must be absolute), never in the working directory, and runs every fail-closed check of the
  amendment above on those, naming the installed file when it refuses. It passes pi
  `--extension <installed extension>` before the deck's arguments (pi 1.6.1's flag for loading an extension
  file), so pi always loads the installed refusal. The allow list and the `PATH` check are unchanged.
- **The project's copy is not used for the checks or for `--extension`, with one line.** When
  `dev/pi-tester.sh`, `.pi/extensions/deny-env-printing.ts` or `.pi/deny-env-printing.json` in the working
  directory is missing or differs byte for byte (`cmp -s`) from the installed file, the wrapper still starts
  pi with the installed files and prints one line on stderr naming each project path, its installed path,
  that pi still loads the project's `.pi/extensions` (see Consequences, issue #702) and `make vm-pi-tester`.
  The line is expected on a branch that changes one of the three files. It prints no file's content and no
  value.
- **The update path is the maintainer's.** A VM rebuild (`make vm-delete vm-create`) installs the template's
  copy; `make vm-pi-tester`, a host target run from the host clone like `make vm-gh-wrapper`, installs the
  host clone's three files into an existing VM (`dev/vm-pi-tester.sh`, over `limactl shell`'s standard
  input; each file is replaced whole, the wrapper last, and it ends by naming the directory it installed
  into and `vm-pi-tester: done`; the recipe runs it under `env -u SHIFT2EU_PI_TESTER_ROOT`, the tests' root
  prefix, and requires both lines). Lima runs the provisioning on every boot, so provisioning installs the
  three files as one set, guarded on the installed wrapper, which both installers write last and which so
  marks a complete installation: with the wrapper there a boot installs nothing and keeps what
  `make vm-pi-tester` installed last; without it, all three come from the template, the deny files first,
  over any already there. A deny file missing behind an installed wrapper stays missing, and the wrapper then
  refuses to start the tester visibly; reinstall with `make vm-pi-tester`. A VM rebuild installs the
  template's copy. The maintainer
  runs one of them after a merge that changes any of the three files; agents never do. Until then the tester
  starts from the copy installed before, and its pane shows the one line.

Alternatives considered for this amendment:

- **Keep the files in the unit folder, reviewed like `.pi/`.** What the amendment above did; a branch that
  changes them takes effect on the next tester pane of that unit, before any review.
- **Copy the files from the workspace mount at provisioning.** The mount is the agents' to write, so the
  installed copy would be whatever a branch left there; the template's here-documents are reviewed text the
  VM keeps.

Consequences of this amendment:

- A branch's change to the wrapper or the refusal no longer changes what the tester starts with: it shows
  up as the one line, and reaches the VM only when the maintainer, after review and merge, runs
  `make vm-pi-tester` or rebuilds the VM.
- pi still discovers the project's `.pi/extensions` (`--approve` trusts the project), so the unit folder's
  copy of the extension, or any other extension a branch adds there, also loads, in the same node process
  as the installed one. A project extension that blocks more is harmless, but one cannot be assumed unable
  to undo the installed refusal: it runs arbitrary code in pi's process and could, for instance, tamper with
  pi's event dispatch or the shared modules before the installed handler sees a call. This amendment does
  not close that path; it stops a branch from silently changing the wrapper, its checks and the installed
  refusal, which is what the review point asked. pi 1.6.1's `--no-extensions` turns off all extension
  discovery (explicit `--extension` paths still load), which would close it but also stop the global
  extensions under `~/.pi/agent`, the deck's own pi extension among them, from loading; that needs its own
  check and is left for a later decision: issue #702 (start pi with `--no-extensions` and explicit
  extension paths). The amendment below closes it.
- The tester `command` itself lives in the project's `.dot-agent-deck.toml`, so a branch could point it
  elsewhere; that is the same same-uid residual, and the deck's trust approval (`dot-agent-deck trust
  approve`) is what makes a changed command visible.
- Like ADR 0031's gh wrapper, this is a guard against a branch silently changing the wrapper, not a security
  boundary against a determined agent: the tester runs as the VM's one user, who has passwordless sudo, so
  it can replace the installed files or start pi without the wrapper at will. The limits of the amendment
  above (same uid, lexical patterns) stand.
- The one line costs nothing when the copies agree, which they do after `make vm-pi-tester` on a VM whose
  host clone is at the merged `main`.

## Amendment: the tester loads no project extension or setting

After the amendment above, pi (1.6.1, `--approve`) still loaded the unit folder's `.pi/extensions/`, which a
branch can change, in the same node process as the installed refusal (issue #702). Before deciding, three
points were checked against pi 1.6.1's own package (`@earendil-works/pi-coding-agent` 1.6.1, installed
under `~/.local/lib/node_modules/`; paths below are relative to that package) and the deck's own
documentation (`dot-agent-deck docs`, 0.51.4). pi was not run for this.

- **Skills do not depend on extensions.** `--no-extensions` (`-ne`) only narrows the extension list:
  `dist/core/resource-loader.js` builds it as `this.noExtensions ? cliEnabledExtensions : mergePaths(cli,
  configured)`, so discovered, configured (settings `extensions`), package and built-in extensions drop and
  explicit `--extension` paths stay (`docs/cli.md`, Resources: "Explicit `-e` paths still load";
  `docs/settings.md`: "`--no-extensions` disables them too"). Skills come from a separate list that only
  `--no-skills` narrows, so `.pi/settings.json`'s `skills` would still load. Skill paths given with
  `--skill` are added in every case, trusted project or not (`additionalSkillPaths`, resolved against the
  working directory in `dist/main.js`), and go through the same loader (`loadSkills`) as the settings' list.
- **A trusted `.pi/settings.json` is a second branch-writable load path, and `--no-extensions` does not
  close it.** Under `--approve` the project's settings are merged over the user's (`docs/settings.md`:
  "Project settings override agent-directory settings"; `SettingsManager`'s merged `this.settings`), and
  three of those settings run commands with no extension involved:
  - `shellCommandPrefix` is prepended to every bash tool command inside the tool's own `execute`
    (`dist/core/tools/bash.js`: `` `${commandPrefix}\n${command}` ``), after the `tool_call` event the
    refusal checks, so the refusal never sees it: a prefix that prints the environment would run before every
    command the tester makes. `shellPath` picks the shell that runs them.
  - `packages` are resolved by `PackageManager.resolve()` on every start whatever `--no-extensions` says
    (`dist/core/package-manager.js`), and a missing npm or git package is installed then
    (`docs/packages.md`: "Project packages are installed and loaded only after project trust is resolved"),
    which runs the package's install or prepare scripts.
  - `npmCommand` is the command that install runs (`getNpmCommand()`), any program the branch names.
  The trust decision itself is no help on its own: without a command-line override pi falls back to
  `~/.pi/agent/trust.json`, a saved decision for that folder (`dist/main.js`; `docs/security.md`). So the
  wrapper passes `--no-approve`, which overrides any saved decision (`docs/cli.md`: "Ignores trust-gated
  project-local configuration and resources"): pi then reads none of `.pi/settings.json`, `.pi/mcp.json`,
  `.pi/extensions`, `.pi/skills`, `.pi/SYSTEM.md` or `.pi/APPEND_SYSTEM.md` (`docs/security.md`, Resources
  protected by project trust), installs no project package and runs no project `npmCommand`. The skills
  `.pi/settings.json` loaded come in by `--skill` instead.
- **The deck's pi extension sits at one fixed place, which the deck rewrites.** The deck's daemon writes its
  pi extension to `$PI_CODING_AGENT_DIR/extensions/dot-agent-deck` (default
  `~/.pi/agent/extensions/dot-agent-deck`) when it starts, and again each time it starts an agent it knows
  is pi, which `agent = "pi"` on the tester role declares; `dot-agent-deck orchestrator setup` does the same
  by hand (`dot-agent-deck docs troubleshooting`, Pi; `orchestrator --help`). It writes no symbolic link
  and does not write through one. On this VM the directory holds `index.ts` and the modules it imports,
  which pi loads as a directory extension (`docs/extensions.md`, Extension Locations: `*/index.ts`;
  `resolveLocalExtensionSource` in `dist/core/package-manager.js` loads a directory without a package
  manifest as one extension). A deck upgrade rewrites the files in that directory, not its path. The
  wrapper does not pass `PI_CODING_AGENT_DIR` to pi, so pi's agent directory is `$HOME/.pi/agent`, and
  the wrapper names the same directory.

Decision:

- **pi starts with no project trust and no discovered extension.** The wrapper's last line becomes
  `allowlisted exec "${pi}" --no-approve --no-extensions --extension "${extension}" --extension "${deck}" --skill "${skills}" "$@"`:
  `extension` the installed refusal, as before; `deck` the deck's extension directory,
  `${HOME}/.pi/agent/extensions/dot-agent-deck`; `skills` the unit's `${PWD}/.claude/skills`, the
  directory `.pi/settings.json` named. Its own flags come before the deck's arguments, so a `--` among
  those cannot turn them into a message.
- **`--approve` is refused.** An argument `--approve` or `-a` would undo `--no-approve` (pi keeps the
  last), so the wrapper exits 1 before anything else starts and names `.dot-agent-deck.toml`'s tester
  command; that command drops `--approve` in the same change.
- **The deck's extension is checked, and a missing one fails closed.** The wrapper requires `HOME` to be
  an absolute path, the deck's directory not to lie in the working directory, not to be a symbolic link,
  to be a directory, and to hold a regular `index.ts`, `index.js` or `package.json`; otherwise it exits 1
  with a message that names the directory and `dot-agent-deck orchestrator setup`. Without that extension
  the pane would start but never receive a task or deliver a work-done (the deck's notice "this agent's
  deck integration has not connected yet"), a silent stall; failing names the cause in the pane, where the
  dispatcher sees an exited agent instead. The deck writes the directory before it starts a pi agent, so a
  missing one is a fault, not a normal state. Its content is not checked or loaded beforehand: it is not
  part of the refusal, and the deck replaces it on upgrade.
- **The installer accepts only the new line.** `dev/vm-pi-tester.sh`'s check that the wrapper arrived
  whole compares its last line with exactly the new exec line, and `dev/lima.yaml`'s embedded copy follows
  the wrapper.

Alternatives considered for this amendment:

- **`--no-extensions` with `--approve`, as the issue first proposed.** It drops the project's extensions
  but keeps the project's settings, whose `shellCommandPrefix`, `packages` and `npmCommand` run a branch's
  commands outside the refusal's view (above).
- **Start without the deck's extension when it is missing.** The refusal would still hold, but the pane
  would wait for tasks it never gets, with nothing in it to say why.
- **Pass the deck's `index.ts` file instead of its directory.** It ties the wrapper to the deck's current
  file layout, which an upgrade may change; the directory is what pi's own discovery loaded.
- **Silently drop `--approve` from the arguments.** It keeps a stale tester command working but hides that
  the command asks for something the wrapper will not do.

Consequences of this amendment:

- No file under the unit folder's `.pi/` is code pi runs or a setting pi reads; the residual of the
  amendment above (a project extension in pi's process) is closed, and so is the one the issue's question
  found (a project setting that runs commands). The project's `.pi/extensions/deny-env-printing.ts` and
  `.pi/deny-env-printing.json` stay as the source the installed copies are made from, and the wrapper's
  one line still names them when they differ.
- What is left:
  - pi reads the project's `sessionDir` setting before trust is resolved, so `--no-approve` does not stop
    it (`docs/security.md`, Understand project trust): a branch can choose where pi writes the tester's
    session files, which could be a path in the worktree a later commit picks up.
  - Context files (`AGENTS.md`, `CLAUDE.md`) and the skills under `.claude/skills` still come from the unit
    folder, as instructions to the model, not code in pi's process; they were never inside this guard.
  - The deck's extension directory is under `HOME`, writable by the VM's one user, so a program the tester
    runs could change it for its next start; the same-uid limit stated above.
  - `.dot-agent-deck.toml`'s tester command can still add `--extension` or point elsewhere; the deck's trust
    approval is what shows that, as above.
- A unit whose folder still has the old tester command (with `--approve`) gets the refusal once the
  maintainer installs the new wrapper; merging `main` into its branch fixes it.
- `.pi/settings.json` no longer matters to the tester; it stays for a pi run by hand with `--approve`.
