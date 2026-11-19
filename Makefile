# The VM's size and SSH port, for a host that runs several VMs (issue #228, docs/dev-environment.md, Several
# VMs on one host). Empty keeps dev/lima.yaml's value and the port Lima picks. They apply only when make vm-up
# creates the VM: an existing VM is never resized, and vm-up warns that they were ignored. VM_CPUS is a CPU
# count of at most 3 digits (5), VM_MEMORY and VM_DISK a whole size in GiB of at most 5 digits (7GiB, 58GiB),
# VM_SSH_PORT a port in 1024-65535 (60022); vm-up refuses anything else before limactl runs. They can also come
# from the environment. Each is read raw ($(value ...), so make expands nothing in it) and
# exported, so the recipes read it from the environment as "$$VM_CPUS" and no shell ever parses the value.
# Keep this block first: make hands exported variables, command-line ones included, to every $(shell ...),
# expanding them, so a $(shell ...) evaluated before these lines would run a value's $(...) as make code.
VM_CPUS     ?=
VM_MEMORY   ?=
VM_DISK     ?=
VM_SSH_PORT ?=
override VM_CPUS     := $(value VM_CPUS)
override VM_MEMORY   := $(value VM_MEMORY)
override VM_DISK     := $(value VM_DISK)
override VM_SSH_PORT := $(value VM_SSH_PORT)
export VM_CPUS VM_MEMORY VM_DISK VM_SSH_PORT
# The ones that are set, for vm-up's warning when the VM exists already.
VM_RESOURCES_SET = $(strip $(if $(VM_CPUS),VM_CPUS) $(if $(VM_MEMORY),VM_MEMORY) $(if $(VM_DISK),VM_DISK) $(if $(VM_SSH_PORT),VM_SSH_PORT))

# The Lima VM instance every vm-*, deck* and agents-token* target works on (issue #252). Read raw and exported
# like the VM_* above, and for the same reason in this first block: make expands nothing in it, and the
# recipes read it as "$$LIMA_INSTANCE", so no shell parses the value either. Every target that reads it starts
# its recipe with $(LIMA_INSTANCE_CHECK) below, which refuses a name that is not letters, digits, '.', '_' and
# '-' starting with a letter or digit: an empty name matches the empty line an empty listing prints and would
# point at all of ~/.lima, a leading dash reads as an option and a slash as a path. The check is make code,
# not shell: make expands a target's whole recipe before it runs its first line, so a refused name stops the
# target before any of its own commands runs, even under make -i or -k. Its prerequisites run first, but
# none of them reads the name: vm-env's vm-env-check checks the credentials on the host. Targets that do not
# read LIMA_INSTANCE (make check, make build) never check it.
LIMA_INSTANCE ?= shift2eu
override LIMA_INSTANCE := $(value LIMA_INSTANCE)
export LIMA_INSTANCE
LIMA_INSTANCE_RULE = a Lima instance name is letters, digits, '.', '_' and '-', starting with a letter or digit
# Spelled out, one per word: what is left of a name once these are removed is what the rule does not allow.
LIMA_INSTANCE_CHARS := a b c d e f g h i j k l m n o p q r s t u v w x y z A B C D E F G H I J K L M N O P Q R S T U V W X Y Z 0 1 2 3 4 5 6 7 8 9 . _ -
# $(call lima_instance_strip,<chars>,<text>) is <text> without any of the space-separated <chars>.
lima_instance_strip = $(if $(1),$(call lima_instance_strip,$(wordlist 2,$(words $(1)),$(1)),$(subst $(firstword $(1)),,$(2))),$(2))
# Non-empty when LIMA_INSTANCE follows the rule: it is set, starts with neither '.', '_' nor '-', and has
# nothing left once the allowed characters are removed. Whitespace is refused by that last test alone, which
# is why it never hands the remainder to $(if) or $(words) bare: $(if) strips every isspace character (LF, CR,
# VT, FF too) before it tests, and make 3.81 (macOS /usr/bin/make) splits words on blanks only, so a name
# like a<LF>b would leave a lone LF that $(if) reads as empty and $(words) counts as part of one word. 'x' is
# an allowed character, so the remainder never holds one: x<remainder>x is exactly xx, and empty after
# $(subst xx,,...), only when the remainder is empty. Once that test passes the name holds no whitespace, so
# the $(filter) sees it as one word and tests its first character.
LIMA_INSTANCE_VALID = $(and $(LIMA_INSTANCE),$(if $(filter .% _% -%,$(LIMA_INSTANCE)),,ok),$(if $(subst xx,,x$(call lima_instance_strip,$(LIMA_INSTANCE_CHARS),$(LIMA_INSTANCE))x),,ok))
LIMA_INSTANCE_CHECK = $(if $(LIMA_INSTANCE_VALID),,$(error $@: invalid LIMA_INSTANCE '$(LIMA_INSTANCE)': $(LIMA_INSTANCE_RULE)))

# The absolute path of the repository's checkout inside the workspace, the directory the VM mounts, e.g.
# ~/src/ws/shift2eu spelled out (issue #227, docs/dev-environment.md, The host clone). The host clone's direnv
# .envrc sets it (or pass it on the make command line); only the host targets below read it. The workspace is
# its parent, the repository's name its last component. Read raw and exported in this first block like the
# VM_* above, so make expands nothing in it, and a sub-make (make vm-create's) gets it too.
WORKSPACE_REPO ?=
override WORKSPACE_REPO := $(value WORKSPACE_REPO)
export WORKSPACE_REPO

BINARY  := shift2eu
# The HTTP API's binary (PRD #103, docs/adr/0019-serve-assessments-over-a-versioned-http-api-from-a-separate-binary.md).
SERVER  := shift2eu-server
PKG     := github.com/timothymamo/shift2eu
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(PKG)/internal/version.version=$(VERSION) \
	-X $(PKG)/internal/version.commit=$(COMMIT) \
	-X $(PKG)/internal/version.date=$(DATE)
# The container image make image builds (docs/adr/0013-ship-shift2eu-as-a-distroless-container-image.md).
IMAGE   ?= shift2eu
# The HTTP API's container image make server-image builds, the Dockerfile's server stage (PRD #103).
SERVER_IMAGE ?= shift2eu-server
# The container CLI make image runs: docker where it is installed, otherwise nerdctl, the rootless runtime
# of the Lima VM (docs/adr/0017-run-a-rootless-container-runtime-in-the-lima-vm.md).
DOCKER  ?= $(if $(shell command -v docker 2>/dev/null),docker,nerdctl)
# The platforms make image builds, comma separated (linux/amd64,linux/arm64). Empty builds the host's own.
PLATFORMS ?=

# The GOOS/GOARCH pairs the release builds, all with CGO_ENABLED=0. Must match the goos and goarch lists in
# .goreleaser.yaml: test/architecture checks this by running make lint with a fake golangci-lint.
RELEASE_TARGETS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64
# The GOOS/GOARCH pairs make lint lints with CGO_ENABLED=0 beside the release targets, though no release build
# lists them: js/wasm, the site's WebAssembly module (PRD #297), whose js && wasm files no other run
# type-checks. test/architecture requires it in make lint and in CI's lint matrix.
LINT_EXTRA_TARGETS := js/wasm

# The main checkout of the clone make runs in, found from git's common directory, so it is the same when make
# runs in a worktree.
REPO_ROOT := $(patsubst %/.git,%,$(shell git rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo $(CURDIR)/.git))

# The host targets: every target that runs on the Mac as the maintainer, with the maintainer's credentials in
# reach - limactl, launchctl, sudo, gh as the maintainer, the agents App's key (issue #227, ADR 0031). They run
# only from the host clone, a clone outside the workspace, and work on the workspace through WORKSPACE_REPO:
# the VM mounts HOST_WORKSPACE, the parent of WORKSPACE_REPO, which must hold nothing but the repository's
# checkout, HOST_REPO_NAME, and the worktrees dot-agent-deck's dispatcher creates beside it
# (docs/adr/0008-mount-a-workspace-directory-for-dispatcher-mode.md). Every other target (check, test, lint,
# worktree, git-hooks, ...) is the agents', runs in the workspace inside the VM and never reads these.
HOST_TARGETS := agents-token agents-token-refresh agents-token-refresh-start agents-token-refresh-install \
	agents-token-refresh-uninstall deck deck-restart gate-check vm-claude-settings vm-create vm-delete vm-env \
	vm-env-check vm-gh-wrapper vm-init vm-shell vm-stop vm-up
HOST_WORKSPACE_REPO := $(abspath $(WORKSPACE_REPO))
HOST_WORKSPACE      := $(patsubst %/,%,$(dir $(HOST_WORKSPACE_REPO)))
HOST_REPO_NAME      := $(notdir $(HOST_WORKSPACE_REPO))

