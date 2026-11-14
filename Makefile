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
# the Mac and only its one-hour tokens enter the VM (make agents-token, PRD #161, ADR 0029).
VM_ENV ?= CLAUDE_CODE_OAUTH_TOKEN,DEEPSEEK_API_KEY,GITHUB_TOKEN
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

.PHONY: worktree
worktree: ## Create or resume the worktree for a PR: make worktree BRANCH=feat/12-short-name
	@./dev/worktree.sh "$(BRANCH)"

.PHONY: git-hooks
git-hooks: ## Enable .githooks, which refuse commits and pushes to main
	git config core.hooksPath .githooks

##@ Isolated environment (Lima)

.PHONY: vm-up
vm-up: git-hooks ## Create or start the Lima VM with only this repository's workspace directory mounted
	@stray="$$(ls -A "$(WORKSPACE)" | grep -vx -e '$(REPO_NAME)' -e '$(REPO_NAME)-dispatch-.*' -e '\.DS_Store' || true)"; \
	if [ -n "$$stray" ]; then \
		echo "vm-up: the VM mounts $(WORKSPACE), which must hold only $(REPO_NAME) and its dispatch worktrees; move these out first:" >&2; \
		echo "$$stray" | sed 's/^/  /' >&2; \
		exit 1; \
	fi
	@if limactl list --quiet | grep -qx "$(LIMA_INSTANCE)"; then \
		limactl start "$(LIMA_INSTANCE)"; \
	else \
		limactl start --name="$(LIMA_INSTANCE)" --mount-only "$(WORKSPACE):w" --tty=false dev/lima.yaml; \
	fi

.PHONY: vm-shell
vm-shell: ## Open a shell in the VM at the repository root
	limactl shell --workdir "$(CURDIR)" "$(LIMA_INSTANCE)"

.PHONY: vm-env
vm-env: ## Copy the VM_ENV credentials from your shell into the VM
	@for name in $$(echo '$(VM_ENV)' | tr ',' ' '); do \
		case "$$name" in (SHIFT2EU_AGENTS_APP_*) \
			echo "vm-env: refusing $$name: the agents App's key and its path never enter the VM; make agents-token writes its tokens there" >&2; \
			exit 1;; esac; \
	done
	@env_file="$$(for name in $$(echo '$(VM_ENV)' | tr ',' ' '); do \
		value="$$(printenv "$$name")"; \
		[ -n "$$value" ] || { echo "vm-env: $$name is not set, skipped" >&2; continue; }; \
		case "$$value" in (*[!A-Za-z0-9._~+/=-]*) echo "vm-env: $$name has unsupported characters" >&2; exit 1;; esac; \
		printf '%s=%s\n' "$$name" "$$value"; \
	done)" && printf '%s\n' "$$env_file" | limactl shell --workdir / "$(LIMA_INSTANCE)" \
		sh -c 'umask 077 && mkdir -p ~/.config/dot-agent-deck && cat > ~/.config/dot-agent-deck/daemon.env && chmod 600 ~/.config/dot-agent-deck/daemon.env' >/dev/null
	@echo "vm-env: written to ~/.config/dot-agent-deck/daemon.env in the VM; 'make deck-restart' applies it to a running deck"

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

# One target at a time, in order, even under make -j.
.PHONY: vm-create
vm-create: ## Create and set up the VM, ready for make deck: vm-up, vm-env, vm-claude-settings, vm-init
	$(MAKE) vm-up
	$(MAKE) vm-env
	$(MAKE) vm-claude-settings
	$(MAKE) vm-init

# Where make agents-token-refresh-start keeps the pid and the log of the agents App token refresh, and the
# refresh its lock, one set per VM instance: on the Mac, outside the workspace (ADR 0029).
AGENTS_TOKEN_STATE = $${XDG_STATE_HOME:-$${HOME:?set HOME}/.local/state}/shift2eu

# The token targets run on the Mac with the App's key in reach, so run them only from the host clone: a clone
# outside the workspace, updated only with git pull --ff-only from GitHub, whose own direnv .envrc holds the
# three SHIFT2EU_AGENTS_APP_* variables (docs/dev-environment.md, The host clone). No check run in the mounted
# workspace can vouch for it: the agents can edit its Makefile, dev/ and .git/config, and git and make run
# what those say. The token targets need only limactl and LIMA_INSTANCE, never WORKSPACE or REPO_ROOT.

