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

LIMA_INSTANCE ?= shift2eu
# The directory the VM mounts: this repository's parent, which must hold nothing but this repository and
# the worktrees dot-agent-deck's dispatcher creates beside it
# (docs/adr/0008-mount-a-workspace-directory-for-dispatcher-mode.md). Found from the main checkout, so it
# is the same when make runs in a worktree.
REPO_ROOT := $(patsubst %/.git,%,$(shell git rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo $(CURDIR)/.git))
WORKSPACE := $(abspath $(REPO_ROOT)/..)
REPO_NAME := $(notdir $(REPO_ROOT))
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
# refuses such a host), then once per release target with cgo off, and last runs the source rules in
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
	for target in $(RELEASE_TARGETS); do \
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

# Reads GitHub through gh, so run it where gh is logged in: from the host clone as the maintainer, to read the
# rulesets' bypass list, or in the VM, where the agents App's token cannot read it.
.PHONY: gate-check
gate-check: ## Check that Protect main and CODEOWNERS on main require the maintainer's approval (fails on any gap)
	dev/protect-main-check.sh

.PHONY: worktree
worktree: ## Create or resume the worktree for a PR: make worktree BRANCH=feat/12-short-name
	@./dev/worktree.sh "$(BRANCH)"

.PHONY: git-hooks
git-hooks: ## Enable .githooks, which refuse commits and pushes to main
	git config core.hooksPath .githooks

##@ Isolated environment (Lima)