# The refusal (issue #227). When a goal on the command line is a host target, make checks here, while it reads
# this file, that it runs from the host clone, and stops with $(error) otherwise: before any target, any
# prerequisite (vm-env's vm-env-check), any sub-make and any recipe line runs, even under make -i or -k. A
# sub-make (make vm-create's $(MAKE) vm-up) has its own goal and checks again. It refuses when WORKSPACE_REPO is
# unset or empty, holds a character other than letters, digits and . _ - / + @ # = (WORKSPACE_REPO_CHARS), or
# is not absolute, and when REPO_ROOT, the checkout make runs from, is the workspace (WORKSPACE_REPO's parent,
# its symlinks resolved), inside it or holds it. make with no goal runs help, which is no host target. The goals
# it checks are MAKECMDGOALS, which make sets from its command line's goals (origin default, or undefined with
# no goal). make reads MAKECMDGOALS and .DEFAULT_GOAL from anywhere else too, and then runs goals this check
# never sees: MAKECMDGOALS from the command line (make MAKECMDGOALS= vm-up) or the environment (MAKECMDGOALS=
# make vm-up, honoured with or without make -e), and .DEFAULT_GOAL from the command line (make
# .DEFAULT_GOAL=vm-up runs vm-up with MAKECMDGOALS empty) or the environment (make -e runs it). So make refuses
# every run where either has such an origin, whatever the goals are, and names the variable. make sets neither
# in a sub-make's environment (nothing here exports them), so vm-create's $(MAKE) vm-up is checked as any run.
#
# This is a guard against the maintainer's mistakes, NOT a security boundary: the workspace's Makefile is one
# the agents can edit, so a make run in the workspace runs whatever that file says, check or no check. The
# protection is that the maintainer runs make for these targets only in the host clone, whose files come only
# from git pull --ff-only of GitHub's reviewed main (docs/dev-environment.md, The host clone; ADR 0031).
host_hidden = $(error $(1) is set on the command line or in the environment (its origin is $(origin $(1))), which hides the goals make runs, so make cannot tell whether one is a host target ($(HOST_TARGETS)); those run only from the host clone, never with MAKECMDGOALS or .DEFAULT_GOAL set on the command line or in the environment (docs/dev-environment.md, The host clone))
ifeq ($(filter default undefined,$(origin MAKECMDGOALS)),)
$(call host_hidden,MAKECMDGOALS)
endif
ifneq ($(filter command line environment override,$(origin .DEFAULT_GOAL)),)
$(call host_hidden,.DEFAULT_GOAL)
endif
HOST_GOALS := $(filter $(HOST_TARGETS),$(MAKECMDGOALS))
host_refuse = $(error $(firstword $(HOST_GOALS)): runs only from the host clone, a clone outside the workspace the VM mounts, with WORKSPACE_REPO set in its .envrc to the absolute path of the repository's checkout in the workspace: $(1) (docs/dev-environment.md, The host clone))
# The characters a WORKSPACE_REPO may hold: letters, digits and . _ - / + @ # =. That leaves out whitespace,
# every character a shell reads inside double or single quotes ($ ` " ' \), make's pattern character (%), the
# ':' Lima splits a --mount-only value on, the ',' and '~' it reads too - limactl's --mount-only is a pflag
# StringSlice, which splits its value on commas and expands a leading ~ of every part, so
# /Users/x/ws,~/shift2eu would mount the home directory read-write - and every other one a shell or make gives
# a meaning (; & ( ) ^ { } ! * ? [ ] < > |). The recipes paste the value inside quotes, so these leave it one
# literal word. '=' stays: Go's t.TempDir keeps it from a test's name (GATE_TRANSITION=1), and inside quotes it
# means nothing to a shell, make or Lima. The whitespace test is LIMA_INSTANCE_VALID's (see there). No message
# shows a value that fails these checks, which may be a credential typed where a path goes.
host_comma := ,
host_hash   := \#
WORKSPACE_REPO_CHARS := $(LIMA_INSTANCE_CHARS) / + @ $(host_hash) =
ifneq ($(HOST_GOALS),)
ifeq ($(strip $(WORKSPACE_REPO)),)
$(call host_refuse,WORKSPACE_REPO is not set)
else ifneq ($(subst xx,,x$(call lima_instance_strip,$(WORKSPACE_REPO_CHARS),$(WORKSPACE_REPO))x),)
$(call host_refuse,WORKSPACE_REPO holds a character other than letters$(host_comma) digits and . _ - / + @ $(host_hash) = (its value is not shown))
else ifeq ($(filter /%,$(WORKSPACE_REPO)),)
$(call host_refuse,WORKSPACE_REPO is not an absolute path (its value is not shown))
else ifeq ($(HOST_REPO_NAME),)
$(call host_refuse,WORKSPACE_REPO names no checkout below / (its value is not shown))
endif
# The workspace with its symlinks resolved, as git resolves REPO_ROOT's (macOS: /var is /private/var), or as
# written when it does not exist. The value passed the character check above, so it is safe in single quotes.
host_workspace_real := $(or $(shell cd '$(HOST_WORKSPACE)/' 2>/dev/null && pwd -P),$(HOST_WORKSPACE))
ifneq ($(filter $(host_workspace_real) $(patsubst %/,%,$(host_workspace_real))/%,$(REPO_ROOT)),)
$(call host_refuse,make runs in $(REPO_ROOT) - the workspace $(or $(HOST_WORKSPACE),/) or inside it)
else ifneq ($(filter $(REPO_ROOT)/%,$(host_workspace_real)),)
$(call host_refuse,the workspace $(HOST_WORKSPACE) is inside $(REPO_ROOT) - the checkout make runs in)
endif
endif
# What a host target that opens a shell in the VM (deck, deck-restart, vm-claude-settings, vm-init, vm-shell)
# puts before limactl: the host clone's .envrc holds the agents App's variables, and none of those commands
# needs them, so their limactl runs without them (issue #227, ADR 0031). macOS /usr/bin/env has -u too.
VM_SHELL_ENV := env -u SHIFT2EU_AGENTS_APP_ID -u SHIFT2EU_AGENTS_APP_INSTALLATION_ID -u SHIFT2EU_AGENTS_APP_KEY_FILE
# Credentials vm-env copies from your shell (e.g. direnv) into the VM, by name. Never add
# ANTHROPIC_API_KEY: Claude Code prefers it over the subscription token and bills per call. Never add a
# SHIFT2EU_AGENTS_APP_* variable either: vm-env refuses them, as the agents App's key and its path stay on
# the Mac and only its one-hour tokens enter the VM (make agents-token, PRD #161, ADR 0031). No personal
# GitHub token of the maintainer's either: the agents' only GitHub credential is the App's token (PRD #161
# milestone 3).
VM_ENV ?= CLAUDE_CODE_OAUTH_TOKEN,DEEPSEEK_API_KEY
# The git identity vm-init gives the VM, so the agents commit as you: the Mac's own by default.
GIT_USER_NAME  ?= $(shell git config user.name 2>/dev/null)
GIT_USER_EMAIL ?= $(shell git config user.email 2>/dev/null)
# The Claude Code theme vm-init sets in the VM (dark, light, dark-daltonized, light-daltonized, ...).
CLAUDE_THEME ?= dark

.DEFAULT_GOAL := help

##@ Development

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^##@/ {printf "\n%s\n", substr($$0, 5)} /^[a-zA-Z0-9_-]+:.*## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: server ## Build the shift2eu and shift2eu-server binaries into ./bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/$(BINARY)

.PHONY: server
server: ## Build the shift2eu-server binary (the HTTP API) into ./bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(SERVER) ./cmd/$(SERVER)

.PHONY: image
image: ## Build the local container image shift2eu:<version>, shift2eu:dev and shift2eu:latest from git, as make build (docker or nerdctl; PLATFORMS=linux/amd64,linux/arm64 for both; tags are local, nothing is pushed)
	$(DOCKER) build \
		$(if $(PLATFORMS),--platform "$(PLATFORMS)") \
		--build-arg VERSION="$(VERSION)" \
		--build-arg COMMIT="$(COMMIT)" \
		--build-arg DATE="$(DATE)" \
		--tag "$(IMAGE):$(VERSION)" \
		--tag "$(IMAGE):dev" \
		--tag "$(IMAGE):latest" \
		.

.PHONY: server-image
server-image: ## Build the local container image shift2eu-server:<version>, :dev and :latest, the Dockerfile's server stage, as make image does
	$(DOCKER) build \
		$(if $(PLATFORMS),--platform "$(PLATFORMS)") \
		--target server \
		--build-arg VERSION="$(VERSION)" \
		--build-arg COMMIT="$(COMMIT)" \
		--build-arg DATE="$(DATE)" \
		--tag "$(SERVER_IMAGE):$(VERSION)" \
		--tag "$(SERVER_IMAGE):dev" \
		--tag "$(SERVER_IMAGE):latest" \
		.

.PHONY: fmt
fmt: ## Format code (gofumpt + goimports via golangci-lint)
	golangci-lint fmt

# make lint runs golangci-lint natively first, for the host's GOOS/GOARCH with cgo at the host's default (the
# build make test's -race compiles; it warns when the host's cgo is off, and make test, so make check,
# refuses such a host), then once per release target with cgo off and once for each of LINT_EXTRA_TARGETS
# (js/wasm, the site's module) with cgo off, and last runs the source rules in
# test/lint natively, with SHIFT2EU_LINT_PROBES=require so the forbidigo probes must run. Between the two, once
# as they do not depend on the target, dev/shellcheck.sh runs shellcheck --norc on every shell script git tracks
# and dev/actionlint.sh runs actionlint, pinned in tools/go.mod, on .github/workflows, handing it the path of
# the shellcheck on PATH for their run: scripts (issue #164,
# docs/adr/0022-lint-shell-scripts-and-workflows-with-shellcheck-and-actionlint.md). Both fail when no
# shellcheck is installed; the go command builds actionlint for the host. It attempts every run and fails at
# the end if any of them failed. Each golangci-lint run waits for
# golangci-lint's lock (--allow-serial-runners) instead of failing while another worktree lints, and sees
# the same build settings: GOFLAGS=-mod=readonly (a non-empty GOFLAGS also overrides the go env file's, so
# a -tags of yours cannot take files out of lint), GOAMD64=v1 and GOARM64=v8.0 (the levels the release
# builds at), GOWORK=off (a go.work in a parent directory would switch the run into workspace mode), and
# no GOEXPERIMENT. A `go env -w GOEXPERIMENT` value still applies, as an empty variable
# cannot clear it; CI has none.
# Each run gets a cache directory of its own, as a cache entry of one target can hide another target's
# issues: native-<goos>-<goarch>-cgo<0|1> for the native run, named after what it lints so one cache root
# can serve two hosts (e.g. a macOS host and the Lima VM), and <goos>-<goarch> for a release target. The
# root is $XDG_CACHE_HOME/shift2eu/golangci-lint, else ~/.cache/shift2eu/golangci-lint, outside the repository
# so worktrees share it; GOLANGCI_LINT_CACHE overrides it. To clear the caches, rm -rf the root.
.PHONY: lint
lint: ## Lint natively and for every release target, incl. the architecture (depguard) rules, test/lint, shellcheck and actionlint
	@cache="$${GOLANGCI_LINT_CACHE:-$${XDG_CACHE_HOME:-$${HOME:?set HOME or GOLANGCI_LINT_CACHE}/.cache}/shift2eu/golangci-lint}"; \
	export GOFLAGS=-mod=readonly GOAMD64=v1 GOARM64=v8.0 GOWORK=off; \
	unset GOEXPERIMENT; \
	failed=""; \
	( unset GOOS GOARCH CGO_ENABLED; \
		GOOS="$$(go env GOHOSTOS)" && GOARCH="$$(go env GOHOSTARCH)" && export GOOS GOARCH && \
		cgo="$$(go env CGO_ENABLED)" && \
		echo "lint: native GOOS=$$GOOS GOARCH=$$GOARCH CGO_ENABLED=$$cgo" && \
		{ [ "$$cgo" != 0 ] || echo "lint: warning: cgo is off on this host, so its cgo build is not linted" >&2; } && \
		GOLANGCI_LINT_CACHE="$$cache/native-$$GOOS-$$GOARCH-cgo$$cgo" golangci-lint run --allow-serial-runners \
	) || failed="$$failed native"; \
	for target in $(RELEASE_TARGETS) $(LINT_EXTRA_TARGETS); do \
		goos="$${target%/*}"; goarch="$${target#*/}"; \
		echo "lint: GOOS=$$goos GOARCH=$$goarch CGO_ENABLED=0"; \
		GOOS="$$goos" GOARCH="$$goarch" CGO_ENABLED=0 GOLANGCI_LINT_CACHE="$$cache/$$goos-$$goarch" \
			golangci-lint run --allow-serial-runners || failed="$$failed $$target"; \
	done; \
	dev/shellcheck.sh || failed="$$failed shellcheck"; \
	( unset GOOS GOARCH CGO_ENABLED; dev/actionlint.sh ) || failed="$$failed actionlint"; \
	( unset GOOS GOARCH CGO_ENABLED; SHIFT2EU_LINT_PROBES=require go test -count=1 ./test/lint/ ) || failed="$$failed test/lint"; \
	if [ -n "$$failed" ]; then echo "lint: failed for:$$failed" >&2; exit 1; fi

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum, and the lint tools' tools/go.mod and tools/go.sum
	go mod tidy
	go -C tools mod tidy

.PHONY: openapi
openapi: ## Regenerate api/openapi.bundled.json, the self-contained OpenAPI document each release publishes (issue #139)
	go test -count=1 ./test/openapibundle -update

##@ Testing

.PHONY: test
test: ## Unit + contract tests with the race detector (no network, no credentials)
	go test -race -count=1 ./...

.PHONY: test-integration
test-integration: ## Integration tests against real cloud APIs (read-only credentials required)
	go test -race -count=1 -tags=integration ./...

.PHONY: test-e2e
test-e2e: ## Build and run the binary end to end against testdata fixtures
	go test -count=1 -tags=e2e ./test/e2e/...

.PHONY: cover
cover: ## Unit tests with a coverage report in coverage.out
	go test -race -count=1 -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

.PHONY: vuln
# The second scan covers actionlint, the lint tool tools/go.mod pins: a module of its own, so ./... never
# reaches it (ADR 0022).
vuln: ## Scan code and dependencies, and the lint tools, for known vulnerabilities
	govulncheck ./...
	govulncheck -C tools github.com/rhysd/actionlint/cmd/actionlint

.PHONY: check
check: lint test vuln ## Everything CI gates on; run before every commit

##@ Workflow

# Reads GitHub through gh as the maintainer, to read the rulesets' bypass list (the agents App's token cannot),
# so it is a host target: it runs only from the host clone.
.PHONY: gate-check
gate-check: ## Check that Protect main and CODEOWNERS on main require the maintainer's approval (fails on any gap; host clone)
	dev/protect-main-check.sh

.PHONY: worktree
worktree: ## Create or resume the worktree for a PR: make worktree BRANCH=feat/12-short-name
	@./dev/worktree.sh "$(BRANCH)"

.PHONY: git-hooks
git-hooks: ## Enable .githooks, which refuse commits and pushes to main
	git config core.hooksPath .githooks

##@ Isolated environment (Lima; on the Mac, from the host clone only, with WORKSPACE_REPO set)

# limactl start <name> on an instance whose directory has no lima.yaml (Lima lists it as Broken) reads the name
# as a template and falls back to template:default, a VM that mounts the whole home directory (#237). So a
# listed instance is started by name only when its status is known good (an allow list, not a deny list on
# Broken) and its lima.yaml is still there, and a listing or status query that fails stops vm-up (a failed
# listing would otherwise reach the create line). The listing is matched literally (grep -xF), so a name
# with a dot never matches a look-alike. A Broken instance with any entry named lima.yaml in place, even a
# dangling symlink, is broken for another reason and may keep an intact disk: vm-up points at limactl list
# and limactl start --debug, never at removing it by hand. A Stopped or Running instance whose lima.yaml is a
# dangling symlink is not started either, and gets the same pointer. These checks cannot close the window
# between the status query and the start: the directory can still go in between (a launchd job, a removal by
# hand, a concurrent make), and no flag of Lima 2.x stops limactl start <name> from creating the VM then. So
# an instance that passes them is started with limactl shell --start --workdir / <name> true, never with
# limactl start <name> (issue #254): since Lima 2.0 it starts a Stopped instance, only runs true in a Running
# one and fails with "does not exist" for one that is gone, never creating a VM. Its failure is the recipe's
# last command, so it fails vm-up and no limactl call follows it. --workdir / runs true in a directory every VM
# has, not in the host's current one. Like every target that reads
# LIMA_INSTANCE, vm-up refuses an invalid one with $(LIMA_INSTANCE_CHECK) before its recipe runs (issue #252).
# vm-up sets no git hooks: run from the host clone, git-hooks would set the host clone's, and running git in the
# workspace from the Mac is what issue #227 forbids. The workspace checkout's are set inside the VM,
# by make vm-init (dev/vm-init.sh); the setting lives in the mounted .git/config, so it lasts.
# vm-up then checks VM_CPUS, VM_MEMORY, VM_DISK and VM_SSH_PORT, then that WORKSPACE_REPO is a directory holding
# .git, then that the workspace can be listed and holds nothing stray - the repository's name matched literally
# (a case pattern with the name quoted), so a repository a.b never takes a stray axb for its own - then the
# instance's status, all in one shell line with its limactl calls, so no make flag (-i, -k) can run limactl
# after any of these checks failed. It runs neither git nor anything else in the workspace: test and ls only.
.PHONY: vm-up
vm-up: ## Create or start the Lima VM with only this repository's workspace mounted (VM_CPUS, VM_MEMORY, VM_DISK, VM_SSH_PORT on creation)
	$(LIMA_INSTANCE_CHECK)
	@LC_ALL=C; export LC_ALL; \
	whole() { case "$$1" in ([1-9]*) case "$$1" in (*[!0-9]*) return 1;; esac;; (*) return 1;; esac; \
		[ "$${#1}" -le "$$2" ]; }; \
	refuse() { printf 'vm-up: %s=%s is not %s\n' "$$1" "$$2" "$$3" >&2; exit 1; }; \
	[ -z "$$VM_CPUS" ] || whole "$$VM_CPUS" 3 || refuse VM_CPUS "$$VM_CPUS" 'a CPU count (a whole number from 1 to 999)'; \
	[ -z "$$VM_MEMORY" ] || { case "$$VM_MEMORY" in (*GiB) whole "$${VM_MEMORY%GiB}" 5;; (*) false;; esac; } || \
		refuse VM_MEMORY "$$VM_MEMORY" 'a whole size in GiB from 1GiB to 99999GiB, such as 8GiB'; \
	[ -z "$$VM_DISK" ] || { case "$$VM_DISK" in (*GiB) whole "$${VM_DISK%GiB}" 5;; (*) false;; esac; } || \
		refuse VM_DISK "$$VM_DISK" 'a whole size in GiB from 1GiB to 99999GiB, such as 60GiB'; \
	[ -z "$$VM_SSH_PORT" ] || { whole "$$VM_SSH_PORT" 5 && \
		[ "$$VM_SSH_PORT" -ge 1024 ] && [ "$$VM_SSH_PORT" -le 65535 ]; } || \
		refuse VM_SSH_PORT "$$VM_SSH_PORT" 'a port from 1024 to 65535'; \
	if [ ! -d "$(HOST_WORKSPACE_REPO)" ] || [ ! -e "$(HOST_WORKSPACE_REPO)/.git" ]; then \
		echo "vm-up: WORKSPACE_REPO=$(HOST_WORKSPACE_REPO) is no directory holding .git: set it to the repository's checkout in the workspace the VM mounts (docs/dev-environment.md, The host clone)" >&2; \
		exit 1; \
	fi; \
	entries="$$(ls -A "$(HOST_WORKSPACE)")" || { \
		echo "vm-up: cannot list the workspace $(HOST_WORKSPACE), the parent of WORKSPACE_REPO=$(HOST_WORKSPACE_REPO), so it cannot check that it holds nothing stray" >&2; \
		exit 1; \
	}; \
	stray="$$(printf '%s\n' "$$entries" | while IFS= read -r entry; do \
		case "$$entry" in ('' | "$(HOST_REPO_NAME)" | "$(HOST_REPO_NAME)"-dispatch-* | .DS_Store) ;; (*) printf '%s\n' "$$entry";; esac; \
	done)"; \
	if [ -n "$$stray" ]; then \
		echo "vm-up: the VM mounts $(HOST_WORKSPACE), which must hold only $(HOST_REPO_NAME) and its dispatch worktrees; move these out first:" >&2; \
		echo "$$stray" | sed 's/^/  /' >&2; \
		exit 1; \
	fi; \
	list="$$(limactl list --quiet)" || { \
		echo "vm-up: could not list the Lima instances, so it cannot tell whether $$LIMA_INSTANCE exists" >&2; \
		exit 1; \
	}; \
	if printf '%s\n' "$$list" | grep -qxF -- "$$LIMA_INSTANCE"; then \
		dir="$${LIMA_HOME:-$$HOME/.lima}/$$LIMA_INSTANCE"; \
		status="$$(limactl list --format '{{.Status}}' "$$LIMA_INSTANCE")" || { \
			echo "vm-up: could not read the status of the Lima instance $$LIMA_INSTANCE" >&2; \
			exit 1; \
		}; \
		case "$$status" in \
		(Stopped|Running) \
			if [ -L "$$dir/lima.yaml" ] && [ ! -e "$$dir/lima.yaml" ]; then \
				echo "vm-up: the Lima instance $$LIMA_INSTANCE is $$status, but $$dir/lima.yaml is a dangling symlink: it is there, but Lima cannot use it." >&2; \
				echo "  Its disk may be intact: see why with 'limactl list' and 'limactl start --debug $$LIMA_INSTANCE', and fix the link instead of deleting the instance." >&2; \
				echo "  See Troubleshooting: a Broken instance, in docs/dev-environment.md." >&2; \
				exit 1; \
			elif [ ! -f "$$dir/lima.yaml" ]; then \
				echo "vm-up: the Lima instance $$LIMA_INSTANCE is $$status, but $$dir has no lima.yaml:" >&2; \
				echo "  limactl start $$LIMA_INSTANCE would read the name as a template and fall back to template:default, a VM that mounts the home directory." >&2; \
				echo "  See Troubleshooting: a Broken instance, in docs/dev-environment.md." >&2; \
				exit 1; \
			fi; \
			$(if $(VM_RESOURCES_SET),echo "vm-up: warning: $(VM_RESOURCES_SET) apply only when the VM is created; $$LIMA_INSTANCE exists and starts as it is: an existing VM is never resized (stop it and resize it with limactl edit, or rebuild it with make vm-delete and make vm-create)" >&2;) \
			limactl shell --start --workdir / "$$LIMA_INSTANCE" true;; \
		(Broken) \
			if [ -e "$$dir/lima.yaml" ] || [ -L "$$dir/lima.yaml" ]; then \
				echo "vm-up: the Lima instance $$LIMA_INSTANCE is Broken although $$dir/lima.yaml is there: it is broken for another reason, such as a lima.yaml Lima cannot load." >&2; \
				echo "  Its disk may be intact: see why with 'limactl list' and 'limactl start --debug $$LIMA_INSTANCE', and fix the cause instead of deleting the instance." >&2; \
				echo "  See Troubleshooting: a Broken instance, in docs/dev-environment.md." >&2; \
				exit 1; \
			fi; \
			echo "vm-up: the Lima instance $$LIMA_INSTANCE is Broken: $$dir has no lima.yaml, and starting it would create a default VM that mounts the home directory" >&2; \
			echo "  Remove $$dir by hand once no limactl job uses it; make vm-delete cannot, as limactl delete --force refuses a directory without lima.yaml." >&2; \
			echo "  A launchd job running limactl start may recreate it through its log paths; look for one with" >&2; \
			echo "  'launchctl print system | grep -i lima' and in ~/Library/LaunchAgents and /Library/LaunchDaemons, and unload it first." >&2; \
			echo "  See Troubleshooting: a Broken instance, in docs/dev-environment.md." >&2; \
			exit 1;; \
		(*) \
			echo "vm-up: the Lima instance $$LIMA_INSTANCE has the status '$$status'; vm-up starts only a Stopped or Running instance" >&2; \
			exit 1;; \
		esac; \
	else \
		limactl start --name="$$LIMA_INSTANCE" --mount-only "$(HOST_WORKSPACE):w" --tty=false \
			$${VM_CPUS:+--cpus="$$VM_CPUS"} $${VM_MEMORY:+--memory="$${VM_MEMORY%GiB}"} \
			$${VM_DISK:+--disk="$${VM_DISK%GiB}"} $${VM_SSH_PORT:+--ssh-port="$$VM_SSH_PORT"} dev/lima.yaml; \
	fi

.PHONY: vm-shell
vm-shell: ## Open a shell in the VM at the workspace's checkout, WORKSPACE_REPO (host clone)
	$(LIMA_INSTANCE_CHECK)
	$(VM_SHELL_ENV) limactl shell --workdir "$(HOST_WORKSPACE_REPO)" "$$LIMA_INSTANCE"

# The names in VM_ENV, one per line: commas and spaces separate them, and a name listed twice is listed once.
# The recipe reads VM_ENV from its environment ("$$VM_ENV"), never pasted into its own text, and refuses any
# character but those of names, commas and spaces before it splits the list (with globbing off), so no quote,
# `$` or glob in VM_ENV ever reaches a shell as code.
vm_env_names = $$(printf '%s\n' "$$VM_ENV" | tr ', ' '\n\n' | awk 'NF && !seen[$$0]++')
# How a message names the entry $$name at 1-based position $$i of that list, in $$label (issue #268 L1): the
# entry itself only when it is an upper-case variable name (^[A-Z_][A-Z0-9_]*$), the shape of every credential
# name; anything else may be a credential pasted where a name goes, so only its position is given: entry N is the
# Nth name of VM_ENV counting each name once, at its first place, and skipping empty entries. The rule has
# a limit: a pasted value of that shape (upper-case letters, digits and underscores only, such as an AWS access
# key ID, AKIA..., not secret on its own) is still printed, e.g. in the not-set list. The letters
# are spelled out, as a range such as [A-Z] can match lower-case letters in some shells' locales. It sets
# $$ehidden for a hidden entry and clears it for a shown one, so a message about that one entry says why,
# $(vm_env_entry_why), only when it hides the entry. A message listing several entries sets $$hidden only when it
# lists a hidden one, and says why once, after its list: $(vm_env_hidden_why).
vm_env_upper = ABCDEFGHIJKLMNOPQRSTUVWXYZ
vm_env_label = case "$$name" in ([!$(vm_env_upper)_]* | *[!$(vm_env_upper)0123456789_]*) \
		label="VM_ENV entry $$i (entry $$i is not shown)"; ehidden=1;; \
	(*) label="$$name"; ehidden=;; esac
vm_env_hidden_why = $${hidden:+ It may be a credential typed by mistake.}
vm_env_entry_why = $${ehidden:+ It may be a credential typed by mistake.}
# The refusals of a name for what it is, shared by vm-env-check and vm-env's write step, which runs it before it
# reads the name's value, so make -i, which runs the write step although vm-env-check failed, cannot copy a
# refused name into the VM (issue #268 L2): an agents App variable, a GitHub token (github_token_names below) or
# no variable name. Each names the entry by $$label (vm_env_label, which it runs first), so a refused entry that
# may be a credential is never printed, and exits 1. The GitHub-token refusal adds no $(vm_env_entry_why): its
# names are literal upper-case names, which are always shown, never hidden.
vm_env_refuse = $(vm_env_label); \
	case "$$name" in (SHIFT2EU_AGENTS_APP_*) \
		echo "vm-env: refusing $$label: the agents App's key and its path never enter the VM; make agents-token writes its tokens there.$(vm_env_entry_why) Nothing was written." >&2; \
		exit 1;; esac; \
	case "$$name" in ($(github_token_names)) \
		echo "vm-env: refusing $$label: a GitHub token never enters the VM; the agents' only GitHub credential is the agents App's token, which make agents-token writes there (its value is not shown). Nothing was written." >&2; \
		exit 1;; esac; \
	case "$$name" in ([!A-Za-z_]* | *[!A-Za-z0-9_]*) \
		echo "vm-env: refusing $$label: not a variable name.$(vm_env_entry_why) Nothing was written." >&2; \
		exit 1;; esac
# Only a VM_ENV on the make command line merges (make vm-create hands it to its sub-make the same way); the
# default, an exported VM_ENV, make -e and override all replace, so a VM_ENV left in a .envrc cannot keep a
# stale credential.
vm_env_mode = $(if $(filter command line,$(origin VM_ENV)),merge,replace)
# The variables a GitHub token can arrive in (the two gh reads and their GitHub Enterprise spellings), as one
# alternation that serves both as a shell case pattern and as an extended regular expression. None is a
# credential of the agents (PRD #161 milestone 3): vm-env-check refuses them in VM_ENV, and a merge drops a
# stale line of theirs from the VM's daemon.env instead of keeping it.
github_token_names = GITHUB_TOKEN|GH_TOKEN|GH_ENTERPRISE_TOKEN|GITHUB_ENTERPRISE_TOKEN
# A daemon.env line a merge keeps: a plain NAME=value assignment, nothing the login shell would run.
vm_env_safe_line = ^[A-Za-z_][A-Za-z0-9_]*=[A-Za-z0-9._~+/=-]*\$$

# vm-env-check is vm-env's host-side check, shared by vm-env (its prerequisite) and vm-create (its first step):
# it refuses a VM_ENV holding anything but names, commas and spaces, and an empty one, then checks every name
# and refuses, without calling limactl at all, unless each one is set and not empty: a locked macOS login
# Keychain in an SSH session or an unloaded direnv .envrc must never wipe the credentials the VM has (issue
# #234), nor leave a VM that make vm-create created and provisioned only to stop at vm-env (issue #258). It
# checks VM_ENV the way vm-env reads it: from its environment, never expanded by make, so the refusal of a bad
# VM_ENV never shows its value, which may be a credential typed by mistake. A per-name refusal names an entry
# only when it is an upper-case variable name, and any other entry by its position alone (vm_env_label, issue
# #268 L1).
.PHONY: vm-env-check
vm-env-check: export VM_ENV := $(value VM_ENV)
vm-env-check: ## Check the VM_ENV credentials on the host, as vm-env and vm-create do first (calls no limactl)
	@case "$$VM_ENV" in (*[!A-Za-z0-9_,\ ]*) \
		echo "vm-env: refusing VM_ENV: it may hold only variable names, commas and spaces. Its value is not shown, as it may hold a credential typed by mistake. Nothing was written." >&2; \
		exit 1;; esac; \
	[ -n "$(vm_env_names)" ] || { \
		echo "vm-env: VM_ENV is empty and names no credential. Nothing was written: name at least one, e.g. make vm-env VM_ENV=DEEPSEEK_API_KEY" >&2; \
		exit 1; }
	@set -f; i=0; for name in $(vm_env_names); do \
		i=$$((i + 1)); $(vm_env_refuse); \
	done
	@set -f; missing=''; i=0; hidden=; \
	for name in $(vm_env_names); do \
		i=$$((i + 1)); $(vm_env_label); \
		[ -n "$$(printenv "$$name")" ] || { missing="$$missing $$label"; [ -z "$$ehidden" ] || hidden=1; }; \
	done; \
	if [ -n "$$missing" ]; then \
		echo "vm-env: not set or empty:$$missing.$(vm_env_hidden_why) Nothing was written: the VM's daemon.env is unchanged." >&2; \
		echo "  1. If your credentials come from the macOS login Keychain, an SSH session has it locked: run" >&2; \
		echo "     security unlock-keychain ~/Library/Keychains/login.keychain-db (it asks for the account password)." >&2; \
		echo "  2. Run cd ~ && cd $(REPO_ROOT) so direnv reads the host clone's .envrc again. On any host, check that" >&2; \
		echo "     the .envrc exists, sets them and is allowed (direnv allow), or export the variables yourself." >&2; \
		echo "  3. Re-run make vm-env (or the target that ran it, e.g. make vm-create)." >&2; \
		echo "  To copy only some of them on purpose, name them in VM_ENV: make vm-env VM_ENV=DEEPSEEK_API_KEY keeps the others." >&2; \
		exit 1; \
	fi

# vm-env runs vm-env-check first and writes nothing unless it passes. Only a VM_ENV on the make command line
# merges; an exported VM_ENV replaces like the default, so a VM_ENV left in a .envrc cannot keep a stale
# credential. A replace writes the VM's whole daemon.env with exactly those lines, so a stale line (an old
# ANTHROPIC_API_KEY) is removed. A merge replaces the lines of the names in VM_ENV and keeps the other plain
# NAME=value lines, so `make vm-env VM_ENV=DEEPSEEK_API_KEY` replaces only DEEPSEEK_API_KEY; it drops every other line (a command, a
# comment, a value the shell would expand) with a warning that says how many (never their content); it does not
# keep blank lines either, and does not count them in that warning, as they held nothing (issue #259), and
# prints the names (never the values) of the lines it kept. Either mode removes a GitHub token line
# (one of github_token_names above) and names the variable on stderr, never its value, saying to revoke that token:
# every agent in the VM could read it. The name read errs towards warning: it takes any line that assigns one of
# those names after a non-name character (`GITHUB_TOKEN = x`, `declare -x GH_TOKEN=x`, a leading tab), but not
# MY_GITHUB_TOKEN, and prints only the literal name. The values travel on standard input, never on a
# command line (only the mode does), and the file is written to a temporary file beside daemon.env and moved
# into place, mode 0600, so a failure never leaves half a file; a trap removes the temporary file if the
# VM-side shell is interrupted, and a leftover of a killed run (a regular file) older than a minute or two (find
# rounds -mmin) is removed by the next vm-env: a fresh one may be a concurrent run's (concurrent vm-env runs are
# not supported beyond that). Either mode refuses, before it writes anything, a daemon.env that is not a regular
# file or is a symlink (the move would write into a directory or replace the link; issue #259), and checks that
# again right before the move to narrow the window between the check and mv (mv -T would close it, but the
# tests' fake limactl runs this script on the host, which may be macOS without GNU mv). The small window left is
# accepted: only the VM user's own account can race it, and that account can read daemon.env anyway. It also
# refuses a daemon.env that exists but cannot be read, as it could not check it for a GitHub token; the token
# read fails on sed's own status (not a pipeline's), and a merge's greps accept only status 1 (no match), so a
# read error is never taken for an empty file. Their status-2 branch is defence in depth: the -r pre-check
# refuses an unreadable file first, so it is reached only if the file turns unreadable between the two, a race
# no fake reaches deterministically. The write step refuses itself, before it calls limactl, a VM_ENV that
# names nothing, every name vm-env-check refuses for what it is (vm_env_refuse, the same shell code) and an
# unset or empty credential, so make -i, which runs it although vm-env-check failed, can neither wipe the VM's
# daemon.env nor copy a GitHub token or the agents App's key into it (issue #268 L2); its messages name an
# entry the way vm-env-check does (vm_env_label). It never re-runs vm-env-check through $(MAKE): make -i would
# pass -i to that sub-make too.
.PHONY: vm-env
vm-env: export VM_ENV := $(value VM_ENV)
vm-env: vm-env-check ## Copy the VM_ENV credentials from your shell into the VM (refuses if one is missing)
	$(LIMA_INSTANCE_CHECK)
	@set -f; env_file="$$(i=0; [ -n "$(vm_env_names)" ] || { \
		echo "vm-env: VM_ENV is empty and names no credential. Nothing was written: the VM's daemon.env is unchanged." >&2; exit 1; }; \
	for name in $(vm_env_names); do \
		i=$$((i + 1)); $(vm_env_refuse); \
		value="$$(printenv "$$name")"; \
		[ -n "$$value" ] || { echo "vm-env: $$label is not set or empty.$(vm_env_entry_why) Nothing was written: the VM's daemon.env is unchanged." >&2; exit 1; }; \
		case "$$value" in (*[!A-Za-z0-9._~+/=-]*) echo "vm-env: $$label has unsupported characters.$(vm_env_entry_why) Nothing was written." >&2; exit 1;; esac; \
		printf '%s=%s\n' "$$name" "$$value"; \
	done)" && out="$$(printf '%s\n' "$$env_file" | limactl shell --workdir / "$$LIMA_INSTANCE" \
		sh -c 'umask 077 && LC_ALL=C && export LC_ALL && d="$$HOME/.config/dot-agent-deck" && f="$$d/daemon.env" && \
		t= && trap "rm -f \"\$$t\"" EXIT && trap "exit 1" HUP INT TERM && \
		unreadable() { echo "vm-env: cannot read ~/.config/dot-agent-deck/daemon.env in the VM, so it cannot check the file for a GitHub token to warn about (nor a merge keep its lines). Nothing was written: make it readable by the VM user (chmod 600) and run make vm-env again." >&2; exit 1; } && \
		notregular() { echo "vm-env: ~/.config/dot-agent-deck/daemon.env in the VM is not a regular file (a directory or a symlink), so moving the new file into place would write into it or replace it. Nothing was written: make it a regular file, or remove it, and run make vm-env again." >&2; exit 1; } && \
		mkdir -p "$$d" && if [ -L "$$f" ] || { [ -e "$$f" ] && ! [ -f "$$f" ]; }; then notregular; fi && \
		if [ -e "$$f" ] && ! [ -r "$$f" ]; then unreadable; fi && \
		find "$$d" -maxdepth 1 -type f -name ".daemon.env.??????" -mmin +1 -exec rm -f {} + && \
		new="$$(cat)" && safe="$(vm_env_safe_line)" && \
		names="$$(printf "%s\n" "$$new" | sed -n "s/=.*//p" | paste -sd "|" -)" && \
		removed= && if [ -f "$$f" ]; then \
			found=$$(sed -n -E "s/^(.*[^A-Za-z0-9_])?($(github_token_names))[[:space:]]*=.*/\2/p" "$$f") || unreadable; \
			removed=$$(printf "%s\n" "$$found" | sort -u | paste -sd " " -) || exit 1; fi && \
		dropped=0 && kept= && if [ "$$1" = merge ] && [ -f "$$f" ]; then \
			total=$$(grep -a -c . "$$f") || [ "$$?" -eq 1 ] || unreadable; \
			plain=$$(grep -a -E "$$safe" "$$f") || [ "$$?" -eq 1 ] || unreadable; \
			nplain=$$(printf "%s" "$$plain" | grep -a -c .) || [ "$$?" -eq 1 ] || exit 1; \
			dropped=$$(( total - nplain )); \
			kept=$$(printf "%s\n" "$$plain" | grep -a -v -E "^($$names|$(github_token_names))=") || [ "$$?" -eq 1 ] || exit 1; fi && \
		t="$$(mktemp "$$d/.daemon.env.XXXXXX")" && \
		{ if [ -n "$$kept" ]; then printf "%s\n" "$$kept"; fi && printf "%s\n" "$$new"; } > "$$t" && \
		chmod 600 "$$t" && if [ -L "$$f" ] || { [ -e "$$f" ] && ! [ -f "$$f" ]; }; then notregular; fi && \
		mv "$$t" "$$f" && t= && trap - EXIT HUP INT TERM && \
		if [ "$$1" = merge ]; then printf "vm-env-dropped:%s\n" "$$dropped" && \
			printf "vm-env-kept:%s\n" "$$(sed -n "s/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p" "$$f" | grep -v -E "^($$names)\$$" | paste -sd " " -)"; fi && \
		if [ -n "$$removed" ]; then printf "vm-env-removed:%s\n" "$$removed"; fi' sh $(vm_env_mode))" && \
	echo "vm-env: $$(echo $(vm_env_names)) written to ~/.config/dot-agent-deck/daemon.env in the VM, $(if $(filter replace,$(vm_env_mode)),replacing the whole file,its other lines kept); 'make deck-restart' applies it to a running deck" && \
	dropped="$$(printf '%s\n' "$$out" | sed -n 's/^vm-env-dropped:\([0-9][0-9]*\)$$/\1/p')" && \
	if [ -n "$$dropped" ] && [ "$$dropped" -gt 0 ]; then \
		echo "vm-env: warning: dropped $$dropped line(s) of the VM's daemon.env that were not plain NAME=value assignments (their content is not shown)" >&2; fi && \
	kept="$$(printf '%s\n' "$$out" | sed -n 's/^vm-env-kept:\([A-Za-z0-9_ ]*\)$$/\1/p')" && \
	if [ -n "$$kept" ]; then echo "vm-env: kept (unchanged): $$kept"; fi && \
	removed="$$(printf '%s\n' "$$out" | sed -n 's/^vm-env-removed:\([A-Z_ ]*\)$$/\1/p')" && \
	if [ -n "$$removed" ]; then \
		echo "vm-env: warning: removed $$removed from the VM's daemon.env: every agent in the VM could read it, so revoke that token at GitHub (its value is not shown)" >&2; fi

.PHONY: vm-claude-settings
vm-claude-settings: ## Merge dev/claude-user-settings.json into Claude Code's user settings in the VM
	$(LIMA_INSTANCE_CHECK)
	$(VM_SHELL_ENV) limactl shell --workdir "$(HOST_WORKSPACE_REPO)" "$$LIMA_INSTANCE" dev/claude-settings.sh "$(HOST_WORKSPACE)"

# A login shell (bash -l), because only a login shell loads the credentials vm-env wrote
# (/etc/profile.d/shift2eu.sh). Safe to run again: it changes only what is not set up yet, and the theme
# when CLAUDE_THEME differs. Run it before make deck (docs/dev-environment.md, What make vm-init does).
.PHONY: vm-init
vm-init: ## Once-only VM setup after vm-env and vm-claude-settings: Claude Code first run, git identity, credential checks, deck command, daemon service
	$(LIMA_INSTANCE_CHECK)
	$(VM_SHELL_ENV) limactl shell --workdir "$(HOST_WORKSPACE_REPO)" "$$LIMA_INSTANCE" bash -l dev/vm-init.sh \
		"$(HOST_WORKSPACE)" "$(HOST_WORKSPACE_REPO)" "$(GIT_USER_NAME)" "$(GIT_USER_EMAIL)" "$(CLAUDE_THEME)"

# One target at a time, in order, even under make -j. vm-env-check runs first, before vm-up and any limactl
# call, so a missing credential or a VM_ENV vm-env would refuse creates nothing (issue #258). Each sub-make
# gets a command-line VM_ENV the same way (through MAKEFLAGS), so it is checked as vm-env then reads it.
# When the check refuses, vm-create says on stderr that it created nothing, after vm-env's own messages.
.PHONY: vm-create
vm-create: ## Create and set up the VM, ready for make deck: vm-env-check, vm-up (VM_CPUS, ... pass through), vm-env, vm-claude-settings, vm-init
	$(LIMA_INSTANCE_CHECK)
	@$(MAKE) vm-env-check || { echo "vm-create: nothing was created: the credential check runs before the VM is created" >&2; exit 1; }
	$(MAKE) vm-up
	$(MAKE) vm-env
	$(MAKE) vm-claude-settings
	$(MAKE) vm-init

# Where make agents-token-refresh-start keeps the pid and the log of the agents App token refresh, and the
# refresh its lock, one set per VM instance: on the Mac, outside the workspace (ADR 0031).
AGENTS_TOKEN_STATE = $${XDG_STATE_HOME:-$${HOME:?set HOME}/.local/state}/shift2eu

# The token targets run on the Mac with the App's key in reach. Like every host target they run only from the
# host clone, whose own direnv .envrc holds the three SHIFT2EU_AGENTS_APP_* variables, and make refuses them
# elsewhere (HOST_TARGETS above; docs/dev-environment.md, The host clone). Their recipes use only limactl
# and LIMA_INSTANCE, not WORKSPACE_REPO, but like every host target they refuse to run without it (the
# host-clone refusal). The host clone does not put the key out of reach of code that runs as you: what
# remains open is issue #227's part 2 (ADR 0031, Consequences).
# Like dev/protect-main-check.sh and dev/shellcheck.sh, their scripts run by path and shebang, with no
# explicit interpreter (issue #242): both are executable (100755) in git.

.PHONY: agents-token
agents-token: ## Mint one agents App installation token on the Mac and write it, and the bot identity, into the VM (host clone)
	$(LIMA_INSTANCE_CHECK)
	@dev/agents-app-token.sh mint

.PHONY: agents-token-refresh
agents-token-refresh: ## Mint the agents App token now and again before each one expires, in the foreground, until stopped (host clone)
	$(LIMA_INSTANCE_CHECK)
	@dev/agents-app-token.sh refresh

# Starts the refresh of this VM instance in the background unless one already runs: the one whose pid the
# instance's lock holds (a refresh started by hand with make agents-token-refresh holds it too), or the one
# this target started earlier. It warns when that one has stopped, and prints where the log is. When launchd
# has the instance's LaunchDaemon loaded (agents-token-refresh-install), it warns and starts nothing, writing
# no file: the job is the instance's refresher. Without launchctl (Linux) that check is skipped. Like install
# and uninstall below, it reads LIMA_INSTANCE from its environment ($$LIMA_INSTANCE) and refuses an invalid
# name with $(LIMA_INSTANCE_CHECK) before it runs: the name becomes part of its file names and of the job's label.
# The refresher runs without the default VM_ENV credentials, CLAUDE_CODE_OAUTH_TOKEN and DEEPSEEK_API_KEY
# (env -u), which it never needs and which a shell with the host clone's .envrc loaded may hold (issue #227).
# A name a custom VM_ENV adds is not stripped.
.PHONY: agents-token-refresh-start
agents-token-refresh-start: ## Start the agents App token refresh in the background, one per VM instance, and print where its log is (host clone)
	$(LIMA_INSTANCE_CHECK)
	@if [ -z "$${SHIFT2EU_AGENTS_APP_ID:-}" ] || [ -z "$${SHIFT2EU_AGENTS_APP_INSTALLATION_ID:-}" ] || [ -z "$${SHIFT2EU_AGENTS_APP_KEY_FILE:-}" ]; then \
		echo "agents-token-refresh-start: set SHIFT2EU_AGENTS_APP_ID, SHIFT2EU_AGENTS_APP_INSTALLATION_ID and SHIFT2EU_AGENTS_APP_KEY_FILE in the host clone's .envrc (docs/dev-environment.md, The host clone)" >&2; \
		exit 1; \
	fi; \
	label="$(AGENTS_TOKEN_LAUNCHD_LABEL_PREFIX)$$LIMA_INSTANCE"; \
	state="$(AGENTS_TOKEN_STATE)"; pid_file="$$state/agents-token-refresh-$$LIMA_INSTANCE.pid"; log="$$state/agents-token-refresh-$$LIMA_INSTANCE.log"; \
	if command -v launchctl >/dev/null 2>&1 && printed="$$(launchctl print "system/$$label" 2>/dev/null)"; then \
		echo "agents-token-refresh-start: warning: the LaunchDaemon $$label runs the agents App token refresh of $$LIMA_INSTANCE, so none is started beside it; check it with sudo launchctl print system/$$label, its log is $$log, and make agents-token-refresh-uninstall removes it" >&2; \
		job_script="$$(printf '%s\n' "$$printed" | $(AGENTS_TOKEN_LAUNCHD_SCRIPT_OF))"; \
		own_script="$$(pwd -P)/dev/agents-app-token.sh"; \
		if [ -z "$$job_script" ]; then \
			echo "agents-token-refresh-start: warning: launchctl print names no dev/agents-app-token.sh in the job's arguments, so it may run another clone's script: check its arguments with sudo launchctl print system/$$label, and install it from the host clone (make agents-token-refresh-install)" >&2; \
		elif [ "$$job_script" != "$$own_script" ]; then \
			echo "agents-token-refresh-start: warning: the LaunchDaemon runs $$job_script, another clone's script, not this clone's $$own_script: edits and pulls here do not reach the job; install it from the host clone (make agents-token-refresh-install there)" >&2; \
		fi; \
		exit 0; \
	fi; \
	lock_pid="$$state/agents-token-refresh-$$LIMA_INSTANCE.lock/pid"; \
	(umask 077 && mkdir -p "$$state" && : >>"$$log") || exit 1; \
	started="$$(cat "$$pid_file" 2>/dev/null || true)"; \
	for pid in "$$(cat "$$lock_pid" 2>/dev/null || true)" "$$started"; do \
		case "$$pid" in ''|*[!0-9]*) continue ;; esac; \
		if kill -0 "$$pid" 2>/dev/null && ps -p "$$pid" -o command= 2>/dev/null | grep -q 'agents-app-token.sh refresh'; then \
			echo "agents-token-refresh-start: the agents App token refresh of $$LIMA_INSTANCE is already running (pid $$pid); its log is $$log"; \
			exit 0; \
		fi; \
	done; \
	if [ -n "$$started" ]; then \
		case "$$started" in *[!0-9]*) started=unknown ;; esac; \
		echo "agents-token-refresh-start: warning: the agents App token refresh of $$LIMA_INSTANCE (pid $$started) had stopped; its log's last lines:" >&2; \
		tail -n 3 "$$log" 2>/dev/null | sed 's/^/  /' >&2 || true; \
	fi; \
	env -u CLAUDE_CODE_OAUTH_TOKEN -u DEEPSEEK_API_KEY nohup dev/agents-app-token.sh refresh </dev/null >>"$$log" 2>&1 & \
	pid="$$!"; \
	(umask 077 && echo "$$pid" >"$$pid_file") || exit 1; \
	echo "agents-token-refresh-start: started the agents App token refresh of $$LIMA_INSTANCE (pid $$pid); its log is $$log"

