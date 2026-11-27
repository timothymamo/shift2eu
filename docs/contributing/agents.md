# Working with agents

The project is built by a team of AI agents, each in its own role, running in an isolated Lima VM.
This page says who does what and how to set up the VM and open the deck.

The project is built by a team of AI agents orchestrated with
[dot-agent-deck](https://github.com/vfarcic/dot-agent-deck): an **orchestrator** that plans from PRDs
and delegates to a **tester** (writes failing tests first), a **coder**, a **reviewer**, a security
**auditor** and a **releaser** that opens and verifies pull requests. The only human gate is the
maintainer's approval and merge: `Protect main` requires a code owner's approval, and
`.github/CODEOWNERS` names only the maintainer, so no agent, App or bot can land a change alone (PRD #161).
The AI reviewer's comments and change requests are advice the maintainer weighs, never a substitute for
their review: a GitHub Actions workflow outside the VM, `.github/workflows/ai-review.yml`, reviews every
pull request into `main` with `main`'s `review-change` and `security-audit` skills as a reviewer App that
comments or requests changes and never approves, asking Claude through the Claude Code CLI on the
maintainer's subscription with every tool off
([ADR 0033](../adr/0033-review-pull-requests-with-an-ai-reviewer-that-only-comments-or-requests-changes.md),
[set-up](../dev-environment.md#the-ai-reviewer)). After the switch to the agents App, nobody pushes to an agent's branch by hand: a push by
the maintainer could not be approved (the last push needs someone else's approval). Ask the agents for
the change instead ([docs/dev-environment.md](../dev-environment.md#the-gate)).

The tester runs [pi](https://pi.dev) on DeepSeek V4.1 Flash; every other role runs Claude Code. Tests and
the code they check come from different model families, so one model's blind spots are less likely to
end up in both ([ADR 0003](../adr/0003-pi-with-deepseek-for-the-tester-role.md)).
The tester signs its commits with one trailer, `Co-Authored-By: pi (DeepSeek V4.1 Flash)`, without an
email address, so GitHub credits no account for it (the `go-tdd` skill, issue #653); the required
**Co-author trailers (base)** check refuses a pull request whose commits carry a co-author address
`.github/co-author-addresses.txt` does not list, with or without spaces before the trailer's colon, or
whose author or committer address it does not list, since a squash merge credits every distinct author
(issue #663). The check runs from `.github/workflows/coauthor-trailers.yml` under `pull_request_target`
(issue #706, [ADR 0050](../adr/0050-run-the-co-author-check-from-the-base-branch-under-pull-request-target.md)):
GitHub reads that workflow from the base branch, never from the pull request, so for every pull request
into main the job's steps, the `test/lint` that runs, the Go it builds under and the allow list
(issue #672) all come from main. The job checks out only the base commit and fetches the pull request's
commits as git objects that it never checks out or runs. A pull request that changes the workflow,
`test/lint` or `test/architecture`'s pin of the workflow is still checked by main's copies, and its own
change takes effect once it is merged. A pull request into another branch runs that branch's copy; when
it is retargeted to main, or that branch merges into main, main's copy checks the commits again. The
check's git calls ignore the global and the system git configuration (`GIT_CONFIG_GLOBAL=/dev/null`,
`GIT_CONFIG_NOSYSTEM=1`, issue #689); the repository's own `.git/config` is the one `actions/checkout`
writes for the base commit. The move is a two-step rollout: issue #706's pull request keeps `ci.yml`'s
old `coauthor-trailers` job (**Co-author trailers**, inside `Lint`), which runs the base commit's copy of
`test/lint` (PR #703) but is defined by the pull request, and that job checks issue #706's pull request
itself; the new workflow first runs on pull requests opened or updated after the merge. A follow-up pull
request removes the old job, `Lint`'s need of it and the transition parts of the pin. It is merged only
after **Co-author trailers (base)** is required, and it is the first pull request checked by main's
definition alone. Risks remain (ADR 0050). The required check matches only the check run's name and the
GitHub Actions app (15368), never the workflow file or the base branch, so a pull request could add an
always-passing job of the same name to its own `ci.yml`, an edit its diff and the maintainer's
code-owner review show. Also (plausible, not verified): someone with push rights (the agents App can
write contents and workflows) can push a branch `x` whose own `coauthor-trailers.yml` keeps the job name
but runs `exit 0`, then open a second pull request with the same head into `x`, or retarget one; its run
posts a passing check run of that name on the same head SHA, and the pull request into main shows no
workflow change in its diff. A re-run of an old main run replays that run's older allow list (bounded).
The maintainer's pre-merge check in `docs/dev-environment.md` looks at every such check run, cancelled
ones included, at each run's event, workflow file, head SHA and base, and at every pull request with that
head, through search and through the commit's pulls endpoint; the
longer-term fixes are an organization required workflow or a ruleset refusing `.github/workflows/**`
changes on every branch but main. And the job trusts the runner image, the pinned actions and the Go
toolchain, as every job does. A fork's or a new
contributor's pull request therefore fails the check until their address is on the base branch's
`.github/co-author-addresses.txt`: the address is added by a separate pull request, merged first.

The agents run inside a [Lima](https://lima-vm.io) VM that can see this repository's workspace (the
directory you clone it into, which holds only the repository and the worktrees dot-agent-deck's
dispatcher creates beside it) and nothing else on your Mac:

```bash
git clone https://github.com/timothymamo/shift2eu.git ~/src/shift2eu-ws/shift2eu   # the workspace the VM mounts
git clone https://github.com/timothymamo/shift2eu.git ~/src/shift2eu-host          # the host clone, outside it
brew install lima
cd ~/src/shift2eu-host   # every make on the Mac runs here, never in the workspace
make vm-create    # create and provision the VM (Go, Node.js, linters, gh, Claude Code, pi, dot-agent-deck),
                  # copy your Claude and DeepSeek credentials in, and do the once-only setup
make deck         # open the deck; detach and the agents keep running in the VM
```

The host clone's direnv `.envrc` sets `WORKSPACE_REPO` to the workspace's checkout and holds the
credentials ([The host clone](../dev-environment.md#the-host-clone)). Claude Code runs on your Claude
subscription, through a token from `claude setup-token`.
The agents' GitHub credential is the agents App's installation token, which a job on the Mac mints and
writes into the VM (PRD #161, ADR 0031).

Details, including credentials and why Lima rather than devbox:
[docs/dev-environment.md](../dev-environment.md).