# limactl start <name> on an instance whose directory has no lima.yaml (Lima lists it as Broken) reads the name
# as a template and falls back to template:default, a VM that mounts the whole home directory (#237). So a
# listed instance is started by name only when its status is known good (an allow list, not a deny list on
# Broken) and its lima.yaml is still there, and a listing or status query that fails stops vm-up (a failed
# listing would otherwise reach the create line). The listing is matched literally (grep -xF), so a name
# with a dot never matches a look-alike. A Broken instance with any entry named lima.yaml in place, even a
# dangling symlink, is broken for another reason and may keep an intact disk: vm-up points at limactl list
# and limactl start --debug, never at removing it by hand. A Stopped or Running instance whose lima.yaml is a
# dangling symlink is not started either, and gets the same pointer. Before any limactl call, vm-up refuses a
# LIMA_INSTANCE that is not letters, digits, '.', '_' and '-' starting with a letter or digit: an empty name
# matches the empty line an empty listing prints and would point at all of ~/.lima, a leading dash reads as an
# option and a slash as a path. The name is single-quoted for the shell, so a space or quote cannot break out.
# vm-up checks VM_CPUS, VM_MEMORY, VM_DISK and VM_SSH_PORT, then LIMA_INSTANCE, then that the workspace holds
# nothing stray, then the instance's status, all in one shell line with its limactl calls, so no make flag (-i,
# -k) can run limactl after any of these checks failed.
.PHONY: vm-up
vm-up: git-hooks ## Create or start the Lima VM with only this repository's workspace mounted (VM_CPUS, VM_MEMORY, VM_DISK, VM_SSH_PORT on creation)
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
	name='$(subst ','\'',$(LIMA_INSTANCE))'; \
	case "$$name" in \
	("" | [!A-Za-z0-9]* | *[!A-Za-z0-9._-]*) \
		printf "vm-up: invalid LIMA_INSTANCE '%s': a Lima instance name is letters, digits, '.', '_' and '-', starting with a letter or digit\n" "$$name" >&2; \
		exit 1;; \
	esac; \
	stray="$$(ls -A "$(WORKSPACE)" | grep -vx -e '$(REPO_NAME)' -e '$(REPO_NAME)-dispatch-.*' -e '\.DS_Store' || true)"; \
	if [ -n "$$stray" ]; then \
		echo "vm-up: the VM mounts $(WORKSPACE), which must hold only $(REPO_NAME) and its dispatch worktrees; move these out first:" >&2; \
		echo "$$stray" | sed 's/^/  /' >&2; \
		exit 1; \
	fi; \
	list="$$(limactl list --quiet)" || { \
		echo "vm-up: could not list the Lima instances, so it cannot tell whether $(LIMA_INSTANCE) exists" >&2; \
		exit 1; \
	}; \
	if printf '%s\n' "$$list" | grep -qxF -- "$(LIMA_INSTANCE)"; then \
		dir="$${LIMA_HOME:-$$HOME/.lima}/$(LIMA_INSTANCE)"; \
		status="$$(limactl list --format '{{.Status}}' "$(LIMA_INSTANCE)")" || { \
			echo "vm-up: could not read the status of the Lima instance $(LIMA_INSTANCE)" >&2; \
			exit 1; \
		}; \
		case "$$status" in \
		(Stopped|Running) \
			if [ -L "$$dir/lima.yaml" ] && [ ! -e "$$dir/lima.yaml" ]; then \
				echo "vm-up: the Lima instance $(LIMA_INSTANCE) is $$status, but $$dir/lima.yaml is a dangling symlink: it is there, but Lima cannot use it." >&2; \
				echo "  Its disk may be intact: see why with 'limactl list' and 'limactl start --debug $(LIMA_INSTANCE)', and fix the link instead of deleting the instance." >&2; \
				echo "  See Troubleshooting: a Broken instance, in docs/dev-environment.md." >&2; \
				exit 1; \
			elif [ ! -f "$$dir/lima.yaml" ]; then \
				echo "vm-up: the Lima instance $(LIMA_INSTANCE) is $$status, but $$dir has no lima.yaml:" >&2; \
				echo "  limactl start $(LIMA_INSTANCE) would read the name as a template and fall back to template:default, a VM that mounts the home directory." >&2; \
				echo "  See Troubleshooting: a Broken instance, in docs/dev-environment.md." >&2; \
				exit 1; \
			fi; \
			$(if $(VM_RESOURCES_SET),echo "vm-up: warning: $(VM_RESOURCES_SET) apply only when the VM is created; $(LIMA_INSTANCE) exists and starts as it is: an existing VM is never resized (stop it and resize it with limactl edit, or rebuild it with make vm-delete and make vm-create)" >&2;) \
			limactl start "$(LIMA_INSTANCE)";; \
		(Broken) \
			if [ -e "$$dir/lima.yaml" ] || [ -L "$$dir/lima.yaml" ]; then \
				echo "vm-up: the Lima instance $(LIMA_INSTANCE) is Broken although $$dir/lima.yaml is there: it is broken for another reason, such as a lima.yaml Lima cannot load." >&2; \
				echo "  Its disk may be intact: see why with 'limactl list' and 'limactl start --debug $(LIMA_INSTANCE)', and fix the cause instead of deleting the instance." >&2; \
				echo "  See Troubleshooting: a Broken instance, in docs/dev-environment.md." >&2; \
				exit 1; \
			fi; \
			echo "vm-up: the Lima instance $(LIMA_INSTANCE) is Broken: $$dir has no lima.yaml, and starting it would create a default VM that mounts the home directory" >&2; \
			echo "  Remove $$dir by hand once no limactl job uses it; make vm-delete cannot, as limactl delete --force refuses a directory without lima.yaml." >&2; \
			echo "  A launchd job running limactl start may recreate it through its log paths; look for one with" >&2; \
			echo "  'launchctl print system | grep -i lima' and in ~/Library/LaunchAgents and /Library/LaunchDaemons, and unload it first." >&2; \
			echo "  See Troubleshooting: a Broken instance, in docs/dev-environment.md." >&2; \
			exit 1;; \
		(*) \
			echo "vm-up: the Lima instance $(LIMA_INSTANCE) has the status '$$status'; vm-up starts only a Stopped or Running instance" >&2; \
			exit 1;; \
		esac; \
	else \
		limactl start --name="$(LIMA_INSTANCE)" --mount-only "$(WORKSPACE):w" --tty=false \
			$${VM_CPUS:+--cpus="$$VM_CPUS"} $${VM_MEMORY:+--memory="$${VM_MEMORY%GiB}"} \
			$${VM_DISK:+--disk="$${VM_DISK%GiB}"} $${VM_SSH_PORT:+--ssh-port="$$VM_SSH_PORT"} dev/lima.yaml; \
	fi

.PHONY: vm-shell
vm-shell: ## Open a shell in the VM at the repository root
	limactl shell --workdir "$(CURDIR)" "$(LIMA_INSTANCE)"