# The LaunchDaemon that runs the refresh at boot, with nobody logged in, as you (issue #229, ADR 0034): one job
# per VM instance, whose plist dev/agents-token-launchd.sh renders from the host clone's .envrc (the plist, not
# direnv, holds the variables, copied at install time: install again after changing them). The job runs
# /bin/sh dev/agents-app-token.sh refresh from the host clone, never make or git, and logs where
# agents-token-refresh-start does. Installing needs sudo; both targets print every sudo command before they
# run it. A refresh started by hand keeps the instance's lock while it runs: the job then exits 0 at once and
# launchd tries again every ThrottleInterval (5 minutes), so install says so and names the pid to stop. It reads
# the lock before the bootstrap, since the job's own refresh takes the lock as soon as it loads, and warns only
# about a refresh that held it then, still holds it, and is not the pid launchctl print reports for the job.
#
# install and uninstall read LIMA_INSTANCE from their environment ($$LIMA_INSTANCE) and refuse an invalid one
# with $(LIMA_INSTANCE_CHECK) before they run, so make never splices a value into the shell text that runs sudo.
#
# install refuses, before any sudo:
#   - a run as root (sudo make, sudo -E make), before anything else: the job runs as the account that installs
#     it, and as root it would run the host clone's script, which you can edit, at every boot. Run it as you;
#     it runs sudo itself;
#   - a clone whose dev/ or dev/agents-app-token.sh is a symlink: the job would run a file the install never
#     looked at;
#   - a clone that is one of the VM's mounts, lies under one, or holds one: the agents can edit a mounted
#     clone (or its dev/ through a mount inside it), and the job would run their code at every boot, as you,
#     with the App's key in reach. It asks `limactl list` once for the instance's name and mount locations
#     (`~` and `~/...` expand against HOME, a symlinked location is resolved). The first line must be exactly
#     the instance: limactl may print nothing and exit 0 for an instance it does not know, and the mounts of
#     another instance prove nothing. When limactl fails or names no such instance the install refuses, since
#     the check cannot be done: create the VM first, or check by hand that limactl list prints the instance.
# It then renders the plist, lints it with plutil and prints it with its SHA-256 and the script the job runs.
# A loaded job is booted out first, and the install waits (at most 10 seconds) until launchctl print stops
# finding it; one that stays loaded stops the install before it installs anything. After `sudo install` it
# reads the installed file back, as you, and compares its SHA-256 with the one it printed: on a mismatch, or
# when the file cannot be read, it removes the file again (a plist left there would load at the next boot)
# and fails. launchctl bootstrap is tried twice; when both fail, launchctl print decides: a loaded job is a
# success (bootstrap can load the job and still exit non-zero), otherwise the install fails and says that the
# checked plist stays where launchd loads it at the next boot.
#
# uninstall boots the job out, waits (at most 10 seconds, as install does) until launchctl print stops finding
# it and removes the plist either way. A job that stays loaded is reported with the command that unloads it,
# and the uninstall still exits 0: the plist is gone, and running it again would change nothing.
#
# AGENTS_TOKEN_LAUNCHD_DIR is where launchd reads LaunchDaemons from. It is a hook for the tests, which point
# it into their sandbox; never change it on a real Mac. Root writes there, so install and uninstall refuse a
# value that is not absolute or holds anything but letters, digits, /, ., _ and -, or a .. component. The value
# comes only from the make command line or from this file, never from the environment: a stale export (from a
# test, an earlier session) would have root install the plist where launchd never looks at boot, and the job
# would be gone after the next reboot, so both targets refuse it before any sudo. Its origin is read here, at
# parse time, since the target-specific export below would change it inside the recipes.
AGENTS_TOKEN_LAUNCHD_DIR_ORIGIN := $(origin AGENTS_TOKEN_LAUNCHD_DIR)
AGENTS_TOKEN_LAUNCHD_DIR ?= /Library/LaunchDaemons
AGENTS_TOKEN_LAUNCHD_LABEL_PREFIX = io.github.timothymamo.shift2eu.agents-token-refresh.
# The script a loaded job runs, read from `launchctl print system/<label>`: the first line of its arguments
# block that ends in /dev/agents-app-token.sh, with the indentation removed (empty when there is none).
AGENTS_TOKEN_LAUNCHD_SCRIPT_OF = awk '/^[[:space:]]*arguments = \{/ { a = 1; next } a && /^[[:space:]]*\}/ { exit } a { sub(/^[[:space:]]+/, ""); if ($$0 ~ /\/dev\/agents-app-token\.sh$$/) { print; exit } }'
# The check of AGENTS_TOKEN_LAUNCHD_DIR, the directory root writes the plist into and removes it from.
AGENTS_TOKEN_LAUNCHD_DIR_CHECK = case "$(AGENTS_TOKEN_LAUNCHD_DIR_ORIGIN)" in \
	environment*) \
		echo "$@: refused: AGENTS_TOKEN_LAUNCHD_DIR comes from the environment, where a stale export would have root install the plist where launchd never looks at boot: unset it (it is a test hook: leave it at /Library/LaunchDaemons, or pass it on the make command line)" >&2; exit 1 ;; \
	esac; \
	case "$${AGENTS_TOKEN_LAUNCHD_DIR:-}" in \
	/*) ;; \
	*) echo "$@: AGENTS_TOKEN_LAUNCHD_DIR must be an absolute path (it is a test hook: leave it at /Library/LaunchDaemons)" >&2; exit 1 ;; \
	esac; \
	case "$$AGENTS_TOKEN_LAUNCHD_DIR" in \
	*[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789/._-]* | */../* | */..) \
		echo "$@: AGENTS_TOKEN_LAUNCHD_DIR may hold only letters, digits, /, ., _ and -, and no .. component, since root writes there (it is a test hook: leave it at /Library/LaunchDaemons)" >&2; exit 1 ;; \
	esac

