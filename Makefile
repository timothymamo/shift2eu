# This Makefile's own directory, with a trailing slash (issue #347): the last word of MAKEFILE_LIST before any
# include is this file, however make found it (make, make -C <dir>, make -f <dir>/Makefile from elsewhere, or
# with MAKEFILES set). Keep it the first line, ahead of any include. It is make code, no $(shell ...), so it
# may come before the block below. Make splits words on spaces, so a path with a space in it yields a wrong
# directory: its go.mod is then not found and GO_LINE below is empty, as without a go.mod.
MAKEFILE_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

# No `#` comment, whole-line or mid-line, anywhere in a backslash-continued recipe command: GNU Make 3.81 (macOS
# /usr/bin/make) then comments out the rest of the command and the recipe ends silently with exit 0. Put comments
# above the target (TestMakefileHasNoCommentInsideAContinuedRecipe, issue #437).

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

# The release tag make site builds the site's pages for (PRD #297), empty for an untagged build. Read raw and
# exported like the VM_* above, and for the same reason in this first block: the recipe reads it as
# "$$SITE_VERSION", so neither make nor a shell parses the value, and the builder refuses any value that is no
# vX.Y.Z tag.
SITE_VERSION ?=
override SITE_VERSION := $(value SITE_VERSION)
export SITE_VERSION

# The Go toolchain every go command make runs builds with (issue #347, docs/adr/0035): go.mod's exact go line
# is the pin, and GOTOOLCHAIN=go<go line> makes the go command run exactly that release, the one on PATH when
# it is that release, never a newer local Go (a go line alone admits any newer one). When the local Go is not
# the pin, the go command fetches it once as the module golang.org/toolchain through GOPROXY into the module
# cache, so make can then touch the network. That fetch is verified against the checksum database
# (sum.golang.org) and is only as trustworthy as your GOSUMDB settings: GOSUMDB=off, or a GONOSUMDB or
# GOPRIVATE pattern matching golang.org, turns the check off. Normally nothing is fetched: the VM installs the
# pinned Go from its checksum-verified tarball (dev/lima.yaml), and CI and the release run with
# GOTOOLCHAIN=local, which never downloads. Exported, so it reaches every recipe, sub-make, go -C tools, the scripts under dev/
# and the go commands ci/sitebuild runs, whose go env GOROOT then names the pinned toolchain, the one
# wasm_exec.js is copied from. It is read from go.mod in this one place, so a Renovate bump of the go line
# needs no change here. The one value of yours that is kept is local, as CI sets it (actions/setup-go exports
# it, after installing the go line's release): it runs the Go on PATH and never downloads one, and a Go other
# than the go line's then fails TestGoModPinsTheBuildingToolchain in make test rather than building with it.
# Any other value, from the environment or the command line (auto, path, another release, <name>+auto), is
# replaced, since every one of them can run a Go other than the pinned one. The value is read raw, so make
# expands nothing in it, and fixed in this first block, before any $(shell ...) is handed it; the go line is
# set below, once $(shell ...) may run. Without a go.mod (a test's copy of the Makefile) nothing is pinned
# and GOTOOLCHAIN is exported empty, which the go command reads as unset.
ifeq ($(value GOTOOLCHAIN),local)
override GOTOOLCHAIN := local
else
override GOTOOLCHAIN :=
endif
export GOTOOLCHAIN

BINARY  := shift2eu
# go.mod's go line, the version GOTOOLCHAIN above pins: the second field of the first line that starts with
# the go directive, as the go command writes it, less a trailing carriage return (a go.mod with CRLF line
# endings would otherwise make GOTOOLCHAIN invalid and fail every go command). It is the go.mod beside this
# Makefile (MAKEFILE_DIR, at the top), not one in make's working directory, so make -f <root>/Makefile run
# from elsewhere pins the same release; the path is single-quoted for the shell.
GO_LINE := $(shell awk '/^go[ \t]/ { sub(/\r$$/, "", $$2); print $$2; exit }' '$(MAKEFILE_DIR)go.mod' 2>/dev/null)
ifneq ($(GOTOOLCHAIN),local)
override GOTOOLCHAIN := $(if $(GO_LINE),go$(GO_LINE))
endif
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
# The container CLI make image runs: docker where it is installed, otherwise nerdctl, the rootless runtime
# of the Lima VM (docs/adr/0017-run-a-rootless-container-runtime-in-the-lima-vm.md).
DOCKER  ?= $(if $(shell command -v docker 2>/dev/null),docker,nerdctl)
# The headless Chromium make test-browser runs the site's smoke test in (PRD #297 milestone 2, docs/adr/0036),
# pinned by tag and digest; Renovate updates both through the comment above it. CI's browser job runs make
# test-browser too, so this is the one copy of the pin.
# renovate: datasource=docker depName=chromedp/headless-shell
HEADLESS_SHELL := chromedp/headless-shell:160.0.8157.0@sha256:1459842ec97ea88a20645fffae40769c0c8dc70f51cf43e7df812fbaef9909cc
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
	agents-token-refresh-uninstall deck deck-restart gate-check vm-agents-token-ssh vm-agents-token-ssh-uninstall \
	vm-boot-install vm-boot-uninstall vm-claude-settings vm-create vm-delete vm-env vm-env-check vm-gh-wrapper vm-init vm-shell vm-stop vm-up
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
# The .envrc guard (issue #402). The host targets that load the VM's credentials or the agents App's key -
# vm-env, its check vm-env-check, the three that run them (vm-up, vm-create, vm-init), the two that mint the
# agents App's token in the foreground (agents-token, agents-token-refresh) and the two that start or install its
# refresh - refuse a layout where direnv reads the credentials from anywhere but the host clone's own .envrc: a
# host clone .envrc that is a symlink to a file outside the clone or hands its lines to another file, or a .envrc
# or .env in the workspace or above it - both spellings of it, as WORKSPACE_REPO writes it and with its symlinks
# resolved - which direnv loads in every Mac shell opened in the workspace (found on the Mac Mini on 2026-11-20).
# The host clone's .envrc is held strictly, as the guard parses no shell: source_up, source_up_if_exists,
# source_env, source_env_if_exists, source_url and bash's source and . are refused whatever they name, a file of
# the clone included, as they run a file the guard never reads; dotenv and dotenv_if_exists pass only bare (the
# clone's own .env) or with an argument that, one enclosing pair of matching quotes stripped, holds nothing but
# A-Za-z0-9._/+@=- and resolves inside the clone (a directory standing for its .env); anything else counts as
# outside. ;, &, |, ( and ) separate directives (each doubled before matching, so no match eats the boundary of
# the next), <, > and a backtick end a directive's word as well (source_up>/dev/null, `source_up`), and a
# dotenv directly followed by a quote, a backslash, <, > or a backtick, or with an argument starting with one,
# counts as outside. $(call
# ENVRC_GUARD,<target>) checks both and names each file on stderr, with the line number, the directive's name and
# paths only, never a line or a value: of the host clone's .envrc it reads, through grep -a -o, only the words of
# ENVRC_GUARD_ERE on lines that are no comment (a line ending in \ joined to the next first, as bash does), with
# the one word after each; of the files above the workspace nothing, it only tests that they are there. A
# relative path is resolved against the clone, component by component, every symlink followed, the last one too,
# at most 40 (macOS: /var is /private/var, as in REPO_ROOT); a loop of symlinks counts as outside. A directive in
# a trailing comment counts too, a safe false positive. It checks the usual direnv and bash spellings and is no
# sandbox (eval can always read a parent). It is a macro, not a script of dev/, as it runs while make reads this
# file, like the refusal above:
# before any prerequisite (vm-env's vm-env-check), sub-make, $(LIMA_INSTANCE_CHECK) or recipe line, even under
# make -i or -k; a sub-make (vm-create's) checks again. It prints "refused" on stdout when it refuses, and make
# then stops with $(error). It refuses, never warns, and has no override: the fix is moving a file
# (docs/dev-environment.md, The host clone). vm-boot-install is not one of them: its LaunchDaemon starts the VM
# and holds no credential (its plist names only limactl, LIMA_INSTANCE and LIMA_HOME).
ENVRC_GUARD_TARGETS := agents-token agents-token-refresh agents-token-refresh-install agents-token-refresh-start \
	vm-create vm-env vm-env-check vm-init vm-up