.PHONY: agents-token
agents-token: ## Mint one agents App installation token on the Mac and write it, and the bot identity, into the VM (host clone)
	@LIMA_INSTANCE="$(LIMA_INSTANCE)" sh dev/agents-app-token.sh mint

.PHONY: agents-token-refresh
agents-token-refresh: ## Mint the agents App token now and again before each one expires, in the foreground, until stopped (host clone)
	@LIMA_INSTANCE="$(LIMA_INSTANCE)" sh dev/agents-app-token.sh refresh

# Starts the refresh of this VM instance in the background unless one already runs: the one whose pid the
# instance's lock holds (a refresh started by hand with make agents-token-refresh holds it too), or the one
# this target started earlier. It warns when that one has stopped, and prints where the log is.
.PHONY: agents-token-refresh-start
agents-token-refresh-start: ## Start the agents App token refresh in the background, one per VM instance, and print where its log is (host clone)
	@if [ -z "$${SHIFT2EU_AGENTS_APP_ID:-}" ] || [ -z "$${SHIFT2EU_AGENTS_APP_INSTALLATION_ID:-}" ] || [ -z "$${SHIFT2EU_AGENTS_APP_KEY_FILE:-}" ]; then \
		echo "agents-token-refresh-start: set SHIFT2EU_AGENTS_APP_ID, SHIFT2EU_AGENTS_APP_INSTALLATION_ID and SHIFT2EU_AGENTS_APP_KEY_FILE in the host clone's .envrc (docs/dev-environment.md, The host clone)" >&2; \
		exit 1; \
	fi; \
	state="$(AGENTS_TOKEN_STATE)"; pid_file="$$state/agents-token-refresh-$(LIMA_INSTANCE).pid"; log="$$state/agents-token-refresh-$(LIMA_INSTANCE).log"; \
	lock_pid="$$state/agents-token-refresh-$(LIMA_INSTANCE).lock/pid"; \
	(umask 077 && mkdir -p "$$state" && : >>"$$log") || exit 1; \
	started="$$(cat "$$pid_file" 2>/dev/null || true)"; \
	for pid in "$$(cat "$$lock_pid" 2>/dev/null || true)" "$$started"; do \
		case "$$pid" in ''|*[!0-9]*) continue ;; esac; \
		if kill -0 "$$pid" 2>/dev/null && ps -p "$$pid" -o command= 2>/dev/null | grep -q 'agents-app-token.sh refresh'; then \
			echo "agents-token-refresh-start: the agents App token refresh of $(LIMA_INSTANCE) is already running (pid $$pid); its log is $$log"; \
			exit 0; \
		fi; \
	done; \
	if [ -n "$$started" ]; then \
		echo "agents-token-refresh-start: warning: the agents App token refresh of $(LIMA_INSTANCE) (pid $$started) had stopped; its log's last lines:" >&2; \
		tail -n 3 "$$log" 2>/dev/null | sed 's/^/  /' >&2 || true; \
	fi; \
	LIMA_INSTANCE="$(LIMA_INSTANCE)" nohup sh dev/agents-app-token.sh refresh </dev/null >>"$$log" 2>&1 & \
	pid="$$!"; \
	(umask 077 && echo "$$pid" >"$$pid_file") || exit 1; \
	echo "agents-token-refresh-start: started the agents App token refresh of $(LIMA_INSTANCE) (pid $$pid); its log is $$log"

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
deck-restart: ## Restart the deck's daemon service in the VM, e.g. after vm-env (stops running agents)
	limactl shell --workdir / "$(LIMA_INSTANCE)" systemctl --user restart dot-agent-deck.service

.PHONY: vm-stop
vm-stop: ## Stop the VM
	limactl stop "$(LIMA_INSTANCE)"

.PHONY: vm-delete
vm-delete: ## Delete the VM and everything inside it
	limactl delete --force "$(LIMA_INSTANCE)"