agents-token-refresh-install agents-token-refresh-uninstall: export AGENTS_TOKEN_LAUNCHD_DIR := $(AGENTS_TOKEN_LAUNCHD_DIR)

.PHONY: agents-token-refresh-install
agents-token-refresh-install: ## Install (or update) the LaunchDaemon that runs the agents App token refresh at boot, one per VM instance; needs sudo (host clone, macOS)
	$(LIMA_INSTANCE_CHECK)
	@if [ "$$(id -u)" = 0 ]; then \
		echo "agents-token-refresh-install: refused: run it as the maintainer, not as root (no sudo make); it runs sudo itself. The job runs as the account that installs it, and as root it would run the host clone's script, which the maintainer can edit, at every boot" >&2; \
		exit 1; \
	fi; \
	if [ -z "$${SHIFT2EU_AGENTS_APP_ID:-}" ] || [ -z "$${SHIFT2EU_AGENTS_APP_INSTALLATION_ID:-}" ] || [ -z "$${SHIFT2EU_AGENTS_APP_KEY_FILE:-}" ]; then \
		echo "agents-token-refresh-install: set SHIFT2EU_AGENTS_APP_ID, SHIFT2EU_AGENTS_APP_INSTALLATION_ID and SHIFT2EU_AGENTS_APP_KEY_FILE in the host clone's .envrc (docs/dev-environment.md, The host clone)" >&2; \
		exit 1; \
	fi; \
	command -v launchctl >/dev/null 2>&1 || { echo "agents-token-refresh-install: no launchctl: the LaunchDaemon is for the Mac; elsewhere use make agents-token-refresh-start" >&2; exit 1; }; \
	$(AGENTS_TOKEN_LAUNCHD_DIR_CHECK); \
	label="$(AGENTS_TOKEN_LAUNCHD_LABEL_PREFIX)$$LIMA_INSTANCE"; dest="$${AGENTS_TOKEN_LAUNCHD_DIR%/}/$$label.plist"; \
	clone="$$(pwd -P)" || exit 1; script="$$clone/dev/agents-app-token.sh"; \
	for link in "$$clone/dev" "$$script"; do \
		if [ -L "$$link" ]; then \
			echo "agents-token-refresh-install: refused: $$link is a symlink, so the job would run, at every boot, as you, with the App's key in reach, a file outside the clone $$clone that this install never looked at. Run the install from the host clone, with dev/ and dev/agents-app-token.sh as git checked them out (docs/dev-environment.md, The host clone)" >&2; \
			exit 1; \
		fi; \
	done; \
	listed="$$(limactl list --format '{{.Name}}{{"\n"}}{{range .Config.Mounts}}{{.Location}}{{"\n"}}{{end}}' "$$LIMA_INSTANCE")" || { \
		echo "agents-token-refresh-install: limactl list could not read the mounts of the VM $$LIMA_INSTANCE, so it cannot check that $$clone is outside them: create the VM first (make vm-create), check that limactl list prints $$LIMA_INSTANCE, and run the install again from the host clone" >&2; \
		exit 1; \
	}; \
	name="$$(printf '%s\n' "$$listed" | sed -n 1p)"; \
	if [ "$$name" != "$$LIMA_INSTANCE" ]; then \
		echo "agents-token-refresh-install: limactl list answered about $${name:-no instance at all}, not the VM $$LIMA_INSTANCE, so it cannot check that $$clone is outside that VM's mounts: create the VM first (make vm-create), check that limactl list prints $$LIMA_INSTANCE, and run the install again from the host clone" >&2; \
		exit 1; \
	fi; \
	printf '%s\n' "$$listed" | sed 1d | while IFS= read -r location; do \
		case "$$location" in '') continue ;; '~') location="$$HOME" ;; '~/'*) location="$$HOME/$${location#'~/'}" ;; esac; \
		resolved="$$(cd "$$location" 2>/dev/null && pwd -P)" || resolved="$$location"; \
		while [ "$$resolved" != / ] && [ "$${resolved%/}" != "$$resolved" ]; do resolved="$${resolved%/}"; done; \
		case "$$clone/" in "$${resolved%/}/"*) \
			echo "agents-token-refresh-install: refused: $$clone is inside $$location, which the VM $$LIMA_INSTANCE mounts: the agents can edit it, and the job would run their code at every boot, as you, with the App's key in reach. Run the install from the host clone, a clone outside every mount of the VM (docs/dev-environment.md, The host clone)" >&2; \
			exit 1 ;; \
		esac; \
		case "$$resolved/" in "$$clone/"*) \
			echo "agents-token-refresh-install: refused: the VM $$LIMA_INSTANCE mounts $$location, which is inside $$clone: through it the agents can edit the clone the job runs, at every boot, as you, with the App's key in reach. Run the install from the host clone, a clone no mount of the VM reaches into (docs/dev-environment.md, The host clone)" >&2; \
			exit 1 ;; \
		esac; \
	done || exit 1; \
	state="$(AGENTS_TOKEN_STATE)"; log="$$state/agents-token-refresh-$$LIMA_INSTANCE.log"; \
	lock_pid="$$state/agents-token-refresh-$$LIMA_INSTANCE.lock/pid"; \
	tmp="$$(mktemp "$${TMPDIR:-/tmp}/agents-token-refresh-plist.XXXXXX")" || exit 1; \
	trap 'rm -f "$$tmp"' EXIT; \
	dev/agents-token-launchd.sh plist >"$$tmp" || exit 1; \
	plutil -lint "$$tmp" || { echo "agents-token-refresh-install: plutil refused the rendered plist, so launchd would load no job: nothing was installed" >&2; exit 1; }; \
	sum="$$(shasum -a 256 <"$$tmp" | cut -d ' ' -f 1)" || exit 1; \
	echo "agents-token-refresh-install: installing this plist as $$dest (it holds the App ID, the installation ID and the key's path, never the key):"; \
	cat "$$tmp"; \
	echo "agents-token-refresh-install: its SHA-256 is $$sum"; \
	echo "agents-token-refresh-install: the job runs $$script at every boot as $$(id -un): it must be the host clone's, outside the workspace and every mount of the VM"; \
	echo "agents-token-refresh-install: creating $$state and the log $$log (mode 0600) as $$(id -un)"; \
	(umask 077 && mkdir -p "$$state" && : >>"$$log") || exit 1; \
	if launchctl print "system/$$label" >/dev/null 2>&1; then \
		echo "agents-token-refresh-install: $$label is loaded already: unloading it to load the new plist"; \
		echo "+ sudo launchctl bootout system/$$label"; \
		sudo launchctl bootout "system/$$label" || echo "agents-token-refresh-install: launchctl bootout exited non-zero; checking whether $$label unloaded anyway"; \
		tries=0; \
		while [ "$$tries" -lt 10 ] && launchctl print "system/$$label" >/dev/null 2>&1; do sleep 1; tries=$$((tries + 1)); done; \
		if launchctl print "system/$$label" >/dev/null 2>&1; then \
			echo "agents-token-refresh-install: ERROR: the old job $$label is still loaded $$tries second(s) after launchctl bootout, so nothing was installed: it keeps running the plist it was loaded from. Check it with sudo launchctl print system/$$label, wait for it to unload or run make agents-token-refresh-uninstall, then run make agents-token-refresh-install again" >&2; \
			exit 1; \
		fi; \
	fi; \
	held="$$(cat "$$lock_pid" 2>/dev/null || true)"; \
	case "$$held" in ''|*[!0-9]*) held='' ;; *) \
		kill -0 "$$held" 2>/dev/null && ps -p "$$held" -o command= 2>/dev/null | grep -q 'agents-app-token.sh refresh' || held='' ;; \
	esac; \
	echo "+ sudo install -m 0644 -o root -g wheel $$tmp $$dest"; \
	sudo install -m 0644 -o root -g wheel "$$tmp" "$$dest" || exit 1; \
	installed="$$( { shasum -a 256 | cut -d ' ' -f 1; } 2>/dev/null <"$$dest")" || installed=''; \
	if [ "$$installed" != "$$sum" ]; then \
		if [ -n "$$installed" ]; then \
			why="it has the SHA-256 $$installed, not the $$sum of the plist printed above"; \
		else \
			why="it could not be read to compare its SHA-256 with the $$sum of the plist printed above"; \
		fi; \
		echo "+ sudo rm -f $$dest"; \
		if ! sudo rm -f "$$dest"; then \
			echo "agents-token-refresh-install: ERROR: $$dest is not the plist printed above ($$why), and sudo rm -f could not remove it: remove it by hand before the next boot loads it. It was not loaded: no token refresh job of $$LIMA_INSTANCE is loaded now" >&2; \
			exit 1; \
		fi; \
		echo "agents-token-refresh-install: ERROR: $$dest is not the plist printed above ($$why), so it was removed again and not loaded: no token refresh job of $$LIMA_INSTANCE is loaded now. Look for what changed the file, then run make agents-token-refresh-install again, or start a refresh by hand with make agents-token-refresh-start" >&2; \
		exit 1; \
	fi; \
	echo "agents-token-refresh-install: $$dest has the SHA-256 printed above"; \
	echo "+ sudo launchctl bootstrap system $$dest"; \
	if ! sudo launchctl bootstrap system "$$dest"; then \
		sleep 1; \
		echo "+ sudo launchctl bootstrap system $$dest"; \
		if ! sudo launchctl bootstrap system "$$dest"; then \
			if launchctl print "system/$$label" >/dev/null 2>&1; then \
				echo "agents-token-refresh-install: launchctl bootstrap exited non-zero twice, but launchctl print finds $$label loaded: the job is loaded" >&2; \
			else \
				echo "agents-token-refresh-install: ERROR: launchctl bootstrap failed twice: NO token refresh job of $$LIMA_INSTANCE is loaded now, and the VM's token expires within the hour. The checked plist stays at $$dest, so launchd loads it at the next boot. Run make agents-token-refresh-install again, or start a refresh by hand with make agents-token-refresh-start" >&2; \
				exit 1; \
			fi; \
		fi; \
	fi; \
	echo "agents-token-refresh-install: loaded $$label: it runs now, at every boot, and again whenever it exits"; \
	if [ -n "$$held" ] && [ "$$(cat "$$lock_pid" 2>/dev/null || true)" = "$$held" ] && kill -0 "$$held" 2>/dev/null && ps -p "$$held" -o command= 2>/dev/null | grep -q 'agents-app-token.sh refresh'; then \
		job_pid="$$(launchctl print "system/$$label" 2>/dev/null | awk '/^[[:space:]]*pid = [0-9]+[[:space:]]*$$/ { print $$3; exit }')"; \
		if [ "$$held" != "$$job_pid" ]; then \
			echo "agents-token-refresh-install: warning: a refresh you started by hand (pid $$held) holds the lock of $$LIMA_INSTANCE, so the job waits, trying again every 5 minutes; stop it (kill $$held) and the job takes over" >&2; \
		fi; \
	fi; \
	echo "agents-token-refresh-install: check it with: sudo launchctl print system/$$label (state = running, and $$script in its arguments), and its log: tail $$log"