envrc_guard_goals := $(filter $(ENVRC_GUARD_TARGETS),$(MAKECMDGOALS))
host_sq = '$(subst ','\'',$(1))'
# A directive the guard reads: the word, alone (no letter, digit, _ or . before it, so a backtick may; a space, a
# separator, a quote, a backslash, <, >, a backtick or the end after it), with the one word after it, which stops
# at a space, a separator, < or > after its first character. Its first character may be <, > or a backtick, so
# dotenv's argument then fails the allow list; a backtick later in it does not stop it, as bash joins what a
# command substitution prints to the word, so dotenv .env`...` fails the allow list too.
# docs/dev-environment.md, The host clone, counts the lines with the same words and boundaries.
ENVRC_GUARD_ERE := (^|[^A-Za-z0-9_.])(source_up_if_exists|source_up|source_env_if_exists|source_env|source_url|dotenv_if_exists|dotenv|source|\.)([[:space:]]+[^[:space:];&|()][^[:space:];&|()<>]*|[[:space:];&|()<>`\"'\'']|$$)
ENVRC_GUARD = LC_ALL=C; export LC_ALL; set -f; \
	target=$(call host_sq,$(1)); clone=$(call host_sq,$(REPO_ROOT)); dir=$(call host_sq,$(HOST_WORKSPACE)); \
	why="every line of the VM's credentials and the agents App's variables belongs in the host clone's .envrc itself (docs/dev-environment.md, The host clone)"; \
	dir_real=$(call host_sq,$(host_workspace_real)); \
	refused=; nl=$$(printf '\n_'); nl=$${nl%_}; \
	resolve() { out=; rest=$$1; hops=0; \
		while :; do \
			while :; do case $$rest in (/*) rest=$${rest$(host_hash)/} ;; (*) break ;; esac; done; \
			[ -n "$$rest" ] || break; \
			part=$${rest%%/*}; rest=$${rest$(host_hash)"$$part"}; \
			case $$part in \
			(.) ;; \
			(..) out=$${out%/*} ;; \
			(*) if [ -L "$$out/$$part" ]; then \
					hops=$$((hops + 1)); [ "$$hops" -le 40 ] || return 1; \
					link=$$(readlink "$$out/$$part") || return 1; \
					case $$link in (/*) out= ;; esac; rest="$$link$$rest"; \
				else out="$$out/$$part"; fi ;; \
			esac; \
		done; printf '%s\n' "$${out:-/}"; }; \
	envrc_lines() { acc=; held=0; \
		while IFS= read -r line || [ -n "$$line" ]; do \
			if [ "$$held" -eq 0 ]; then \
				case $${line$(host_hash)"$${line%%[![:space:]]*}"} in ('$(host_hash)'*) line= ;; esac; \
			fi; \
			case $$line in \
			(*'\') acc="$$acc$${line%?}"; held=$$((held + 1)) ;; \
			(*) printf '%s\n' "$$acc$$line"; acc=; \
				while [ "$$held" -gt 0 ]; do printf '\n'; held=$$((held - 1)); done ;; \
			esac; \
		done; \
		[ "$$held" -eq 0 ] || printf '%s\n' "$$acc"; }; \
	envrc="$$clone/.envrc"; \
	if [ -e "$$envrc" ] || [ -L "$$envrc" ]; then \
		clone_real=$$(cd "$$clone" && pwd -P); \
		envrc_real=$$(resolve "$$envrc") || envrc_real=; \
		if [ -z "$$clone_real" ]; then \
			envrc_real=; \
		elif case $$envrc_real in ("$$clone_real"/*) false ;; (*) true ;; esac; then \
			echo "$$target: refused: the host clone's .envrc, $$envrc, is a symlink to a file outside the host clone: $$why" >&2; \
			refused=1; envrc_real=-; \
		fi; \
		if [ "$$envrc_real" = - ]; then \
			:; \
		elif [ -n "$$envrc_real" ] && [ -f "$$envrc_real" ] && [ -r "$$envrc_real" ] && \
			text=$$(envrc_lines < "$$envrc_real") && \
			found=$$(printf '%s\n' "$$text" | sed 's/[;&|()]/&&/g' | \
				grep -a -n -oE '$(ENVRC_GUARD_ERE)' || \
				[ "$$?" -eq 1 ]); then \
			text=; \
			IFS=$$nl; for entry in $$found; do \
				unset IFS; \
				num=$${entry%%:*}; match=$${entry$(host_hash)*:}; \
				case $$match in ([sd.]*) ;; (*) match=$${match$(host_hash)?} ;; esac; \
				directive=$${match%%[!a-z_.]*}; rest=$${match$(host_hash)"$$directive"}; \
				set -- $$rest; arg=$${1-}; \
				case $$arg in (';' | '&' | '|' | '(' | ')') arg= ;; esac; \
				case $$directive in \
				(dotenv | dotenv_if_exists) \
					outside=; \
					case $$rest in \
					([\"\'\\]) outside=1 ;; \
					(*) case $$arg in ('') arg=.env ;; (\"*\" | \'*\') arg=$${arg$(host_hash)?}; arg=$${arg%?} ;; esac; \
						case $$arg in \
						('' | *[!A-Za-z0-9._/+@=-]*) outside=1 ;; \
						(*) case $$arg in (/*) path=$$arg ;; (*) path="$$clone_real/$$arg" ;; esac; \
							path=$$(resolve "$$path") || path=; \
							if [ -n "$$path" ] && [ -d "$$path" ]; then path=$$(resolve "$$path/.env") || path=; fi; \
							case $$path in ("$$clone_real"/*) ;; (*) outside=1 ;; esac ;; \
						esac ;; \
					esac; \
					if [ -n "$$outside" ]; then \
						echo "$$target: refused: $$envrc: line $$num: the host clone's .envrc loads with $$directive a path this check cannot read as a plain path inside the host clone: $$why" >&2; \
						refused=1; \
					fi ;; \
				(*) echo "$$target: refused: $$envrc: line $$num: the host clone's .envrc runs another file with $$directive, which this check does not read: put that file's lines in the host clone's .envrc itself (docs/dev-environment.md, The host clone)" >&2; \
					refused=1 ;; \
				esac; \
			done; \
		else \
			text=; \
			echo "$$target: refused: cannot read the host clone's .envrc, $$envrc, to check that it reads no file outside the host clone: $$why" >&2; \
			refused=1; \
		fi; \
	fi; \
	seen=$$nl; \
	for start in "$$dir" "$$dir_real"; do \
		dir=$$start; \
		while :; do \
			for name in .envrc .env; do \
				file="$${dir%/}/$$name"; \
				if [ -e "$$file" ] || [ -L "$$file" ]; then \
					case $$seen in \
					(*"$$nl$$file$$nl"*) ;; \
					(*) seen="$$seen$$file$$nl"; \
						echo "$$target: refused: $$file is in the workspace the VM mounts or in a directory above it, where direnv loads it in every shell opened in the workspace: move its lines into the host clone's .envrc and remove it; $$why" >&2; \
						refused=1 ;; \
					esac; \
				fi; \
			done; \
			[ -n "$$dir" ] && [ "$$dir" != / ] || break; \
			dir=$${dir%/*}; [ -n "$$dir" ] || dir=/; \
		done; \
	done; \
	[ -z "$$refused" ] || echo refused
ifneq ($(envrc_guard_goals),)
envrc_guard_refused := $(shell $(call ENVRC_GUARD,$(firstword $(envrc_guard_goals))))
ifneq ($(envrc_guard_refused),)
$(error $(firstword $(envrc_guard_goals)): refused: direnv would read the credentials from a .envrc or .env other than the host clone's own (each named above); nothing ran (docs/dev-environment.md, The host clone))
endif
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
build: ## Build the shift2eu binary into ./bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/$(BINARY)

# The site's bundle (PRD #297): make site writes it into bin/site and its archive beside it,
# bin/shift2eu-site_<version>.tar.gz, both git-ignored under /bin/. SITE_VERSION (read raw and exported in the
# first block) is a release tag, vX.Y.Z, which the pages show and their links name, and which the builder
# refuses in any other form; empty, the pages show internal/version's development value and link to main, as an
# untagged build must, and the archive is named for dev. bin/site is removed first: the builder refuses a
# directory that holds anything, so no stray file is ever bundled.
.PHONY: site
site: ## Build the site's bundle into bin/site and its archive into bin/shift2eu-site_<version>.tar.gz (SITE_VERSION=vX.Y.Z for a release's)
	rm -rf bin/site
	go run ./ci/sitebuild -out bin/site -archive "bin/shift2eu-site_$${SITE_VERSION:-dev}.tar.gz" $${SITE_VERSION:+-version "$$SITE_VERSION"}

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

# The site's headless-browser smoke test (PRD #297 milestone 2, docs/adr/0036). make site builds the bundle; the
# recipe starts the pinned headless Chromium ($(HEADLESS_SHELL)) with the host network and its own entrypoint
# replaced, so DevTools listens on 127.0.0.1 only (the image's run.sh would open it on every interface through
# socat), on a port the kernel picks (--remote-debugging-port=0, issue #368), so no fixed port is open to
# guess and the DevTools ports and containers of two runs at once do not collide. Two runs in the same worktree
# still share bin/site, which make site removes and rebuilds (a follow-up). It waits until Chromium logs the
# browser's DevTools address on 127.0.0.1, reads that address back from the log line (port and browser id, so
# the test attaches to the browser this run started and no other: a correctness guarantee, not access control,
# since any local process that finds the port can read the id from /json/version on it), and runs test/browser
# against it. The container's name carries the recipe shell's PID, so parallel runs have their own; it is
# removed on every exit, failure and interrupt included. chromedp's pipe transport (--remote-debugging-pipe) would open no port at all, but it hands the
# browser fds 3 and 4 of a process it starts itself, and docker run and nerdctl run pass no such fds into a
# container (docs/adr/0036, amendment for issue #368).
.PHONY: test-browser
test-browser: site ## Build the site's bundle and run its headless-browser smoke test in a pinned Chromium container (docker or nerdctl)
	@set -eu; \
	devtools=; \
	name=shift2eu-test-browser-$$$$; \
	trap '$(DOCKER) rm -f "$$name" >/dev/null 2>&1 || true' EXIT; \
	trap 'exit 130' INT TERM; \
	$(DOCKER) run -d --name "$$name" --network host --entrypoint /headless-shell/headless-shell \
		$(HEADLESS_SHELL) --no-sandbox --use-gl=angle --use-angle=swiftshader \
		--remote-debugging-address=127.0.0.1 --remote-debugging-port=0 >/dev/null; \
	tries=0; \
	until devtools=$$($(DOCKER) logs "$$name" 2>&1 | \
		sed -n 's|^DevTools listening on \(ws://127\.0\.0\.1:[0-9][0-9]*/devtools/browser/[0-9a-f-][0-9a-f-]*\)$$|\1|p' | \
		head -n 1); [ -n "$$devtools" ]; do \
		tries=$$((tries + 1)); \
		if [ "$$tries" -ge 60 ]; then \
			echo "test-browser: Chromium logged no DevTools address on 127.0.0.1 within 60 seconds" >&2; \
			$(DOCKER) logs "$$name" >&2 || true; \
			exit 1; \
		fi; \
		sleep 1; \
	done; \
	SHIFT2EU_DEVTOOLS_URL="$$devtools" SHIFT2EU_SITE_BUNDLE="$(CURDIR)/bin/site" \
		go test -count=1 -timeout 20m -tags browser ./test/browser/

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

# The offline checks of infra/'s OpenTofu configuration (issue #422): dev/infra-check.sh runs the pinned,
# checksum-verified OpenTofu (dev/tofu.sh, infra/tofu.yaml) with no credential and no call to Scaleway.
.PHONY: infra-check
infra-check: ## Check infra/'s OpenTofu configuration offline: static rules, fmt, init -backend=false, validate, test
	dev/infra-check.sh

.PHONY: check
check: lint test vuln infra-check ## Everything CI gates on; run before every commit

##@ Workflow

# Reads GitHub through gh as the maintainer, to read the rulesets' bypass list (the agents App's token cannot),
# so it is a host target: it runs only from the host clone.
.PHONY: gate-check
gate-check: ## Check that Protect main and CODEOWNERS on main require the maintainer's approval (fails on any gap; host clone)
	dev/protect-main-check.sh

.PHONY: worktree
worktree: ## Create or resume the worktree for a PR: make worktree BRANCH=feat/12-short-name
	@dev/worktree.sh "$(BRANCH)"

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
# Before it lists the instances it warns, and only warns, about a Lima autostart job of the instance,
# $(call LIMA_AUTOSTART_JOBS,warning) below (issue #315): the VM starts as it always does.
#
# LIMA_AUTOSTART_JOBS finds Lima's own autostart job of LIMA_INSTANCE (issues #315, #398): `limactl autostart
# enable` writes and loads io.lima-vm.autostart.<instance> in the user's domain (gui/<uid>,
# ~/Library/LaunchAgents/<label>.plist) and, with --condition=boot, io.lima-vm.daemon.<instance> as a
# LaunchDaemon in the system domain ($(LIMA_LAUNCHDAEMONS_DIR)/<label>.plist). Both run
# `limactl start <name> --foreground`, the form that can fall back to template:default, a VM that mounts the home
# directory (issue #237). For each it asks, where launchctl exists, `launchctl print <domain>/<label>`, never
# sudo, and else, launchctl or not, looks for the plist on disk: an SSH-only session has no gui/<uid> domain to
# ask, and a plist nothing has loaded yet loads at the next login or boot. For each job found, once, it prints on
# stderr "$@: <its argument>:", the job, its domain (loaded) or its plist (installed), `limactl autostart disable
# <instance>` and the removal by hand (the bootout and `rm -f` of the plist of a loaded job, the `rm -f` of an
# installed one; sudo only in the system domain), and sets lima_autostart=1 (empty when there is none). The
# plist's path is repeated only when it is absolute and holds only / and [A-Za-z0-9._-] with no .. segment, the
# rule of dev/agents-token-launchd.sh's foreign_jobs (auditor nit #396): the user domain's path is built from
# $HOME, which may hold a space, a ; or a $ (security audit L1). Otherwise it is never printed, and the advice
# says "remove its plist, whose path is not shown" with an ls of the literal ~/Library/LaunchAgents/<label>.plist
# (or $(LIMA_LAUNCHDAEMONS_DIR)/<label>.plist) that names it, joined to the bootout by ", then" rather than the
# && of the pasteable safe-path command (AI reviewer nit, PR #417). It
# removes nothing itself. vm-up warns with it; vm-boot-install refuses with it. Only labels and files of Lima's
# own naming are asked about, so it reads no job of anyone else.
#
# LIMA_LAUNCHDAEMONS_DIR is a test hook like VM_BOOT_LAUNCHD_DIR (see there), but only read, never written: the
# directory Lima's LaunchDaemon plist sits in, /Library/LaunchDaemons. Never set it on a real Mac; it comes only
# from the make command line or this file, and a value from the environment is silently replaced by
# /Library/LaunchDaemons: the check only reads there, so a stale export can at worst miss a plist, unlike
# LAUNCHD_DIR_CHECK, which refuses one for the directories root writes to. The macro refuses a value that is
# not absolute or holds anything but letters, digits, /, ., _ and -. The test matches every origin that starts
# with environment, as LAUNCHD_DIR_CHECK's environment* does, so a value from the environment is replaced under
# make -e too, whichever GNU Make reports it ("environment" or "environment override", as 3.81 on the Mac may).
ifneq (,$(filter environment%,$(origin LIMA_LAUNCHDAEMONS_DIR)))
override LIMA_LAUNCHDAEMONS_DIR := /Library/LaunchDaemons
endif
LIMA_LAUNCHDAEMONS_DIR ?= /Library/LaunchDaemons
LIMA_AUTOSTART_USER_LABEL_PREFIX = io.lima-vm.autostart.
LIMA_AUTOSTART_SYSTEM_LABEL_PREFIX = io.lima-vm.daemon.
LIMA_AUTOSTART_JOBS = lima_autostart=''; \
	case "$${LIMA_LAUNCHDAEMONS_DIR:-}" in \
	(/*) ;; \
	(*) echo "$@: LIMA_LAUNCHDAEMONS_DIR must be an absolute path (it is a test hook: leave it at /Library/LaunchDaemons)" >&2; exit 1 ;; \
	esac; \
	case "$$LIMA_LAUNCHDAEMONS_DIR" in \
	(*[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789/._-]* | */../* | */..) \
		echo "$@: LIMA_LAUNCHDAEMONS_DIR may hold only letters, digits, /, ., _ and -, and no .. component (it is a test hook: leave it at /Library/LaunchDaemons)" >&2; exit 1 ;; \
	esac; \
	lima_launchctl=''; command -v launchctl >/dev/null 2>&1 && lima_launchctl=1; \
	lima_uid="$$(id -u)" || lima_uid=''; \
	for lima_domain in user system; do \
		case "$$lima_domain" in \
		(user) lima_label="$(LIMA_AUTOSTART_USER_LABEL_PREFIX)$$LIMA_INSTANCE"; lima_target="$${lima_uid:+gui/$$lima_uid/$$lima_label}"; \
			lima_plist="$$HOME/Library/LaunchAgents/$$lima_label.plist"; lima_sudo='' ;; \
		(*) lima_label="$(LIMA_AUTOSTART_SYSTEM_LABEL_PREFIX)$$LIMA_INSTANCE"; lima_target="system/$$lima_label"; \
			lima_plist="$${LIMA_LAUNCHDAEMONS_DIR%/}/$$lima_label.plist"; lima_sudo='sudo ' ;; \
		esac; \
		lima_at=" at $$lima_plist"; lima_remove="$${lima_sudo}rm -f $$lima_plist"; lima_then=' && '; \
		case "$$lima_plist" in \
		([!/]* | */../* | */.. | *[!/ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-]*) \
			case "$$lima_domain" in \
			(user) lima_where="~/Library/LaunchAgents/$$lima_label.plist" ;; \
			(*) lima_where="$${LIMA_LAUNCHDAEMONS_DIR%/}/$$lima_label.plist" ;; \
			esac; \
			lima_at=' (its plist'"'"'s path is not shown)'; \
			lima_remove="remove its plist, whose path is not shown: ls $$lima_where names it"; lima_then=', then ' ;; \
		esac; \
		if [ -n "$$lima_launchctl" ] && [ -n "$$lima_target" ] && launchctl print "$$lima_target" </dev/null >/dev/null 2>&1; then \
			lima_autostart=1; \
			echo "$@: $(1): Lima's autostart job $$lima_label is loaded in $${lima_target%/*}: it runs limactl start $$LIMA_INSTANCE --foreground, which can fall back to template:default, a VM that mounts the home directory. Remove it with: limactl autostart disable $$LIMA_INSTANCE (or by hand: $${lima_sudo}launchctl bootout $$lima_target$$lima_then$$lima_remove) (docs/dev-environment.md, Do not let Lima start the VM)" >&2; \
		elif [ -e "$$lima_plist" ] || [ -L "$$lima_plist" ]; then \
			lima_autostart=1; \
			echo "$@: $(1): Lima's autostart job $$lima_label is installed$$lima_at, not loaded now: launchd loads it at the next login or boot, and it runs limactl start $$LIMA_INSTANCE --foreground, which can fall back to template:default, a VM that mounts the home directory. Remove it with: limactl autostart disable $$LIMA_INSTANCE (or by hand: $$lima_remove) (docs/dev-environment.md, Do not let Lima start the VM)" >&2; \
		fi; \
	done

vm-up vm-boot-install: export LIMA_LAUNCHDAEMONS_DIR := $(LIMA_LAUNCHDAEMONS_DIR)
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
	$(call LIMA_AUTOSTART_JOBS,warning); \
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

# $(call AGENTS_TOKEN_READ_PID,"$$path") prints what a pid entry of that state directory holds, or nothing,
# the way read_pid in dev/agents-app-token.sh reads one: only a regular file that is no symlink, and at most
# 32 bytes of it. The directory is on the Mac and anything may have been put in it, and a cat of a FIFO
# nobody writes to blocks for ever (issue #335). An entry that is no regular file reads as empty.
AGENTS_TOKEN_READ_PID = { [ -f $(1) ] && [ ! -L $(1) ] && head -c 32 $(1) 2>/dev/null || true; }

# The token targets run on the Mac with the App's key in reach. Like every host target they run only from the
# host clone, whose own direnv .envrc holds the three SHIFT2EU_AGENTS_APP_* variables, and make refuses them
# elsewhere (HOST_TARGETS above; docs/dev-environment.md, The host clone). Their recipes use only limactl
# and LIMA_INSTANCE, not WORKSPACE_REPO, but like every host target they refuse to run without it (the
# host-clone refusal). The host clone does not put the key out of reach of code that runs as you: what
# remains open is issue #227's part 2 (ADR 0031, Consequences).
# Like dev/protect-main-check.sh and dev/shellcheck.sh, their scripts run by path and shebang, with no
# explicit interpreter (issue #242): both are executable (100755) in git.
#
# Account mode (issue #227 part 2, ADR 0031's and ADR 0034's updates of 2026-11-19): with SHIFT2EU_AGENTS_ACCOUNT
# set in the host clone's .envrc, the App's key is the separate account's (/Users/<account>/.config/shift2eu),
# out of the maintainer's reach, and the refresh runs as that account's LaunchDaemon, from the account's own clone
# (/Users/<account>/shift2eu-host), writing the token into the VM over its pinned SSH port
# (SHIFT2EU_AGENTS_VM_SSH_PORT; SHIFT2EU_AGENTS_VM_SSH_USER, default id -un). Then agents-token,
# agents-token-refresh and agents-token-refresh-start refuse before they run anything (the maintainer cannot
# mint), agents-token-refresh-install renders the plist as the account with the account's generator, and every
# account-mode target that runs sudo ends with sudo -k, on success and on failure. Without it, the
# single-account mode below is unchanged: the fallback until the maintainer has switched.

# AGENTS_ACCOUNT_CHECK sets account to SHIFT2EU_AGENTS_ACCOUNT (empty: single-account mode) and refuses a name that
# does not begin with a letter or _ (so no -, ., .. or digit first), holds anything but letters, digits, ., _ and
# -, or is root or the maintainer's own (id -un): it becomes part of root's commands and the job's paths. It never
# shows the value it refuses. Before any sudo it checks the account's kind (issue #227 part 2, the security audit's
# second pass): dscl must read a UniqueID from 1 to 499 (a hidden role account; macOS gives a login account 500 and
# above), and dseditgroup must answer its explicit negative, a non-zero status with an answer that begins "no ":
# the admin check fails closed, so an exit 0, a yes, an error without an answer or a missing dseditgroup refuses
# the account. A refusal names the check, never the value. In account mode a SHIFT2EU_AGENTS_APP_KEY_FILE still
# set gets a warning: the account's key is the derived path.
#
# Its parts: AGENTS_ACCOUNT_NAME_CHECK refuses a bad name, AGENTS_ACCOUNT_KIND sets kind to what is wrong with the
# account's kind (empty: a hidden role account, or single-account mode) and AGENTS_ACCOUNT_KEY_FILE_WARNING warns
# about the key file. agents-token-refresh-uninstall, the kill switch, runs them with AGENTS_ACCOUNT_KIND_WARNING in
# place of the refusal: a job loaded for an account that is no role account must still be removable.
AGENTS_ACCOUNT_CHECK = $(AGENTS_ACCOUNT_NAME_CHECK); $(AGENTS_ACCOUNT_KIND); \
	if [ -n "$$kind" ]; then echo "$@: $$kind" >&2; exit 1; fi; \
	$(AGENTS_ACCOUNT_KEY_FILE_WARNING)
AGENTS_ACCOUNT_KIND_WARNING = if [ -n "$$kind" ]; then \
		echo "$@: warning: $$kind; booting the job out anyway, so it stops whatever the account is" >&2; \
	fi
AGENTS_ACCOUNT_NAME_CHECK = account="$${SHIFT2EU_AGENTS_ACCOUNT:-}"; \
	if [ -n "$$account" ]; then \
		case "$$account" in \
		[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_]* | *[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-]*) \
			echo "$@: SHIFT2EU_AGENTS_ACCOUNT must be letters, digits, ., _ and -, beginning with a letter or _: it becomes part of root's commands and the job's paths (its value is not shown; docs/dev-environment.md, The agents App)" >&2; exit 1 ;; \
		esac; \
		if [ "$$account" = root ] || [ "$$account" = "$$(id -un)" ]; then \
			echo "$@: SHIFT2EU_AGENTS_ACCOUNT must name the separate account that holds the App's key, never root or your own account (docs/dev-environment.md, The agents App)" >&2; exit 1; \
		fi; \
	fi
AGENTS_ACCOUNT_KIND = kind=""; \
	if [ -n "$$account" ]; then \
		if ! account_uid="$$(dscl . -read "/Users/$$account" UniqueID 2>/dev/null)"; then \
			kind="dscl cannot read the UniqueID of the account SHIFT2EU_AGENTS_ACCOUNT names: create the hidden role account first (docs/dev-environment.md, The agents App)"; \
		else \
			account_uid="$$(printf '%s\n' "$$account_uid" | sed -n 's/^UniqueID:[[:space:]]*\([0123456789][0123456789]*\)[[:space:]]*$$/\1/p' | head -n 1)"; \
			case "$$account_uid" in \
			'' | 0* | ????*) \
				kind="the account SHIFT2EU_AGENTS_ACCOUNT names has no UniqueID from 1 to 499 (dscl), so it is no hidden role account: a login account's key is in the maintainer's reach (docs/dev-environment.md, The agents App)" ;; \
			*) \
				if [ "$$account_uid" -ge 500 ]; then \
					kind="the account SHIFT2EU_AGENTS_ACCOUNT names has a UniqueID of 500 or above (dscl), a login account, not a hidden role account below 500 (docs/dev-environment.md, The agents App)"; \
				fi ;; \
			esac; \
		fi; \
		if [ -z "$$kind" ]; then \
			account_admin="$$(dseditgroup -o checkmember -m "$$account" admin 2>/dev/null)"; account_admin_status=$$?; \
			account_admin_kind=refuse; \
			if [ "$$account_admin_status" -eq 0 ]; then \
				account_admin_kind=admin; \
			else \
				case "$$account_admin" in \
				yes*) account_admin_kind=admin ;; \
				"no "*) account_admin_kind=pass ;; \
				esac; \
			fi; \
			case "$$account_admin_kind" in \
			admin) \
				kind="the account SHIFT2EU_AGENTS_ACCOUNT names is a member of the admin group (dseditgroup), so it is no hidden role account: remove it from admin (docs/dev-environment.md, The agents App)" ;; \
			pass) ;; \
			*) \
				kind="the admin check (dseditgroup -o checkmember) gave no explicit no for the account SHIFT2EU_AGENTS_ACCOUNT names (status $$account_admin_status), so it cannot tell that the account is no admin: the check fails closed (docs/dev-environment.md, The agents App)" ;; \
			esac; \
		fi; \
	fi
