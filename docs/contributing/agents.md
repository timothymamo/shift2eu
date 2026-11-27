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
email address, so GitHub credits no account for it (the `go-tdd` skill, issue #653); CI's
`coauthor-trailers` job refuses a pull request whose commits carry a co-author address
`.github/co-author-addresses.txt` does not list, with or without spaces before the trailer's colon, or
whose author or committer address it does not list, since a squash merge credits every distinct author
(issue #663). The check reads the list from the pull request's base commit, never from its own checkout
(issue #672): the base branch's list, main's for every pull request to main. A pull request into another
branch is checked against that branch's list, and its commits again against main's when that branch is
merged into main. A pull request that changes only the list is checked against the old one; a change to
the check itself is for the maintainer's code-owner review to catch. A fork's or a new contributor's pull
request therefore fails Lint until their address is on the base branch's
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