# The names in VM_ENV, one per line: commas and spaces separate them, and a name listed twice is listed once.
# The recipe reads VM_ENV from its environment ("$$VM_ENV"), never pasted into its own text, and refuses any
# character but those of names, commas and spaces before it splits the list (with globbing off), so no quote,
# `$` or glob in VM_ENV ever reaches a shell as code.
vm_env_names = $$(printf '%s\n' "$$VM_ENV" | tr ', ' '\n\n' | awk 'NF && !seen[$$0]++')
# How a message names the entry $$name at 1-based position $$i of that list, in $$label (issue #268 L1): the
# entry itself only when it is an upper-case variable name (^[A-Z_][A-Z0-9_]*$), the shape of every credential
# name; anything else may be a credential pasted where a name goes, so only its position is given. The letters
# are spelled out, as a range such as [A-Z] can match lower-case letters in some shells' locales.
vm_env_upper = ABCDEFGHIJKLMNOPQRSTUVWXYZ
vm_env_label = case "$$name" in ([!$(vm_env_upper)_]* | *[!$(vm_env_upper)0123456789_]*) \
		label="VM_ENV entry $$i (entry $$i is not shown, as it may be a credential typed by mistake)";; \
	(*) label="$$name";; esac
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
		i=$$((i + 1)); $(vm_env_label); \
		case "$$name" in (SHIFT2EU_AGENTS_APP_*) \
			echo "vm-env: refusing $$label: the agents App's key and its path never enter the VM; make agents-token writes its tokens there" >&2; \
			exit 1;; esac; \
		case "$$name" in ($(github_token_names)) \
			echo "vm-env: refusing $$name: a GitHub token never enters the VM; the agents' only GitHub credential is the agents App's token, which make agents-token writes there (its value is not shown)" >&2; \
			exit 1;; esac; \
		case "$$name" in ([!A-Za-z_]* | *[!A-Za-z0-9_]*) \
			echo "vm-env: refusing $$label: not a variable name" >&2; \
			exit 1;; esac; \
	done
	@set -f; missing=''; i=0; \
	for name in $(vm_env_names); do \
		i=$$((i + 1)); $(vm_env_label); \
		[ -n "$$(printenv "$$name")" ] || missing="$$missing $$label"; \
	done; \
	if [ -n "$$missing" ]; then \
		echo "vm-env: not set or empty:$$missing. Nothing was written: the VM's daemon.env is unchanged." >&2; \
		echo "  1. If your credentials come from the macOS login Keychain, an SSH session has it locked: run" >&2; \
		echo "     security unlock-keychain ~/Library/Keychains/login.keychain-db (it asks for the account password)." >&2; \
		echo "  2. Run cd ~ && cd $(REPO_ROOT) so direnv reads the .envrc again. On any host, check that the" >&2; \
		echo "     .envrc exists, sets them and is allowed (direnv allow), or export the variables yourself." >&2; \
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
# file or is a symlink (the move would write into a directory or replace the link; issue #259), and one that
# exists but cannot be read, as it could not check it for a GitHub token; the token read fails on sed's own
# status (not a pipeline's), and a merge's greps accept only status 1 (no match), so a read error is never taken
# for an empty file. Their status-2 branch is defence in depth: the -r pre-check refuses an unreadable file
# first, so it is reached only if the file turns unreadable between the two, a race no fake reaches
# deterministically. The write step refuses an unset or empty credential itself, writing nothing, so make -i,
# which runs it although vm-env-check failed, cannot wipe the VM's value (issue #268 L2); its messages name an
# entry the way vm-env-check does (vm_env_label).
.PHONY: vm-env
vm-env: export VM_ENV := $(value VM_ENV)
vm-env: vm-env-check ## Copy the VM_ENV credentials from your shell into the VM (refuses if one is missing)
	@set -f; env_file="$$(i=0; for name in $(vm_env_names); do \
		i=$$((i + 1)); $(vm_env_label); \
		value="$$(printenv "$$name")"; \
		[ -n "$$value" ] || { echo "vm-env: $$label is not set or empty. Nothing was written: the VM's daemon.env is unchanged." >&2; exit 1; }; \
		case "$$value" in (*[!A-Za-z0-9._~+/=-]*) echo "vm-env: $$label has unsupported characters" >&2; exit 1;; esac; \
		printf '%s=%s\n' "$$name" "$$value"; \
	done)" && out="$$(printf '%s\n' "$$env_file" | limactl shell --workdir / "$(LIMA_INSTANCE)" \
		sh -c 'umask 077 && LC_ALL=C && export LC_ALL && d="$$HOME/.config/dot-agent-deck" && f="$$d/daemon.env" && \
		t= && trap "rm -f \"\$$t\"" EXIT && trap "exit 1" HUP INT TERM && \
		unreadable() { echo "vm-env: cannot read ~/.config/dot-agent-deck/daemon.env in the VM, so it cannot check the file for a GitHub token to warn about (nor a merge keep its lines). Nothing was written: make it readable by the VM user (chmod 600) and run make vm-env again." >&2; exit 1; } && \
		mkdir -p "$$d" && if [ -L "$$f" ] || { [ -e "$$f" ] && ! [ -f "$$f" ]; }; then \
			echo "vm-env: ~/.config/dot-agent-deck/daemon.env in the VM is not a regular file (a directory or a symlink), so moving the new file into place would write into it or replace it. Nothing was written: make it a regular file, or remove it, and run make vm-env again." >&2; exit 1; fi && \
		if [ -e "$$f" ] && ! [ -r "$$f" ]; then unreadable; fi && \
		find "$$d" -maxdepth 1 -type f -name ".daemon.env.??????" -mmin +1 -exec rm -f {} + && \
		new="$$(cat)" && safe="$(vm_env_safe_line)" && \
		names="$$(printf "%s\n" "$$new" | sed -n "s/=.*//p" | paste -sd "|" -)" && \
		removed= && if [ -f "$$f" ]; then \
			found=$$(sed -n -E "s/^(.*[^A-Za-z0-9_])?($(github_token_names))[[:space:]]*=.*/\2/p" "$$f") || unreadable; \
			removed=$$(printf "%s\n" "$$found" | sort -u | paste -sd " " -) || exit 1; fi && \
		dropped=0 && kept= && if [ "$$1" = merge ] && [ -f "$$f" ]; then \
			: "grep status 2 (unreadable) is defence in depth: the -r pre-check above refuses an unreadable daemon.env, so it is reached only if the file turns unreadable in between, a race no test fakes deterministically"; \
			total=$$(grep -a -c . "$$f") || [ "$$?" -eq 1 ] || unreadable; \
			plain=$$(grep -a -E "$$safe" "$$f") || [ "$$?" -eq 1 ] || unreadable; \
			nplain=$$(printf "%s" "$$plain" | grep -a -c .) || [ "$$?" -eq 1 ] || exit 1; \
			dropped=$$(( total - nplain )); \
			kept=$$(printf "%s\n" "$$plain" | grep -a -v -E "^($$names|$(github_token_names))=") || [ "$$?" -eq 1 ] || exit 1; fi && \
		t="$$(mktemp "$$d/.daemon.env.XXXXXX")" && \
		{ if [ -n "$$kept" ]; then printf "%s\n" "$$kept"; fi && printf "%s\n" "$$new"; } > "$$t" && \
		chmod 600 "$$t" && mv "$$t" "$$f" && t= && trap - EXIT HUP INT TERM && \
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
	limactl shell --workdir "$(CURDIR)" "$(LIMA_INSTANCE)" dev/claude-settings.sh "$(WORKSPACE)"