AGENTS_ACCOUNT_KEY_FILE_WARNING = if [ -n "$$account" ] && [ -n "$${SHIFT2EU_AGENTS_APP_KEY_FILE:-}" ]; then \
		echo "$@: warning: SHIFT2EU_AGENTS_APP_KEY_FILE is set, but in account mode the App's key is the account's, /Users/$$account/.config/shift2eu/agents-app.pem: remove the variable from the host clone's .envrc, delete the file it names, and delete that key on the App's page (docs/dev-environment.md, The agents App)" >&2; \
	fi
# AGENTS_ACCOUNT_NO_MINT refuses a minting target in account mode: only the account can read the key.
AGENTS_ACCOUNT_NO_MINT = if [ -n "$$account" ]; then \
		echo "$@: the refresh runs as $$account, which holds the App's key; the maintainer cannot mint: run sudo launchctl kickstart -k system/$(AGENTS_TOKEN_LAUNCHD_LABEL_PREFIX)$$LIMA_INSTANCE to mint now (docs/dev-environment.md, The agents App)" >&2; \
		exit 1; \
	fi
# AGENTS_SSH_CONFIG sets port to SHIFT2EU_AGENTS_VM_SSH_PORT and vm_user to SHIFT2EU_AGENTS_VM_SSH_USER (default
# id -un, Lima's default VM user) and refuses either malformed, for the account-mode targets.
AGENTS_SSH_CONFIG = port="$${SHIFT2EU_AGENTS_VM_SSH_PORT:-}"; \
	case "$$port" in \
	'') echo "$@: set SHIFT2EU_AGENTS_VM_SSH_PORT, the VM's pinned SSH port, in the host clone's .envrc beside SHIFT2EU_AGENTS_ACCOUNT (docs/dev-environment.md, The agents App)" >&2; exit 1 ;; \
	*[!0123456789]* | ??????* | 0*) echo "$@: SHIFT2EU_AGENTS_VM_SSH_PORT must be a port, 1 to 65535 in digits" >&2; exit 1 ;; \
	esac; \
	if [ "$$port" -gt 65535 ]; then echo "$@: SHIFT2EU_AGENTS_VM_SSH_PORT must be a port, 1 to 65535 in digits" >&2; exit 1; fi; \
	$(AGENTS_SSH_CONFIG_USER)
