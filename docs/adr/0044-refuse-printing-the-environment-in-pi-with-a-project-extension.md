# 0044. Refuse printing the environment in pi with a project extension, and in Claude Code with deny rules

- Status: Accepted
- Date: 2026-11-24

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
  the command, and stays worth doing.
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
  it and runs the tester without the refusal. Only the paths after load fail closed. The `session_start`
  notice shows a TUI pane whether the refusal loaded.
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