.PHONY: agents-token-refresh-uninstall
agents-token-refresh-uninstall: ## Stop and remove the agents App token refresh's LaunchDaemon of this VM instance; needs sudo (host clone, macOS)
	$(LIMA_INSTANCE_CHECK)
	@command -v launchctl >/dev/null 2>&1 || { echo "agents-token-refresh-uninstall: no launchctl: the LaunchDaemon is for the Mac, and there is none to remove here" >&2; exit 1; }; \
	$(AGENTS_TOKEN_LAUNCHD_DIR_CHECK); \
	label="$(AGENTS_TOKEN_LAUNCHD_LABEL_PREFIX)$$LIMA_INSTANCE"; dest="$${AGENTS_TOKEN_LAUNCHD_DIR%/}/$$label.plist"; \
	echo "+ sudo launchctl bootout system/$$label"; \
	if sudo launchctl bootout "system/$$label"; then booted=1; else booted=0; fi; \
	tries=0; \
	while [ "$$tries" -lt 10 ] && launchctl print "system/$$label" >/dev/null 2>&1; do sleep 1; tries=$$((tries + 1)); done; \
	if launchctl print "system/$$label" >/dev/null 2>&1; then \
		stuck=1; \
		echo "agents-token-refresh-uninstall: warning: $$label is still loaded $$tries second(s) after launchctl bootout: it keeps running until the next reboot or until you run sudo launchctl bootout system/$$label; removing its plist anyway, so the next boot does not load it" >&2; \
	elif [ "$$booted" = 1 ]; then \
		stuck=0; echo "agents-token-refresh-uninstall: stopped and unloaded $$label"; \
	else \
		stuck=0; echo "agents-token-refresh-uninstall: $$label was not loaded (launchctl bootout failed); removing its plist anyway"; \
	fi; \
	echo "+ sudo rm -f $$dest"; \
	sudo rm -f "$$dest" || exit 1; \
	echo "agents-token-refresh-uninstall: removed $$dest; the log stays. The VM's token stays valid until it expires, within the hour: start a refresh by hand with make agents-token-refresh-start"; \
	if [ "$$stuck" = 1 ]; then \
		echo "agents-token-refresh-uninstall: warning: $$label is still loaded: unload it with sudo launchctl bootout system/$$label" >&2; \
	fi