AGENTS_SSH_CONFIG_USER = vm_user="$${SHIFT2EU_AGENTS_VM_SSH_USER:-$$(id -un)}"; \
	case "$$vm_user" in \
	[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_]* | *[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-]*) \
		echo "$@: SHIFT2EU_AGENTS_VM_SSH_USER must be letters, digits, ., _ and -, beginning with a letter, digit or _ (its value is not shown)" >&2; exit 1 ;; \
	esac

.PHONY: agents-token
agents-token: ## Mint one agents App installation token on the Mac and write it, and the bot identity, into the VM (host clone)
	$(LIMA_INSTANCE_CHECK)
	@$(AGENTS_ACCOUNT_CHECK); $(AGENTS_ACCOUNT_NO_MINT); \
	exec dev/agents-app-token.sh mint

.PHONY: agents-token-refresh
agents-token-refresh: ## Mint the agents App token now and again before each one expires, in the foreground, until stopped (host clone)
	$(LIMA_INSTANCE_CHECK)
	@$(AGENTS_ACCOUNT_CHECK); $(AGENTS_ACCOUNT_NO_MINT); \
	exec dev/agents-app-token.sh refresh

# Starts the refresh of this VM instance in the background unless one already runs: the one whose pid the
# instance's lock holds (a refresh started by hand with make agents-token-refresh holds it too), or the one
# this target started earlier. It warns when that one has stopped, and prints where the log is. When launchd
# has the instance's LaunchDaemon loaded (agents-token-refresh-install), it warns and starts nothing, writing
# no file: the job is the instance's refresher. Before either it refuses another launchd job that runs the
# instance's refresh, $(AGENTS_TOKEN_FOREIGN_JOBS_CHECK) (issue #396). Without launchctl (Linux) both checks
# are skipped. Like install
# and uninstall below, it reads LIMA_INSTANCE from its environment ($$LIMA_INSTANCE) and refuses an invalid
# name with $(LIMA_INSTANCE_CHECK) before it runs: the name becomes part of its file names and of the job's label.
# The refresher runs without the default VM_ENV credentials, CLAUDE_CODE_OAUTH_TOKEN and DEEPSEEK_API_KEY
# (env -u), which it never needs and which a shell with the host clone's .envrc loaded may hold (issue #227).
# A name a custom VM_ENV adds is not stripped. It reads both pids with $(AGENTS_TOKEN_READ_PID), and removes a
# pid file that is no regular file before it writes the new pid, since writing into a FIFO blocks (issue #335).
# Accepted residue (issue #396, item 5): the new pid goes to a temporary file renamed onto the pid file with
# mv -f, and a Mac's mv has no -T, so a directory created at the pid file's path between that removal and the
# rename would take the temporary file inside it, and the next start would find no pid file. Only this uid can
# create entries in the state directory (created 0700, under the maintainer's home), so that needs code already running
# as the maintainer, which can do worse; nothing is lost but the pid file, and the lock still keeps a second
# refresher from running.
.PHONY: agents-token-refresh-start
agents-token-refresh-start: ## Start the agents App token refresh in the background, one per VM instance, and print where its log is (host clone)
	$(LIMA_INSTANCE_CHECK)
	@$(AGENTS_ACCOUNT_CHECK); $(AGENTS_ACCOUNT_NO_MINT); \
	if [ -z "$${SHIFT2EU_AGENTS_APP_ID:-}" ] || [ -z "$${SHIFT2EU_AGENTS_APP_INSTALLATION_ID:-}" ] || [ -z "$${SHIFT2EU_AGENTS_APP_KEY_FILE:-}" ]; then \
		echo "agents-token-refresh-start: set SHIFT2EU_AGENTS_APP_ID, SHIFT2EU_AGENTS_APP_INSTALLATION_ID and SHIFT2EU_AGENTS_APP_KEY_FILE in the host clone's .envrc (docs/dev-environment.md, The host clone)" >&2; \
		exit 1; \
	fi; \
	$(AGENTS_TOKEN_FOREIGN_JOBS_CHECK); \
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
	started="$$($(call AGENTS_TOKEN_READ_PID,"$$pid_file"))"; \
	for pid in "$$($(call AGENTS_TOKEN_READ_PID,"$$lock_pid"))" "$$started"; do \
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
	if [ -L "$$pid_file" ] || { [ -e "$$pid_file" ] && [ ! -f "$$pid_file" ]; }; then \
		rm -f "$$pid_file" || { echo "agents-token-refresh-start: nothing was started: $$pid_file is no regular file and could not be removed; remove it by hand" >&2; exit 1; }; \
	fi; \
	env -u CLAUDE_CODE_OAUTH_TOKEN -u DEEPSEEK_API_KEY nohup dev/agents-app-token.sh refresh </dev/null >>"$$log" 2>&1 & \
	pid="$$!"; \
	pid_tmp="$$(umask 077 && mktemp "$$state/.agents-token-refresh-$$LIMA_INSTANCE.pid.XXXXXX")" && \
		echo "$$pid" >"$$pid_tmp" && mv -f "$$pid_tmp" "$$pid_file" || \
		{ [ -z "$$pid_tmp" ] || rm -f "$$pid_tmp"; \
		  echo "agents-token-refresh-start: the refresh of $$LIMA_INSTANCE runs (pid $$pid), but writing its pid file $$pid_file failed" >&2; exit 1; }; \
	echo "agents-token-refresh-start: started the agents App token refresh of $$LIMA_INSTANCE (pid $$pid); its log is $$log"