# A login shell (bash -l), because only a login shell loads the credentials vm-env wrote
# (/etc/profile.d/shift2eu.sh). Safe to run again: it changes only what is not set up yet, and the theme
# when CLAUDE_THEME differs. Run it before make deck (docs/dev-environment.md, What make vm-init does).
.PHONY: vm-init
vm-init: ## Once-only VM setup after vm-env and vm-claude-settings: Claude Code first run, git identity, credential checks, deck command, daemon service
	limactl shell --workdir "$(CURDIR)" "$(LIMA_INSTANCE)" bash -l dev/vm-init.sh \
		"$(WORKSPACE)" "$(REPO_ROOT)" "$(GIT_USER_NAME)" "$(GIT_USER_EMAIL)" "$(CLAUDE_THEME)"

# One target at a time, in order, even under make -j. vm-env-check runs first, before vm-up and any limactl
# call, so a missing credential or a VM_ENV vm-env would refuse creates nothing (issue #258). Each sub-make
# gets a command-line VM_ENV the same way (through MAKEFLAGS), so it is checked as vm-env then reads it.
# When the check refuses, vm-create says on stderr that it created nothing, after vm-env's own messages.
.PHONY: vm-create
vm-create: ## Create and set up the VM, ready for make deck: vm-env-check, vm-up (VM_CPUS, ... pass through), vm-env, vm-claude-settings, vm-init
	@$(MAKE) vm-env-check || { echo "vm-create: nothing was created: the credential check runs before the VM is created" >&2; exit 1; }
	$(MAKE) vm-up
	$(MAKE) vm-env
	$(MAKE) vm-claude-settings
	$(MAKE) vm-init

# Where make agents-token-refresh-start keeps the pid and the log of the agents App token refresh, and the
# refresh its lock, one set per VM instance: on the Mac, outside the workspace (ADR 0031).
AGENTS_TOKEN_STATE = $${XDG_STATE_HOME:-$${HOME:?set HOME}/.local/state}/shift2eu