# For a VM created before dev/lima.yaml installed the gh wrapper: moves the real gh off the PATH, installs the
# wrapper with the real gh's path pinned and points the git credential helper at it, all over limactl's
# standard input (dev/vm-gh-wrapper.sh). The script is generated first and sent only when it is whole (its
# last line is the call that runs it), so a generator that fails fails make before the VM is touched. The VM's
# output is captured and printed, and make fails unless it holds the line "vm-gh-wrapper: done", so a
# connection that drops after the stream arrived (sh -s exits 0 having run nothing) is no success. Safe to
# run again.
.PHONY: vm-gh-wrapper
vm-gh-wrapper: ## Install the agents App's gh wrapper in a VM created before it (instead of make vm-delete vm-create)
	$(LIMA_INSTANCE_CHECK)
	@script="$$(sh dev/vm-gh-wrapper.sh dev/gh-app-token.sh)" || exit 1; \
	if [ "$$(printf '%s\n' "$$script" | tail -n 1)" != vm_gh_wrapper ]; then \
		echo "vm-gh-wrapper: dev/vm-gh-wrapper.sh printed no whole script (its last line is not vm_gh_wrapper); nothing sent to the VM" >&2; \
		exit 1; \
	fi; \
	out="$$(printf '%s\n' "$$script" | limactl shell --workdir / "$$LIMA_INSTANCE" sh -s)"; status=$$?; \
	if [ -n "$$out" ]; then printf '%s\n' "$$out"; fi; \
	if [ "$$status" -ne 0 ]; then exit "$$status"; fi; \
	if ! printf '%s\n' "$$out" | grep -Fqx 'vm-gh-wrapper: done'; then \
		echo "vm-gh-wrapper: the VM never printed vm-gh-wrapper: done, so the installation may not have finished; run make vm-gh-wrapper again" >&2; \
		exit 1; \
	fi