# The LaunchDaemon that runs the refresh at boot, with nobody logged in, as you (issue #229, ADR 0034): one job
# per VM instance, whose plist dev/agents-token-launchd.sh renders from the host clone's .envrc (the plist, not
# direnv, holds the variables, copied at install time: install again after changing them). The job runs
# /bin/sh dev/agents-app-token.sh refresh from the host clone, never make or git, and logs where
# agents-token-refresh-start does. Installing needs sudo; both targets print every sudo command before they
# run it. A refresh outside the job keeps the instance's lock while it runs: the job then exits 0 at once and
# launchd tries again every ThrottleInterval (5 minutes), for as long as that refresh runs (issue #396). So the
# install reads the lock before any sudo (the job's own refresh takes it as soon as it loads) and, when a refresh
# holds it that is not the pid launchctl print reports for the loaded job:
#   - when it is the refresh agents-token-refresh-start recorded in the instance's pid file (the documented order:
#     start it for make vm-init, then install), the install takes over from it: right before the bootstrap it
#     sends it TERM, the signal its traps expect, waits (at most 10 seconds) for it to exit, removes the pid file
#     and says that it stopped it, naming the pid;
#   - for any other holder (a make agents-token-refresh in a terminal, another clone's refresh) it refuses before
#     it installs anything, naming the pid and kill <pid>: a refresh it did not start is not the install's to stop.
# It also refuses, before anything, another launchd job that runs the instance's refresh,
# $(AGENTS_TOKEN_FOREIGN_JOBS_CHECK) below.
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
# Also spelled out in dev/agents-token-launchd.sh (label_prefix) and dev/agents-app-token.sh (stopped): keep the
# three alike.
AGENTS_TOKEN_LAUNCHD_LABEL_PREFIX = io.github.timothymamo.shift2eu.agents-token-refresh.
# The script a loaded job runs, read from `launchctl print system/<label>`: the first line of its arguments
# block that ends in /dev/agents-app-token.sh, with the indentation removed (empty when there is none).
AGENTS_TOKEN_LAUNCHD_SCRIPT_OF = awk '/^[[:space:]]*arguments = \{/ { a = 1; next } a && /^[[:space:]]*\}/ { exit } a { sub(/^[[:space:]]+/, ""); if ($$0 ~ /\/dev\/agents-app-token\.sh$$/) { print; exit } }'
# AGENTS_TOKEN_FOREIGN_JOBS_CHECK refuses, where launchctl exists, another loaded launchd job that runs this
# instance's refresh (issue #396): one of no label of the project's prefix, in the system or the user domain,
# whose arguments run agents-app-token.sh refresh or make agents-token-refresh. It holds the instance's lock
# again seconds after every kill (KeepAlive), so the project's job would never take over. dev/agents-token-launchd.sh
# foreign-jobs finds it with launchctl alone, no sudo, and names each job, its domain, its plist and the
# commands that remove it. install and start run it before they install, load or start anything. Only its status
# 3 means a job was found and named; any other non-zero status is the check's own failure, and stops them too.
AGENTS_TOKEN_FOREIGN_JOBS_CHECK = if command -v launchctl >/dev/null 2>&1; then \
		foreign_status=0; dev/agents-token-launchd.sh foreign-jobs || foreign_status=$$?; \
		if [ "$$foreign_status" = 3 ]; then \
			echo "$@: refused: another launchd job runs the agents App token refresh of $$LIMA_INSTANCE (named above), so nothing was installed, loaded or started: remove it with the commands above, then run make $@ again" >&2; \
			exit 1; \
		elif [ "$$foreign_status" != 0 ]; then \
			echo "$@: foreign-jobs check failed (status $$foreign_status): nothing was installed, loaded or started; see the message above, then run make $@ again" >&2; \
			exit 1; \
		fi; \
	fi
# $(call LAUNCHD_DIR_CHECK,<variable>) checks a LaunchDaemons directory variable, the directory root writes a
# plist into and removes it from: AGENTS_TOKEN_LAUNCHD_DIR here, VM_BOOT_LAUNCHD_DIR for the boot job (issue
# #398). It reads the variable's origin from <variable>_ORIGIN, recorded at parse time, and the value from the
# recipe's environment, where the target-specific export puts it.
LAUNCHD_DIR_CHECK = case "$($(1)_ORIGIN)" in \
	environment*) \
		echo "$@: refused: $(1) comes from the environment, where a stale export would have root install the plist where launchd never looks at boot: unset it (it is a test hook: leave it at /Library/LaunchDaemons, or pass it on the make command line)" >&2; exit 1 ;; \
	esac; \
	case "$${$(1):-}" in \
	/*) ;; \
	*) echo "$@: $(1) must be an absolute path (it is a test hook: leave it at /Library/LaunchDaemons)" >&2; exit 1 ;; \
	esac; \
	case "$$$(1)" in \
	*[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789/._-]* | */../* | */..) \
		echo "$@: $(1) may hold only letters, digits, /, ., _ and -, and no .. component, since root writes there (it is a test hook: leave it at /Library/LaunchDaemons)" >&2; exit 1 ;; \
	esac
AGENTS_TOKEN_LAUNCHD_DIR_CHECK = $(call LAUNCHD_DIR_CHECK,AGENTS_TOKEN_LAUNCHD_DIR)

agents-token-refresh-install agents-token-refresh-uninstall: export AGENTS_TOKEN_LAUNCHD_DIR := $(AGENTS_TOKEN_LAUNCHD_DIR)