# The token targets run on the Mac with the App's key in reach, so run them only from the host clone: a clone
# outside the workspace, updated only with git pull --ff-only from GitHub, whose own direnv .envrc holds the
# three SHIFT2EU_AGENTS_APP_* variables (docs/dev-environment.md, The host clone). No check run in the mounted
# workspace can vouch for it: the agents can edit its Makefile, dev/ and .git/config, and git and make run
# what those say. Nor does the host clone put the key out of their reach: any target run from the workspace
# runs as you, can read the key file and can tamper with the host clone (ADR 0031, Consequences). The token
# targets need only limactl and LIMA_INSTANCE, never WORKSPACE or REPO_ROOT.

.PHONY: agents-token
agents-token: ## Mint one agents App installation token on the Mac and write it, and the bot identity, into the VM (host clone)
	@LIMA_INSTANCE="$(LIMA_INSTANCE)" sh dev/agents-app-token.sh mint

.PHONY: agents-token-refresh
agents-token-refresh: ## Mint the agents App token now and again before each one expires, in the foreground, until stopped (host clone)
	@LIMA_INSTANCE="$(LIMA_INSTANCE)" sh dev/agents-app-token.sh refresh

# Starts the refresh of this VM instance in the background unless one already runs: the one whose pid the
# instance's lock holds (a refresh started by hand with make agents-token-refresh holds it too), or the one
# this target started earlier. It warns when that one has stopped, and prints where the log is. When launchd
# has the instance's LaunchDaemon loaded (agents-token-refresh-install), it warns and starts nothing, writing
# no file: the job is the instance's refresher. Without launchctl (Linux) that check is skipped. Like install
# and uninstall below, it reads LIMA_INSTANCE from its environment ($$LIMA_INSTANCE, exported with theirs) and
# refuses an unsupported name before any use: the name becomes part of its file names and of the job's label.
.PHONY: agents-token-refresh-start
agents-token-refresh-start: ## Start the agents App token refresh in the background, one per VM instance, and print where its log is (host clone)
	@if [ -z "$${SHIFT2EU_AGENTS_APP_ID:-}" ] || [ -z "$${SHIFT2EU_AGENTS_APP_INSTALLATION_ID:-}" ] || [ -z "$${SHIFT2EU_AGENTS_APP_KEY_FILE:-}" ]; then \
		echo "agents-token-refresh-start: set SHIFT2EU_AGENTS_APP_ID, SHIFT2EU_AGENTS_APP_INSTALLATION_ID and SHIFT2EU_AGENTS_APP_KEY_FILE in the host clone's .envrc (docs/dev-environment.md, The host clone)" >&2; \
		exit 1; \
	fi; \
	$(AGENTS_TOKEN_INSTANCE_CHECK); \
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
		echo "agents-token-refresh-start: warning: the agents App token refresh of $$LIMA_INSTANCE (pid $$started) had stopped; its log's last lines:" >&2; \
		tail -n 3 "$$log" 2>/dev/null | sed 's/^/  /' >&2 || true; \
	fi; \
	nohup sh dev/agents-app-token.sh refresh </dev/null >>"$$log" 2>&1 & \
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
# install and uninstall read LIMA_INSTANCE from their environment ($$LIMA_INSTANCE, exported below) and check
# it before any use, so make never splices an unchecked value into the shell text that runs sudo.
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
# The check of LIMA_INSTANCE, the same rule as dev/agents-app-token.sh's: it becomes part of the job's label.
# Spelled out, not A-Z: some shells match a range by the locale's collation.
AGENTS_TOKEN_INSTANCE_CHECK = case "$${LIMA_INSTANCE:-}" in \
	'' | [!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789]* | *[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-]*) \
		echo "$@: LIMA_INSTANCE must be letters, digits, ., _ and -, beginning with a letter or digit" >&2; exit 1 ;; \
	esac
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

agents-token-refresh-start agents-token-refresh-install agents-token-refresh-uninstall: export LIMA_INSTANCE := $(LIMA_INSTANCE)
agents-token-refresh-install agents-token-refresh-uninstall: export AGENTS_TOKEN_LAUNCHD_DIR := $(AGENTS_TOKEN_LAUNCHD_DIR)