# deck opens the deck in the VM, at the workspace's checkout, and starts no token refresh: that is make
# agents-token-refresh-start's (or the LaunchDaemon's). The host clone's .envrc hands it the
# SHIFT2EU_AGENTS_APP_* variables; its limactl runs without them (VM_SHELL_ENV).
.PHONY: deck
deck: ## Open the dot-agent-deck TUI in the VM at WORKSPACE_REPO, attached to the daemon service (host clone)
	$(LIMA_INSTANCE_CHECK)
	$(VM_SHELL_ENV) limactl shell --workdir "$(HOST_WORKSPACE_REPO)" "$$LIMA_INSTANCE" dot-agent-deck

.PHONY: deck-restart
deck-restart: ## Reload the units and restart the deck's daemon service in the VM, e.g. after vm-env (stops running agents)
	$(LIMA_INSTANCE_CHECK)
	$(VM_SHELL_ENV) limactl shell --workdir / "$$LIMA_INSTANCE" systemctl --user daemon-reload
	$(VM_SHELL_ENV) limactl shell --workdir / "$$LIMA_INSTANCE" systemctl --user restart dot-agent-deck.service

.PHONY: vm-stop
vm-stop: ## Stop the VM
	$(LIMA_INSTANCE_CHECK)
	limactl stop "$$LIMA_INSTANCE"

.PHONY: vm-delete
vm-delete: ## Delete the VM and everything inside it
	$(LIMA_INSTANCE_CHECK)
	limactl delete --force "$$LIMA_INSTANCE"