# agents-token-refresh-install's take-over of a refresh make agents-token-refresh-start started, between the
# checked install of the plist and launchctl bootstrap: the pid files are read again after sudo, which can wait
# for a password: the start target's pid file, so a refresh it no longer names is not stopped; the lock's pid
# file, so only the refresh that still holds the lock is; and the start target's pid file once more, so only its
# own pid is removed. The pid could be reused between ps -p and kill -TERM: that window is accepted. (This
# comment sits here, not in the recipe: a `#` line inside a continued command ends the recipe under make 3.81,
# issue #437.)
.PHONY: agents-token-refresh-install
agents-token-refresh-install: ## Install (or update) the LaunchDaemon that runs the agents App token refresh at boot, one per VM instance; needs sudo (host clone, macOS)
	$(LIMA_INSTANCE_CHECK)
	@if [ "$$(id -u)" = 0 ]; then \
		echo "agents-token-refresh-install: refused: run it as the maintainer, not as root (no sudo make); it runs sudo itself. The job runs as the account that installs it, and as root it would run the host clone's script, which the maintainer can edit, at every boot" >&2; \
		exit 1; \
	fi; \
	$(AGENTS_ACCOUNT_CHECK); \
	if [ -n "$$account" ]; then \
		case "$${SHIFT2EU_AGENTS_APP_ID:-}" in \
		'' | *[!0123456789]*) \
			echo "agents-token-refresh-install: set SHIFT2EU_AGENTS_APP_ID, a number, in the host clone's .envrc (docs/dev-environment.md, The agents App)" >&2; \
			exit 1 ;; \
		esac; \
		case "$${SHIFT2EU_AGENTS_APP_INSTALLATION_ID:-}" in \
		'' | *[!0123456789]*) \
			echo "agents-token-refresh-install: set SHIFT2EU_AGENTS_APP_INSTALLATION_ID, a number, in the host clone's .envrc (docs/dev-environment.md, The agents App)" >&2; \
			exit 1 ;; \
		esac; \
		$(AGENTS_SSH_CONFIG); \
	elif [ -z "$${SHIFT2EU_AGENTS_APP_ID:-}" ] || [ -z "$${SHIFT2EU_AGENTS_APP_INSTALLATION_ID:-}" ] || [ -z "$${SHIFT2EU_AGENTS_APP_KEY_FILE:-}" ]; then \
		echo "agents-token-refresh-install: set SHIFT2EU_AGENTS_APP_ID, SHIFT2EU_AGENTS_APP_INSTALLATION_ID and SHIFT2EU_AGENTS_APP_KEY_FILE in the host clone's .envrc (docs/dev-environment.md, The host clone)" >&2; \
		exit 1; \
	fi; \
	command -v launchctl >/dev/null 2>&1 || { echo "agents-token-refresh-install: no launchctl: the LaunchDaemon is for the Mac; elsewhere use make agents-token-refresh-start" >&2; exit 1; }; \
	$(AGENTS_TOKEN_LAUNCHD_DIR_CHECK); \
	$(AGENTS_TOKEN_FOREIGN_JOBS_CHECK); \
	label="$(AGENTS_TOKEN_LAUNCHD_LABEL_PREFIX)$$LIMA_INSTANCE"; dest="$${AGENTS_TOKEN_LAUNCHD_DIR%/}/$$label.plist"; \
	if [ -n "$$account" ]; then \
		home="/Users/$$account"; secrets="$$home/.config/shift2eu"; clone="$$home/shift2eu-host"; owner="$$account"; \
		state="$$home/.local/state/shift2eu"; \
	else \
		clone="$$(pwd -P)" || exit 1; owner="$$(id -un)"; state="$(AGENTS_TOKEN_STATE)"; \
		for link in "$$clone/dev" "$$clone/dev/agents-app-token.sh"; do \
			if [ -L "$$link" ]; then \
				echo "agents-token-refresh-install: refused: $$link is a symlink, so the job would run, at every boot, as you, with the App's key in reach, a file outside the clone $$clone that this install never looked at. Run the install from the host clone, with dev/ and dev/agents-app-token.sh as git checked them out (docs/dev-environment.md, The host clone)" >&2; \
				exit 1; \
			fi; \
		done; \
	fi; \
	script="$$clone/dev/agents-app-token.sh"; \
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
	log="$$state/agents-token-refresh-$$LIMA_INSTANCE.log"; \
	lock_pid="$$state/agents-token-refresh-$$LIMA_INSTANCE.lock/pid"; \
	pid_file="$$state/agents-token-refresh-$$LIMA_INSTANCE.pid"; \
	held=''; \
	if [ -z "$$account" ]; then held="$$($(call AGENTS_TOKEN_READ_PID,"$$lock_pid"))"; fi; \
	case "$$held" in ''|*[!0-9]*) held='' ;; *) \
		kill -0 "$$held" 2>/dev/null && ps -p "$$held" -o command= 2>/dev/null | grep -q 'agents-app-token.sh refresh' || held='' ;; \
	esac; \
	if [ -n "$$held" ]; then \
		job_loaded=''; \
		job_print="$$(launchctl print "system/$$label" 2>/dev/null)" && job_loaded=1; \
		job_pid="$$(printf '%s\n' "$$job_print" | awk '/^[[:space:]]*pid = [0-9]+[[:space:]]*$$/ { print $$3; exit }')"; \
		if [ "$$held" = "$$job_pid" ]; then \
			held=''; \
		elif [ "$$($(call AGENTS_TOKEN_READ_PID,"$$pid_file"))" = "$$held" ]; then \
			:; \
		elif [ -n "$$job_loaded" ] && [ -z "$$job_pid" ]; then \
			echo "agents-token-refresh-install: warning: $$label is loaded, but launchctl print names no pid for it, so the install could not read the job's pid to tell whether pid $$held, which holds the lock of $$LIMA_INSTANCE, is the job's own refresh. It is not stopped: if it is the job's, the bootout below ends it and the new job takes over; if it is not (a make agents-token-refresh in a terminal, or another clone's), the job exits at every ThrottleInterval until it ends: stop that refresh, then run make agents-token-refresh-install again" >&2; \
			held=''; \
		else \
			echo "agents-token-refresh-install: refused: a refresh that make agents-token-refresh-start did not start holds the lock of $$LIMA_INSTANCE (pid $$held; a make agents-token-refresh in a terminal, or another clone's), so the job would find the lock taken and exit at every ThrottleInterval. Nothing was installed: stop it (kill $$held), then run make agents-token-refresh-install again" >&2; \
			exit 1; \
		fi; \
	fi; \
	tmp="$$(mktemp "$${TMPDIR:-/tmp}/agents-token-refresh-plist.XXXXXX")" || exit 1; \
	if [ -n "$$account" ]; then \
		trap 'rm -f "$$tmp"; sudo -k' EXIT; \
		echo "+ sudo -u $$account -H /usr/bin/env -i HOME=$$home PATH=/usr/bin:/bin:/usr/sbin:/sbin LIMA_INSTANCE=$$LIMA_INSTANCE SHIFT2EU_AGENTS_APP_ID=$$SHIFT2EU_AGENTS_APP_ID SHIFT2EU_AGENTS_APP_INSTALLATION_ID=$$SHIFT2EU_AGENTS_APP_INSTALLATION_ID SHIFT2EU_AGENTS_APP_KEY_FILE=$$secrets/agents-app.pem SHIFT2EU_AGENTS_VM_SSH_PORT=$$port SHIFT2EU_AGENTS_VM_SSH_USER=$$vm_user SHIFT2EU_AGENTS_VM_SSH_KEY=$$secrets/agents-vm-ssh-key SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS=$$secrets/known-hosts-$$LIMA_INSTANCE /bin/sh $$clone/dev/agents-token-launchd.sh plist"; \
		sudo -u "$$account" -H /usr/bin/env -i "HOME=$$home" PATH=/usr/bin:/bin:/usr/sbin:/sbin \
			"LIMA_INSTANCE=$$LIMA_INSTANCE" "SHIFT2EU_AGENTS_APP_ID=$$SHIFT2EU_AGENTS_APP_ID" \
			"SHIFT2EU_AGENTS_APP_INSTALLATION_ID=$$SHIFT2EU_AGENTS_APP_INSTALLATION_ID" \
			"SHIFT2EU_AGENTS_APP_KEY_FILE=$$secrets/agents-app.pem" \
			"SHIFT2EU_AGENTS_VM_SSH_PORT=$$port" "SHIFT2EU_AGENTS_VM_SSH_USER=$$vm_user" \
			"SHIFT2EU_AGENTS_VM_SSH_KEY=$$secrets/agents-vm-ssh-key" \
			"SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS=$$secrets/known-hosts-$$LIMA_INSTANCE" \
			/bin/sh "$$clone/dev/agents-token-launchd.sh" plist >"$$tmp" || { \
			echo "agents-token-refresh-install: $$account could not render the plist with its clone's dev/agents-token-launchd.sh (see above), so nothing was installed: check the account's clone $$clone and its files (docs/dev-environment.md, The agents App)" >&2; \
			exit 1; \
		}; \
	else \
		trap 'rm -f "$$tmp"' EXIT; \
		dev/agents-token-launchd.sh plist >"$$tmp" || exit 1; \
	fi; \
	plutil -lint "$$tmp" || { echo "agents-token-refresh-install: plutil refused the rendered plist, so launchd would load no job: nothing was installed" >&2; exit 1; }; \
	sum="$$(shasum -a 256 <"$$tmp" | cut -d ' ' -f 1)" || exit 1; \
	echo "agents-token-refresh-install: installing this plist as $$dest (it holds the App ID, the installation ID and the key's path, never the key):"; \
	cat "$$tmp"; \
	echo "agents-token-refresh-install: its SHA-256 is $$sum"; \
	if [ -n "$$account" ]; then \
		echo "agents-token-refresh-install: the job runs $$script at every boot as $$account: it must be $$home/shift2eu-host/dev/agents-app-token.sh, the account's own clone"; \
	else \
		echo "agents-token-refresh-install: the job runs $$script at every boot as $$owner: it must be the host clone's, outside the workspace and every mount of the VM"; \
		echo "agents-token-refresh-install: creating $$state and the log $$log (mode 0600) as $$owner"; \
		(umask 077 && mkdir -p "$$state" && : >>"$$log") || exit 1; \
	fi; \
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
	stopped=''; \
	if [ -n "$$held" ] && [ "$$($(call AGENTS_TOKEN_READ_PID,"$$pid_file"))" != "$$held" ]; then \
		lock_holder="$$($(call AGENTS_TOKEN_READ_PID,"$$lock_pid"))"; \
		if [ -n "$$lock_holder" ] && kill -0 "$$lock_holder" 2>/dev/null; then \
			echo "agents-token-refresh-install: warning: the pid file of make agents-token-refresh-start no longer names pid $$held, which held the lock of $$LIMA_INSTANCE before sudo, so it is not stopped: if a refresh still holds the lock, the job exits at every ThrottleInterval until it ends (stop it, then run make agents-token-refresh-install again)" >&2; \
		fi; \
		held=''; \
	fi; \
	if [ -n "$$held" ] && [ "$$($(call AGENTS_TOKEN_READ_PID,"$$lock_pid"))" = "$$held" ] && kill -0 "$$held" 2>/dev/null && ps -p "$$held" -o command= 2>/dev/null | grep -q 'agents-app-token.sh refresh'; then \
		echo "agents-token-refresh-install: stopping the refresh make agents-token-refresh-start started (pid $$held), which holds the lock of $$LIMA_INSTANCE, so the job takes over from it"; \
		kill -TERM "$$held" 2>/dev/null || true; \
		tries=0; \
		while [ "$$tries" -lt 10 ] && kill -0 "$$held" 2>/dev/null; do sleep 1; tries=$$((tries + 1)); done; \
		if kill -0 "$$held" 2>/dev/null; then \
			echo "agents-token-refresh-install: warning: pid $$held still runs $$tries second(s) after TERM; the job takes the lock once it has exited" >&2; \
		else \
			if [ "$$($(call AGENTS_TOKEN_READ_PID,"$$pid_file"))" = "$$held" ]; then rm -f "$$pid_file"; fi; \
			stopped=' (the hand-started refresh was stopped above, so none runs: start one with make agents-token-refresh-start)'; \
			echo "agents-token-refresh-install: stopped pid $$held, the refresh make agents-token-refresh-start started; the job loaded next is the instance's refresher now"; \
		fi; \
	fi; \
	echo "+ sudo launchctl bootstrap system $$dest"; \
	if ! sudo launchctl bootstrap system "$$dest"; then \
		sleep 1; \
		echo "+ sudo launchctl bootstrap system $$dest"; \
		if ! sudo launchctl bootstrap system "$$dest"; then \
			if launchctl print "system/$$label" >/dev/null 2>&1; then \
				echo "agents-token-refresh-install: launchctl bootstrap exited non-zero twice, but launchctl print finds $$label loaded: the job is loaded" >&2; \
			else \
				echo "agents-token-refresh-install: ERROR: launchctl bootstrap failed twice: NO token refresh job of $$LIMA_INSTANCE is loaded now$$stopped, and the VM's token expires within the hour. The checked plist stays at $$dest, so launchd loads it at the next boot. Run make agents-token-refresh-install again, or start a refresh by hand with make agents-token-refresh-start" >&2; \
				exit 1; \
			fi; \
		fi; \
	fi; \
	echo "agents-token-refresh-install: loaded $$label: it runs now, at every boot, and again whenever it exits"; \
	if [ -n "$$account" ]; then \
		echo "agents-token-refresh-install: check it with: sudo launchctl print system/$$label (state = running, and $$script in its arguments), and its log: sudo tail -n 50 $$log; then run sudo -k"; \
	else \
		echo "agents-token-refresh-install: check it with: sudo launchctl print system/$$label (state = running, and $$script in its arguments), and its log: tail $$log"; \
	fi

.PHONY: agents-token-refresh-uninstall
agents-token-refresh-uninstall: ## Stop and remove the agents App token refresh's LaunchDaemon of this VM instance; needs sudo (host clone, macOS)
	$(LIMA_INSTANCE_CHECK)
	@command -v launchctl >/dev/null 2>&1 || { echo "agents-token-refresh-uninstall: no launchctl: the LaunchDaemon is for the Mac, and there is none to remove here" >&2; exit 1; }; \
	$(AGENTS_ACCOUNT_NAME_CHECK); $(AGENTS_ACCOUNT_KIND); $(AGENTS_ACCOUNT_KIND_WARNING); \
	$(AGENTS_ACCOUNT_KEY_FILE_WARNING); \
	$(AGENTS_TOKEN_LAUNCHD_DIR_CHECK); \
	label="$(AGENTS_TOKEN_LAUNCHD_LABEL_PREFIX)$$LIMA_INSTANCE"; dest="$${AGENTS_TOKEN_LAUNCHD_DIR%/}/$$label.plist"; \
	if [ -n "$$account" ]; then trap 'sudo -k' EXIT; fi; \
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
	if [ -n "$$account" ]; then \
		echo "agents-token-refresh-uninstall: removed $$dest; the log stays. The VM's token stays valid until it expires, within the hour: install the job again with make agents-token-refresh-install, or go back to single-account mode (docs/dev-environment.md, The agents App)"; \
	else \
		echo "agents-token-refresh-uninstall: removed $$dest; the log stays. The VM's token stays valid until it expires, within the hour: start a refresh by hand with make agents-token-refresh-start"; \
	fi; \
	if [ "$$stuck" = 1 ]; then \
		echo "agents-token-refresh-uninstall: warning: $$label is still loaded: unload it with sudo launchctl bootout system/$$label" >&2; \
	fi