.PHONY: agents-token-refresh-install
agents-token-refresh-install: ## Install (or update) the LaunchDaemon that runs the agents App token refresh at boot, one per VM instance; needs sudo (host clone, macOS)
	@if [ "$$(id -u)" = 0 ]; then \
		echo "agents-token-refresh-install: refused: run it as the maintainer, not as root (no sudo make); it runs sudo itself. The job runs as the account that installs it, and as root it would run the host clone's script, which the maintainer can edit, at every boot" >&2; \
		exit 1; \
	fi; \
	if [ -z "$${SHIFT2EU_AGENTS_APP_ID:-}" ] || [ -z "$${SHIFT2EU_AGENTS_APP_INSTALLATION_ID:-}" ] || [ -z "$${SHIFT2EU_AGENTS_APP_KEY_FILE:-}" ]; then \
		echo "agents-token-refresh-install: set SHIFT2EU_AGENTS_APP_ID, SHIFT2EU_AGENTS_APP_INSTALLATION_ID and SHIFT2EU_AGENTS_APP_KEY_FILE in the host clone's .envrc (docs/dev-environment.md, The host clone)" >&2; \
		exit 1; \
	fi; \
	command -v launchctl >/dev/null 2>&1 || { echo "agents-token-refresh-install: no launchctl: the LaunchDaemon is for the Mac; elsewhere use make agents-token-refresh-start" >&2; exit 1; }; \
	$(AGENTS_TOKEN_INSTANCE_CHECK); \
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
	sh dev/agents-token-launchd.sh plist >"$$tmp" || exit 1; \
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
	@command -v launchctl >/dev/null 2>&1 || { echo "agents-token-refresh-uninstall: no launchctl: the LaunchDaemon is for the Mac, and there is none to remove here" >&2; exit 1; }; \
	$(AGENTS_TOKEN_INSTANCE_CHECK); \
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
	@script="$$(sh dev/vm-gh-wrapper.sh dev/gh-app-token.sh)" || exit 1; \
	if [ "$$(printf '%s\n' "$$script" | tail -n 1)" != vm_gh_wrapper ]; then \
		echo "vm-gh-wrapper: dev/vm-gh-wrapper.sh printed no whole script (its last line is not vm_gh_wrapper); nothing sent to the VM" >&2; \
		exit 1; \
	fi; \
	out="$$(printf '%s\n' "$$script" | limactl shell --workdir / "$(LIMA_INSTANCE)" sh -s)"; status=$$?; \
	if [ -n "$$out" ]; then printf '%s\n' "$$out"; fi; \
	if [ "$$status" -ne 0 ]; then exit "$$status"; fi; \
	if ! printf '%s\n' "$$out" | grep -Fqx 'vm-gh-wrapper: done'; then \
		echo "vm-gh-wrapper: the VM never printed vm-gh-wrapper: done, so the installation may not have finished; run make vm-gh-wrapper again" >&2; \
		exit 1; \
	fi

# deck opens the deck and starts no token refresh: that is make agents-token-refresh-start's, in the host
# clone. When a SHIFT2EU_AGENTS_APP_* variable reaches it, it warns that the variable is visible in the
# workspace's shell, and still opens the deck.
.PHONY: deck
deck: ## Open the dot-agent-deck TUI in the VM (attaches to the daemon service)
	@if [ -n "$${SHIFT2EU_AGENTS_APP_ID:-}$${SHIFT2EU_AGENTS_APP_INSTALLATION_ID:-}$${SHIFT2EU_AGENTS_APP_KEY_FILE:-}" ]; then \
		echo "deck: warning: a SHIFT2EU_AGENTS_APP_* variable is set in this shell, where the workspace's code runs: move the three into the direnv .envrc of your host clone, outside the workspace, never into the .envrc above it, and run make agents-token-refresh-start there (docs/dev-environment.md, The host clone)" >&2; \
	fi
	limactl shell --workdir "$(CURDIR)" "$(LIMA_INSTANCE)" dot-agent-deck

.PHONY: deck-restart
deck-restart: ## Reload the units and restart the deck's daemon service in the VM, e.g. after vm-env (stops running agents)
	limactl shell --workdir / "$(LIMA_INSTANCE)" systemctl --user daemon-reload
	limactl shell --workdir / "$(LIMA_INSTANCE)" systemctl --user restart dot-agent-deck.service

.PHONY: vm-stop
vm-stop: ## Stop the VM
	limactl stop "$(LIMA_INSTANCE)"

.PHONY: vm-delete
vm-delete: ## Delete the VM and everything inside it
	limactl delete --force "$(LIMA_INSTANCE)"