# The LaunchDaemon that starts the Lima VM of LIMA_INSTANCE at boot, with nobody logged in, as the maintainer
# (issue #398, docs/adr/0038-start-the-lima-vm-at-boot-with-a-launchdaemon-of-our-own.md). dev/vm-boot-launchd.sh
# renders its plist: it runs `limactl shell --start --workdir / <instance> true` once at load, never
# `limactl start`, so it starts an existing instance and never creates one. One job per instance.
#
# install refuses, before any sudo: a run as root (the job runs as the account that installs it, and the VM is that
# account's); a host without launchctl; a VM_BOOT_LAUNCHD_DIR from the environment or of an unsafe form
# ($(call LAUNCHD_DIR_CHECK,VM_BOOT_LAUNCHD_DIR)); Lima's own autostart job of the instance,
# $(call LIMA_AUTOSTART_JOBS,refused) (the two would race for the VM, and Lima's can create template:default);
# and an instance `limactl list --quiet` does not list (or a listing that fails), since the job could only fail.
# It then renders the plist, lints it with plutil and prints it with its SHA-256, creates the log's directory as
# the maintainer, boots a loaded job of the same label out (waiting at most 10 seconds until launchctl print stops
# finding it), installs the plist with `sudo install`, reads it back as the maintainer and compares its SHA-256
# (removing it again on a mismatch), and loads it with `sudo launchctl bootstrap system <plist>`, tried twice,
# launchctl print deciding when both fail, as agents-token-refresh-install does. Loading it runs it once now
# (RunAtLoad): a Stopped VM starts. Every sudo command is printed before it runs, and the target ends with sudo -k.
#
# uninstall boots the job out, waits (at most 10 seconds) until launchctl print stops finding it, and removes
# exactly its plist either way; the VM keeps running, and the log stays.
#
# VM_BOOT_LAUNCHD_DIR is the same test hook AGENTS_TOKEN_LAUNCHD_DIR is (see there): never change it on a real
# Mac, and it comes only from the make command line or this file, never from the environment.
VM_BOOT_LAUNCHD_DIR_ORIGIN := $(origin VM_BOOT_LAUNCHD_DIR)
VM_BOOT_LAUNCHD_DIR ?= /Library/LaunchDaemons
# Also spelled out in dev/vm-boot-launchd.sh (label_prefix): keep the two alike.
VM_BOOT_LAUNCHD_LABEL_PREFIX = io.github.timothymamo.shift2eu.vm-boot.
VM_BOOT_STATE = $${XDG_STATE_HOME:-$$HOME/.local/state}/shift2eu
# The refusal of a run as root, shared by install and uninstall.
VM_BOOT_ROOT_CHECK = if [ "$$(id -u)" = 0 ]; then \
		echo "$@: refused: run it as the maintainer, not as root (no sudo make); it runs sudo itself. The job runs as the account that installs it, and the VM it starts is that account's" >&2; \
		exit 1; \
	fi; \
	command -v launchctl >/dev/null 2>&1 || { echo "$@: no launchctl: the LaunchDaemon is for the Mac" >&2; exit 1; }

vm-boot-install vm-boot-uninstall: export VM_BOOT_LAUNCHD_DIR := $(VM_BOOT_LAUNCHD_DIR)

.PHONY: vm-boot-install
vm-boot-install: ## Install (or update) the LaunchDaemon that starts this VM instance at boot, with nobody logged in; needs sudo (host clone, macOS)
	$(LIMA_INSTANCE_CHECK)
	@$(VM_BOOT_ROOT_CHECK); \
	$(call LAUNCHD_DIR_CHECK,VM_BOOT_LAUNCHD_DIR); \
	$(call LIMA_AUTOSTART_JOBS,refused); \
	if [ -n "$$lima_autostart" ]; then \
		echo "vm-boot-install: refused: Lima's own autostart job of $$LIMA_INSTANCE (named above) is loaded or installed, so nothing was installed: remove it with the commands above, then run make vm-boot-install again" >&2; \
		exit 1; \
	fi; \
	list="$$(limactl list --quiet)" || { \
		echo "vm-boot-install: refused: could not list the Lima instances, so it cannot tell whether $$LIMA_INSTANCE exists: nothing was installed" >&2; \
		exit 1; \
	}; \
	printf '%s\n' "$$list" | grep -qxF -- "$$LIMA_INSTANCE" || { \
		echo "vm-boot-install: refused: Lima lists no instance $$LIMA_INSTANCE, and the job starts an existing instance only: create the VM first (make vm-create), then run make vm-boot-install again" >&2; \
		exit 1; \
	}; \
	label="$(VM_BOOT_LAUNCHD_LABEL_PREFIX)$$LIMA_INSTANCE"; dest="$${VM_BOOT_LAUNCHD_DIR%/}/$$label.plist"; \
	state="$(VM_BOOT_STATE)"; log="$$state/vm-boot-$$LIMA_INSTANCE.log"; \
	tmp="$$(mktemp "$${TMPDIR:-/tmp}/vm-boot-plist.XXXXXX")" || exit 1; \
	trap 'rm -f "$$tmp"; sudo -k' EXIT; \
	dev/vm-boot-launchd.sh plist >"$$tmp" || exit 1; \
	plutil -lint "$$tmp" || { echo "vm-boot-install: plutil refused the rendered plist, so launchd would load no job: nothing was installed" >&2; exit 1; }; \
	sum="$$(shasum -a 256 <"$$tmp" | cut -d ' ' -f 1)" || exit 1; \
	echo "vm-boot-install: installing this plist as $$dest (it holds no credential):"; \
	cat "$$tmp"; \
	echo "vm-boot-install: its SHA-256 is $$sum"; \
	echo "vm-boot-install: creating $$state as $$(id -un); the job logs to $$log"; \
	(umask 077 && mkdir -p "$$state") || exit 1; \
	if launchctl print "system/$$label" >/dev/null 2>&1; then \
		echo "vm-boot-install: $$label is loaded already: unloading it to load the new plist"; \
		echo "+ sudo launchctl bootout system/$$label"; \
		sudo launchctl bootout "system/$$label" || echo "vm-boot-install: launchctl bootout exited non-zero; checking whether $$label unloaded anyway"; \
		tries=0; \
		while [ "$$tries" -lt 10 ] && launchctl print "system/$$label" >/dev/null 2>&1; do sleep 1; tries=$$((tries + 1)); done; \
		if launchctl print "system/$$label" >/dev/null 2>&1; then \
			echo "vm-boot-install: ERROR: the old job $$label is still loaded $$tries second(s) after launchctl bootout, so nothing was installed. Check it with sudo launchctl print system/$$label, wait for it to unload or run make vm-boot-uninstall, then run make vm-boot-install again" >&2; \
			exit 1; \
		fi; \
	fi; \
	echo "+ sudo install -m 0644 -o root -g wheel $$tmp $$dest"; \
	sudo install -m 0644 -o root -g wheel "$$tmp" "$$dest" || exit 1; \
	installed="$$( { shasum -a 256 | cut -d ' ' -f 1; } 2>/dev/null <"$$dest")" || installed=''; \
	if [ "$$installed" != "$$sum" ]; then \
		echo "+ sudo rm -f $$dest"; \
		sudo rm -f "$$dest" || { echo "vm-boot-install: ERROR: $$dest is not the plist printed above, and sudo rm -f could not remove it: remove it by hand before the next boot loads it" >&2; exit 1; }; \
		echo "vm-boot-install: ERROR: $$dest is not the plist printed above (its SHA-256 is $${installed:-unreadable}, not $$sum), so it was removed again and not loaded: look for what changed the file, then run make vm-boot-install again" >&2; \
		exit 1; \
	fi; \
	echo "vm-boot-install: $$dest has the SHA-256 printed above"; \
	echo "+ sudo launchctl bootstrap system $$dest"; \
	if ! sudo launchctl bootstrap system "$$dest"; then \
		sleep 1; \
		echo "+ sudo launchctl bootstrap system $$dest"; \
		if ! sudo launchctl bootstrap system "$$dest"; then \
			if launchctl print "system/$$label" >/dev/null 2>&1; then \
				echo "vm-boot-install: launchctl bootstrap exited non-zero twice, but launchctl print finds $$label loaded: the job is loaded" >&2; \
			else \
				echo "vm-boot-install: ERROR: launchctl bootstrap failed twice: the job is not loaded now. The checked plist stays at $$dest, so launchd loads it at the next boot; run make vm-boot-install again" >&2; \
				exit 1; \
			fi; \
		fi; \
	fi; \
	echo "vm-boot-install: loaded $$label: it ran once now (a Stopped VM starts) and runs again at every boot"; \
	echo "vm-boot-install: check it with: limactl list $$LIMA_INSTANCE (Running) and its log: tail $$log"

.PHONY: vm-boot-uninstall
vm-boot-uninstall: ## Remove the LaunchDaemon that starts this VM instance at boot (the VM keeps running); needs sudo (host clone, macOS)
	$(LIMA_INSTANCE_CHECK)
	@$(VM_BOOT_ROOT_CHECK); \
	$(call LAUNCHD_DIR_CHECK,VM_BOOT_LAUNCHD_DIR); \
	label="$(VM_BOOT_LAUNCHD_LABEL_PREFIX)$$LIMA_INSTANCE"; dest="$${VM_BOOT_LAUNCHD_DIR%/}/$$label.plist"; \
	trap 'sudo -k' EXIT; \
	echo "+ sudo launchctl bootout system/$$label"; \
	if sudo launchctl bootout "system/$$label"; then booted=1; else booted=0; fi; \
	tries=0; \
	while [ "$$tries" -lt 10 ] && launchctl print "system/$$label" >/dev/null 2>&1; do sleep 1; tries=$$((tries + 1)); done; \
	if launchctl print "system/$$label" >/dev/null 2>&1; then \
		echo "vm-boot-uninstall: warning: $$label is still loaded $$tries second(s) after launchctl bootout: unload it with sudo launchctl bootout system/$$label; removing its plist anyway, so the next boot does not load it" >&2; \
	elif [ "$$booted" = 1 ]; then \
		echo "vm-boot-uninstall: unloaded $$label"; \
	else \
		echo "vm-boot-uninstall: $$label was not loaded (launchctl bootout failed); removing its plist anyway"; \
	fi; \
	echo "+ sudo rm -f $$dest"; \
	sudo rm -f "$$dest" || exit 1; \
	echo "vm-boot-uninstall: removed $$dest; the VM keeps running and the log stays. After a reboot, start the VM with make vm-up"

# STRIP_CONTROL keeps only tab, newline and printable ASCII of what the VM printed, on stdout and stderr (the
# recipes capture both, 2>&1), before make echoes it: the scripts in the VM are code the agents can write, and an
# escape sequence or a C1 control (the byte 0x9B, or its UTF-8 form) could drive the maintainer's terminal (issue
# #227 part 2, the security audit's second and third pass).
STRIP_CONTROL = LC_ALL=C tr -cd '\011\012\040-\176'

# VM_OUTPUT prints what the VM printed ($$out, its two streams together) through $(STRIP_CONTROL) and, when
# limactl's $$status is non-zero, sends it to make's stderr and the recipe exits with that status: a failure the maintainer
# pipes away with make's stdout would be a failure they never see. A run that succeeds prints it on stdout (issue
# #227 part 2, the closing review).
VM_OUTPUT = if [ "$$status" -ne 0 ]; then \
		if [ -n "$$out" ]; then printf '%s\n' "$$out" | $(STRIP_CONTROL) >&2; fi; \
		exit "$$status"; \
	fi; \
	if [ -n "$$out" ]; then printf '%s\n' "$$out" | $(STRIP_CONTROL); fi

# For a VM created before dev/lima.yaml installed the gh wrapper: moves the real gh off the PATH, installs the
# wrapper with the real gh's path pinned and points the git credential helper at it, all over limactl's
# standard input (dev/vm-gh-wrapper.sh). The script is generated first and sent only when it is whole (its
# last line is the call that runs it), so a generator that fails fails make before the VM is touched. The VM's
# output, stdout and stderr together, is captured and printed through $(VM_OUTPUT) (filtered, and on make's stderr
# when limactl fails, the recipe exiting with its status), and make fails unless it holds the line "vm-gh-wrapper: done", so a
# connection that drops after the stream arrived (sh -s exits 0 having run nothing) is no success. Safe to
# run again.
.PHONY: vm-gh-wrapper
vm-gh-wrapper: ## Install the agents App's gh wrapper in a VM created before it (instead of make vm-delete vm-create)
	$(LIMA_INSTANCE_CHECK)
	@script="$$(dev/vm-gh-wrapper.sh dev/gh-app-token.sh)" || exit 1; \
	if [ "$$(printf '%s\n' "$$script" | tail -n 1)" != vm_gh_wrapper ]; then \
		echo "vm-gh-wrapper: dev/vm-gh-wrapper.sh printed no whole script (its last line is not vm_gh_wrapper); nothing sent to the VM" >&2; \
		exit 1; \
	fi; \
	out="$$(printf '%s\n' "$$script" | limactl shell --workdir / "$$LIMA_INSTANCE" sh -s 2>&1)"; status=$$?; \
	$(VM_OUTPUT); \
	if ! printf '%s\n' "$$out" | grep -Fqx 'vm-gh-wrapper: done'; then \
		echo "vm-gh-wrapper: the VM never printed vm-gh-wrapper: done, so the installation may not have finished; run make vm-gh-wrapper again" >&2; \
		exit 1; \
	fi

# The VM side of the account mode (ADR 0031, update of 2026-11-19, 4): make vm-agents-token-ssh lets the
# account's SSH key run one root-owned writer in the VM and nothing else, and pins the VM's host key for the
# account. Run by the maintainer from the host clone, with SHIFT2EU_AGENTS_ACCOUNT and SHIFT2EU_AGENTS_VM_SSH_PORT
# set; safe to run again (after make vm-delete vm-create, after a new SSH key). In order, stopping at the first
# failure: it refuses unless limactl reports exactly SHIFT2EU_AGENTS_VM_SSH_PORT as the instance's SSH port (Lima
# picks a random one unless ssh.localPort pins it); reads the account's public key as the account, with a printed
# sudo (every command run as the account goes through /usr/bin/env -i with PATH=/usr/bin:/bin and tools by full
# path, so nothing of the maintainer's environment reaches it); generates the VM's script with dev/vm-agents-token-ssh.sh, which refuses anything but one line
# `ssh-ed25519 <base64>` with an optional plain comment, and sends it only when it is whole over limactl shell's
# standard input, as vm-gh-wrapper does, failing unless the VM prints "vm-agents-token-ssh: done"; pins the
# VM's host key, read through limactl shell (no trust on first use), into the account's known-hosts file as the
# account, the line on standard input; and prints the next step. It ends with sudo -k, on success and on failure.
# make vm-agents-token-ssh-uninstall removes the writer, the keys file and the sshd drop-in from the VM and
# reloads sshd; it leaves the account's files alone and runs no sudo on the Mac.
.PHONY: vm-agents-token-ssh
vm-agents-token-ssh: ## Let the agents App account's SSH key write the token into the VM, and pin the VM's host key for it (host clone, account mode)
	$(LIMA_INSTANCE_CHECK)
	@$(AGENTS_ACCOUNT_CHECK); \
	if [ -z "$$account" ]; then \
		echo "vm-agents-token-ssh: set SHIFT2EU_AGENTS_ACCOUNT, the account that holds the App's key, in the host clone's .envrc (docs/dev-environment.md, The agents App)" >&2; \
		exit 1; \
	fi; \
	$(AGENTS_SSH_CONFIG); \
	listed="$$(limactl list --format '{{.SSHLocalPort}}' "$$LIMA_INSTANCE")" || { \
		echo "vm-agents-token-ssh: limactl list could not read the SSH port of the VM $$LIMA_INSTANCE: create it first (make vm-create)" >&2; \
		exit 1; \
	}; \
	if [ "$$listed" != "$$port" ]; then \
		echo "vm-agents-token-ssh: refused: limactl reports the VM $$LIMA_INSTANCE's SSH port as $${listed:-nothing}, not SHIFT2EU_AGENTS_VM_SSH_PORT $$port, so the account's job would write nowhere: pin the port while the VM is stopped (limactl stop $$LIMA_INSTANCE, limactl edit --set '.ssh.localPort = $$port' $$LIMA_INSTANCE, limactl start $$LIMA_INSTANCE) and run make vm-agents-token-ssh again (docs/dev-environment.md, The agents App)" >&2; \
		exit 1; \
	fi; \
	secrets="/Users/$$account/.config/shift2eu"; known="$$secrets/known-hosts-$$LIMA_INSTANCE"; \
	writer="$$(mktemp "$${TMPDIR:-/tmp}/agents-token-write.XXXXXX")" || exit 1; \
	trap 'rm -f "$$writer"; sudo -k' EXIT; \
	dev/agents-app-token.sh vm-writer >"$$writer" || exit 1; \
	echo "+ sudo -u $$account -H /usr/bin/env -i PATH=/usr/bin:/bin /bin/cat $$secrets/agents-vm-ssh-key.pub"; \
	pub="$$(sudo -u "$$account" -H /usr/bin/env -i PATH=/usr/bin:/bin /bin/cat "$$secrets/agents-vm-ssh-key.pub")" || { \
		echo "vm-agents-token-ssh: cannot read the account's public key $$secrets/agents-vm-ssh-key.pub: generate it as the account (docs/dev-environment.md, The agents App)" >&2; \
		exit 1; \
	}; \
	script="$$(dev/vm-agents-token-ssh.sh "$$writer" "$$pub" "$$vm_user")" || exit 1; \
	if [ "$$(printf '%s\n' "$$script" | tail -n 1)" != vm_agents_token_ssh ]; then \
		echo "vm-agents-token-ssh: dev/vm-agents-token-ssh.sh printed no whole script (its last line is not vm_agents_token_ssh); nothing sent to the VM" >&2; \
		exit 1; \
	fi; \
	out="$$(printf '%s\n' "$$script" | limactl shell --workdir / "$$LIMA_INSTANCE" sh -s 2>&1)"; status=$$?; \
	$(VM_OUTPUT); \
	if ! printf '%s\n' "$$out" | grep -Fqx 'vm-agents-token-ssh: done'; then \
		echo "vm-agents-token-ssh: the VM never printed vm-agents-token-ssh: done, so the installation may not have finished; run make vm-agents-token-ssh again" >&2; \
		exit 1; \
	fi; \
	host_key="$$(printf '%s\n' "$$out" | sed -n 's|^host-key=\(ssh-ed25519 [ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/][ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/]*=*\)$$|\1|p' | head -n 1)"; \
	if [ -z "$$host_key" ]; then \
		echo "vm-agents-token-ssh: the VM printed no host-key=ssh-ed25519 <base64> line, so its host key cannot be pinned for the account; run make vm-agents-token-ssh again" >&2; \
		exit 1; \
	fi; \
	echo "+ sudo -u $$account -H /usr/bin/env -i PATH=/usr/bin:/bin /bin/sh -c 'umask 077 && /bin/cat >\"\$$1.new\" && /bin/mv -f \"\$$1.new\" \"\$$1\"' pin $$known"; \
	printf 'shift2eu-vm-%s %s\n' "$$LIMA_INSTANCE" "$$host_key" | \
		sudo -u "$$account" -H /usr/bin/env -i PATH=/usr/bin:/bin /bin/sh -c 'umask 077 && /bin/cat >"$$1.new" && /bin/mv -f "$$1.new" "$$1"' pin "$$known" || { \
		echo "vm-agents-token-ssh: could not pin the VM's host key in $$known as $$account" >&2; \
		exit 1; \
	}; \
	echo "vm-agents-token-ssh: pinned shift2eu-vm-$$LIMA_INSTANCE $$host_key in $$known"; \
	if launchctl print "system/$(AGENTS_TOKEN_LAUNCHD_LABEL_PREFIX)$$LIMA_INSTANCE" >/dev/null 2>&1; then \
		echo "vm-agents-token-ssh: next: the job is loaded and writes at its next try; to mint now, run sudo launchctl kickstart -k system/$(AGENTS_TOKEN_LAUNCHD_LABEL_PREFIX)$$LIMA_INSTANCE, then sudo -k"; \
	else \
		echo "vm-agents-token-ssh: next: make agents-token-refresh-install"; \
	fi

.PHONY: vm-agents-token-ssh-uninstall
vm-agents-token-ssh-uninstall: ## Remove the agents App account's writer, keys file and sshd drop-in from the VM (host clone)
	$(LIMA_INSTANCE_CHECK)
	@$(AGENTS_SSH_CONFIG_USER); \
	script="$$(dev/vm-agents-token-ssh.sh --uninstall "$$vm_user")" || exit 1; \
	if [ "$$(printf '%s\n' "$$script" | tail -n 1)" != vm_agents_token_ssh_uninstall ]; then \
		echo "vm-agents-token-ssh-uninstall: dev/vm-agents-token-ssh.sh printed no whole script (its last line is not vm_agents_token_ssh_uninstall); nothing sent to the VM" >&2; \
		exit 1; \
	fi; \
	out="$$(printf '%s\n' "$$script" | limactl shell --workdir / "$$LIMA_INSTANCE" sh -s 2>&1)"; status=$$?; \
	$(VM_OUTPUT); \
	if ! printf '%s\n' "$$out" | grep -Fqx 'vm-agents-token-ssh-uninstall: done'; then \
		echo "vm-agents-token-ssh-uninstall: the VM never printed vm-agents-token-ssh-uninstall: done, so the removal may not have finished; run make vm-agents-token-ssh-uninstall again" >&2; \
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
